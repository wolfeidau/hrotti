package hrotti

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"

	"code.google.com/p/go-uuid/uuid"
	"code.google.com/p/go.net/websocket"
	. "github.com/alsm/hrotti/packets"
)

type Hrotti struct {
	PersistStore       Persistence
	authHandler        AuthHandler
	listeners          map[string]*internalListener
	listenersWaitGroup sync.WaitGroup
	maxQueueDepth      int
	clients            *clients
	subs               *subscriptionMap
}

type internalListener struct {
	name        string
	url         url.URL
	connections []net.Conn
	stop        chan struct{}
}

func NewHrotti(maxQueueDepth int, persistence Persistence, authHandler AuthHandler) *Hrotti {

	if authHandler == nil {
		authHandler = NoopAuthHandler
	}

	h := &Hrotti{
		PersistStore:  persistence,
		authHandler:   authHandler,
		listeners:     make(map[string]*internalListener),
		maxQueueDepth: maxQueueDepth,
		clients:       newClients(),
		subs:          newSubMap(),
	}
	//start the goroutine that generates internal message ids for when clients receive messages
	//but are not connected.
	h.PersistStore.Init()
	return h
}

func (h *Hrotti) getClient(id string) *Client {
	h.clients.RLock()
	defer h.clients.RUnlock()
	return h.clients.list[id]
}

func (h *Hrotti) AddListener(name string, config *ListenerConfig) error {
	listener := &internalListener{name: name, url: *config.URL}
	listener.stop = make(chan struct{})

	h.listeners[name] = listener

	ln, err := net.Listen("tcp", listener.url.Host)
	if err != nil {
		ERROR.Println(err.Error())
		return err
	}

	if listener.url.Scheme == "ws" && len(listener.url.Path) == 0 {
		listener.url.Path = "/"
	}

	h.listenersWaitGroup.Add(1)
	INFO.Println("Starting MQTT listener on", listener.url.String())

	go func() {
		<-listener.stop
		INFO.Println("Listener", name, "is stopping...")
		ln.Close()
	}()
	//if this is a WebSocket listener
	if listener.url.Scheme == "ws" {
		var server websocket.Server
		//override the Websocket handshake to accept any protocol name
		server.Handshake = func(c *websocket.Config, req *http.Request) error {
			c.Origin, _ = url.Parse(req.RemoteAddr)
			c.Protocol = []string{"mqtt"}
			return nil
		}
		//set up the ws connection handler, ie what we do when we get a new websocket connection
		server.Handler = func(ws *websocket.Conn) {
			ws.PayloadType = websocket.BinaryFrame
			INFO.Println("New incoming websocket connection", ws.RemoteAddr())
			listener.connections = append(listener.connections, ws)
			h.InitClient(ws)
		}
		//set the path that the http server will recognise as related to this websocket
		//server, needs to be configurable really.
		http.Handle(listener.url.Path, server)
		//ListenAndServe loops forever receiving connections and initiating the handler
		//for each one.
		go func(ln net.Listener) {
			defer h.listenersWaitGroup.Done()
			err := http.Serve(ln, nil)
			if err != nil {
				ERROR.Println(err.Error())
				return
			}
		}(ln)
	} else {
		//loop forever accepting connections and launch InitClient as a goroutine with the connection
		go func() {
			defer h.listenersWaitGroup.Done()
			for {
				conn, err := ln.Accept()
				if err != nil {
					ERROR.Println(err.Error())
					return
				}
				INFO.Println("New incoming connection", conn.RemoteAddr())
				listener.connections = append(listener.connections, conn)
				go h.InitClient(conn)
			}
		}()
	}
	return nil
}

func (h *Hrotti) StopListener(name string) error {
	if listener, ok := h.listeners[name]; ok {
		close(listener.stop)
		for _, conn := range listener.connections {
			conn.Close()
		}
		delete(h.listeners, name)
		return nil
	}
	return errors.New("Listener not found")
}

func (h *Hrotti) Stop() {
	INFO.Println("Exiting...")
	for _, listener := range h.listeners {
		close(listener.stop)
	}
	h.listenersWaitGroup.Wait()
}

func (h *Hrotti) InitClient(conn net.Conn) {
	var sendSessionID bool

	rp, _ := ReadPacket(conn)
	cp := rp.(*ConnectPacket)

	//Validate the CONNECT, check fields, values etc.
	rc := cp.Validate()
	//If it didn't validate...
	if rc != CONN_ACCEPTED {
		//and it wasn't because of a protocol violation...
		if rc != CONN_PROTOCOL_VIOLATION {
			//create and send a CONNACK with the correct rc in it.
			ca := NewControlPacket(CONNACK).(*ConnackPacket)
			ca.ReturnCode = rc
			ca.Write(conn)
		}
		//Put up a local message indicating an errored connection attempt and close the connection
		ERROR.Println(ConnackReturnCodes[rc], conn.RemoteAddr())
		conn.Close()
		return
	} else {
		//Put up an INFO message with the client id and the address they're connecting from.
		INFO.Println(ConnackReturnCodes[rc], cp.ClientIdentifier, conn.RemoteAddr())
	}

	// check the auth handler
	userID, err:= h.authHandler(cp)

	if err != nil {
		//create and send a CONNACK with the correct rc in it.
		ca := NewControlPacket(CONNACK).(*ConnackPacket)
		ca.ReturnCode = CONN_REF_BAD_USER_PASS
		ca.Write(conn)
		//Put up a local message indicating an errored connection attempt and close the connection
		ERROR.Println(ConnackReturnCodes[rc], conn.RemoteAddr())
		conn.Close()
		return
	}

	//check for a zero length client id and if it exists create one from the UUID library and return
	//it on $SYS/session_identifier
	if len(cp.ClientIdentifier) == 0 {
		cp.ClientIdentifier = uuid.New()
		sendSessionID = true
	}
	//Lock the clients hashmap while we check if we already know this clientid.
	h.clients.Lock()
	c, ok := h.clients.list[cp.ClientIdentifier]
	if ok && cp.CleanSession {
		//and if we do, if the clientid is currently connected...
		if c.Connected() {
			INFO.Println("Clientid", c.clientID, "already connected, stopping first client")
			//stop the parts of it that need to stop before we can change the network connection it's using.
			c.StopForTakeover()
		} else {
			//if the clientid known but not connected, ie cleansession false
			INFO.Println("Durable client reconnecting", c.clientID)
			//disconnected client will no longer have the channels for messages
			c.outboundMessages = make(chan *PublishPacket, h.maxQueueDepth)
			c.outboundPriority = make(chan ControlPacket, h.maxQueueDepth)
		}
		//this function stays running until the client disconnects as the function called by an http
		//Handler has to remain running until its work is complete. So add one to the client waitgroup.
		c.Add(1)
		//create a new sync.Once for stopping with later, set the connections and create the stop channel.
		c.stopOnce = new(sync.Once)
		c.conn = conn
		//c.bufferedConn = bufferedConn
		c.stop = make(chan struct{})
		//start the client.
		go c.Start(cp, h)
	} else {
		//This is a brand new client so create a NewClient and add to the clients map
		c = newClient(conn, cp.ClientIdentifier, userID, h.maxQueueDepth)
		h.clients.list[cp.ClientIdentifier] = c
		if sendSessionID {
			go func() {
				sessionIDPacket := NewControlPacket(PUBLISH).(*PublishPacket)
				sessionIDPacket.TopicName = "$SYS/session_identifier"
				sessionIDPacket.Payload = []byte(cp.ClientIdentifier)
				sessionIDPacket.Qos = 1
				c.outboundMessages <- sessionIDPacket
			}()
		}
		//As before this function has to remain running but to avoid races we want to make sure its finished
		//before doing anything else so add it to the waitgroup so we can wait on it later
		c.Add(1)
		go c.Start(cp, h)
	}
	//finished with the clients hashmap
	h.clients.Unlock()
	//wait on the stop channel, we never actually send values down this channel but a closed channel with
	//return the default empty value for it's type without blocking.
	<-c.stop
	//call Done() on the client waitgroup.
	c.Done()
}
