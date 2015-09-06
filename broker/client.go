package hrotti

import (
	"net"
	"sync"
	"time"

	"github.com/alsm/hrotti/packets"

	"code.google.com/p/go-uuid/uuid"
)

type Client struct {
	sync.WaitGroup
	messageIDs
	clientID         string
	authContext      AuthContext
	conn             net.Conn
	keepAlive        uint16
	state            State
	topicSpace       string
	outboundMessages chan *packets.PublishPacket
	outboundPriority chan packets.ControlPacket
	stop             chan struct{}
	stopOnce         *sync.Once
	resetTimer       chan bool
	cleanSession     bool
	willMessage      *packets.PublishPacket
	takeOver         bool
}

func newClient(conn net.Conn, clientID string, authContext AuthContext, maxQDepth int) *Client {
	return &Client{
		conn:             conn,
		clientID:         clientID,
		authContext:      authContext,
		stop:             make(chan struct{}),
		resetTimer:       make(chan bool, 1),
		outboundMessages: make(chan *packets.PublishPacket, maxQDepth),
		outboundPriority: make(chan packets.ControlPacket, maxQDepth),
		stopOnce:         new(sync.Once),
		messageIDs: messageIDs{
			//idChan: make(chan uint16, 10),
			index: make(map[uint16]uuid.UUID),
		},
	}
}

func (c *Client) Connected() bool {
	return c.state.Value() == CONNECTED
}

func (c *Client) KeepAliveTimer(hrotti *Hrotti) {
	//this function is part of the client's waitgroup so call Done() when the function exits
	defer c.Done()
	//In a continuous loop create a Timer for 1.5 * the keepAlive setting
	for {
		t := time.NewTimer(time.Duration(float64(c.keepAlive)*1.5) * time.Second)
		//this select will block on all 3 cases until one of them is ready
		select {
		//if we get a value in on the resetTimer channel we drop out, stop the Timer then loop round again
		case _ = <-c.resetTimer:
		//if the timer triggers then the client has failed to send us a packet in the keepAlive period so
		//must be disconnected, we call Stop() and the function returns.
		case <-t.C:
			ERROR.Println(c.clientID, "has timed out", c.keepAlive)
			go c.Stop(true, hrotti)
			return
		//the client sent a DISCONNECT or some error occurred that triggered the client to stop, so return.
		case <-c.stop:
			return
		}
		t.Stop()
	}
}

func (c *Client) StopForTakeover() {
	//close the stop channel, close the network connection, wait for all the goroutines in the waitgroup to
	//finish, set the conn and bufferedconn to nil
	c.takeOver = true
	c.stopOnce.Do(func() {
		INFO.Println("Closing Stop chan")
		close(c.stop)
		INFO.Println("Closing connection")
		c.conn.Close()
		c.Wait()
		c.conn = nil
	})
}

func (c *Client) Stop(sendWill bool, hrotti *Hrotti) {
	//Its possible that error conditions with the network connection might cause both Send and Receive to
	//try and call Stop(), but we only want it to be called once, so using the sync.Once in the client we
	//run the embedded function, later calls with the same sync.Once will simply return.
	INFO.Println("Stopping client", c.clientID, c.conn.RemoteAddr())
	if !c.takeOver {
		c.stopOnce.Do(func() {
			//close the stop channel, close the network connection, wait for all the goroutines in the waitgroup
			//set the state as disconnected, close the message channels.
			close(c.stop)
			c.conn.Close()
			c.Wait()
			c.state.SetValue(DISCONNECTED)
			close(c.outboundMessages)
			close(c.outboundPriority)
			//If we've stopped in a situation where the will message should be sent, and there is a will
			//message, then send it.
			if sendWill && c.willMessage != nil {
				INFO.Println("Sending will message for", c.clientID)
				go hrotti.DeliverMessage(c.willMessage.TopicName, c.willMessage)
			}
			//if this client connected with cleansession true it means it does not need its state (such as
			//subscriptions, unreceived messages etc) kept around
			if c.cleanSession {
				//so we lock the clients map, delete the clientid and *Client from the map, remove all subscriptions
				//associated with this client, from the normal tree and any plugins. Then close the persistence
				//store that it was using.
				hrotti.clients.Lock()
				delete(hrotti.clients.list, c.clientID)
				hrotti.clients.Unlock()
				hrotti.DeleteSubAll(c.clientID)
				hrotti.PersistStore.Close(c.clientID)
			}
		})
	}
}

func (c *Client) Start(cp *packets.ConnectPacket, hrotti *Hrotti) {
	//If cleansession was set to 1 in the CONNECT packet set as true in the client.
	c.cleanSession = cp.CleanSession
	//There is a will message in the connect packet, so construct the publish packet that will be sent if
	//the will is triggered.
	if cp.WillFlag {
		pp := packets.NewControlPacket(packets.PUBLISH).(*packets.PublishPacket)
		pp.FixedHeader.Qos = cp.WillQos
		pp.FixedHeader.Retain = cp.WillRetain
		pp.TopicName = cp.WillTopic
		pp.Payload = cp.WillMessage

		c.willMessage = pp
	} else {
		c.willMessage = nil
	}
	c.keepAlive = cp.KeepaliveTimer

	//If cleansession true, or there doesn't already exist a persistence store for this client (ie a new
	//durable client), create the inbound and outbound persistence stores.
	if c.cleanSession || !hrotti.PersistStore.Exists(c.clientID) {
		hrotti.PersistStore.Open(c.clientID)
	} else {
		//we have an existing inbound and outbound persistence store for this client already, so lets
		//get any messages still in outbound and attempt to send them.
		INFO.Println("Getting unacknowledged messages from persistence")
		for _, msg := range hrotti.PersistStore.GetAll(c.clientID) {
			switch msg.(type) {
			//If the message in the store is a publish packet
			case *packets.PublishPacket:
				//It's possible we already sent this message and didn't remove it from the store because we
				//didn't get an acknowledgement, so set the dup flag to 1. (only for QoS > 0)
				if msg.(*packets.PublishPacket).Qos > 0 {
					msg.(*packets.PublishPacket).Dup = true
				}
				c.outboundMessages <- msg.(*packets.PublishPacket)
			//If it's something else like a PUBACK etc send it to the priority outbound channel
			default:
				c.outboundPriority <- msg
			}
		}
	}

	//Prepare and write the CONNACK packet.
	ca := packets.NewControlPacket(packets.CONNACK).(*packets.ConnackPacket)
	ca.ReturnCode = packets.CONN_ACCEPTED
	ca.Write(c.conn)
	//Receive and Send are part of this WaitGroup, so add 2 to the waitgroup and run the goroutines.
	c.Add(2)
	go c.Receive(hrotti)
	go c.Send(hrotti)
	c.state.SetValue(CONNECTED)
	//If keepalive value was set run the keepalive time and add 1 to the waitgroup.
	if c.keepAlive > 0 {
		c.Add(1)
		go c.KeepAliveTimer(hrotti)
	}
}

func validateclientID(clientID string) bool {
	return true
}

func (c *Client) ResetTimer() {
	//If we're using keepalive on this client attempt to reset the timer, if the channel blocks it's because
	//the timer is already being reset so we can safely drop the attempt here (the default case of the select)
	if c.keepAlive > 0 {
		select {
		case c.resetTimer <- true:
		default:
		}
	}
}

func (c *Client) Receive(hrotti *Hrotti) {
	//part of the client waitgroup so call Done() when the function returns.
	defer c.Done()
	//loop forever...
	for {
		select {
		//if called to stop then return
		case <-c.stop:
			return
		//otherwise...
		default:
			cp, err := packets.ReadPacket(c.conn)
			if err != nil {
				ERROR.Println(err.Error(), c.clientID)
				go c.Stop(true, hrotti)
				return
			}

			// reset the keep alive timer.
			c.ResetTimer()

			switch cp.(type) {
			//a second CONNECT packet is a protocol violation, so Stop (send will) and return.
			case *packets.ConnectPacket:
				ERROR.Println("Received second CONNECT from", c.clientID, c.authContext)
				go c.Stop(true, hrotti)
				return
			//client wishes to disconnect so Stop (don't send will) and return.
			case *packets.DisconnectPacket:
				INFO.Println("Received DISCONNECT from", c.clientID)
				go c.Stop(false, hrotti)
				return
			//client has sent us a PUBLISH message, unpack it persist (if QoS > 0) in the inbound store
			case *packets.PublishPacket:
				pp := cp.(*packets.PublishPacket)
				PROTOCOL.Println("Received PUBLISH from", c.clientID, pp.TopicName)
				if pp.Qos > 0 {
					hrotti.PersistStore.Add(c.clientID, INBOUND, pp)
				}
				//if this message has the retained flag set then set as the retained message for the
				//appropriate node in the topic tree
				if pp.Retain {
					hrotti.subs.SetRetained(pp.TopicName, pp)
				}
				// if we are permitted to publish this packet then deliver it
				if c.authContext.CheckPublish(pp) {
					//go and deliver the message to any subscribers.
					go hrotti.DeliverMessage(pp.TopicName, pp)
				}
				//if the message was QoS1 or QoS2 start the acknowledgement flows.
				switch pp.Qos {
				case 1:
					pa := packets.NewControlPacket(packets.PUBACK).(*packets.PubackPacket)
					pa.MessageID = pp.MessageID
					c.HandleFlow(pa, hrotti)
				case 2:
					pr := packets.NewControlPacket(packets.PUBREC).(*packets.PubrecPacket)
					pr.MessageID = pp.MessageID
					c.HandleFlow(pr, hrotti)
				}
			//We received a PUBACK acknowledging a QoS1 PUBLISH we sent to the client
			case *packets.PubackPacket:
				pa := cp.(*packets.PubackPacket)
				//Check that we also think this message id is in use, if it is remove the original
				//PUBLISH from the outbound persistence store and set the message id as free for reuse
				if c.inUse(pa.MessageID) {
					hrotti.PersistStore.Delete(c.clientID, OUTBOUND, pa.UUID())
					c.freeID(pa.MessageID)
				} else {
					ERROR.Println("Received a PUBACK for unknown msgid", pa.MessageID, "from", c.clientID)
				}
			//We received a PUBREC for a QoS2 PUBLISH we sent to the client.
			case *packets.PubrecPacket:
				pr := cp.(*packets.PubrecPacket)
				//Check that we also think this message id is in use, if it is run the next stage of the
				//message flows for QoS2 messages.
				if c.inUse(pr.MessageID) {
					prel := packets.NewControlPacket(packets.PUBREL).(*packets.PubrelPacket)
					prel.MessageID = pr.MessageID
					c.HandleFlow(prel, hrotti)
				} else {
					ERROR.Println("Received a PUBREC for unknown msgid", pr.MessageID, "from", c.clientID)
				}
			//We received a PUBREL for a QoS2 PUBLISH from the client, hrotti delivers on PUBLISH though
			//so we've already sent the original message to any subscribers, so just create a new
			//PUBCOMP message with the correct message id and pass it to the HandleFlow function.
			case *packets.PubrelPacket:
				pr := cp.(*packets.PubrelPacket)
				pc := packets.NewControlPacket(packets.PUBCOMP).(*packets.PubcompPacket)
				pc.MessageID = pr.MessageID
				c.HandleFlow(pc, hrotti)
			//Received a PUBCOMP for a QoS2 PUBLISH we originally sent the client. Check the messageid is
			//one we think is in use, if so delete the original PUBLISH from the outbound persistence store
			//and free the message id for reuse
			case *packets.PubcompPacket:
				pc := cp.(*packets.PubcompPacket)
				if c.inUse(pc.MessageID) {
					//hrotti.PersistStore.Delete(c, OUTBOUND, pc.UUID)
					c.freeID(pc.MessageID)
				} else {
					ERROR.Println("Received a PUBCOMP for unknown msgid", pc.MessageID, "from", c.clientID)
				}
			//The client wishes to make a subscription, unpack the message and call AddSubscription with the
			//requested topics and QoS'. Create a new SUBACK message and put the granted QoS values in it
			//and send back to the client.
			case *packets.SubscribePacket:
				PROTOCOL.Println("Received SUBSCRIBE from", c.clientID)
				sp := cp.(*packets.SubscribePacket)
				rQos := hrotti.AddSubscription(c, sp.Topics, sp.Qoss)
				sa := packets.NewControlPacket(packets.SUBACK).(*packets.SubackPacket)
				sa.MessageID = sp.MessageID
				sa.GrantedQoss = append(sa.GrantedQoss, rQos...)
				c.outboundPriority <- sa
			//The client wants to unsubscribe from a topic.
			case *packets.UnsubscribePacket:
				PROTOCOL.Println("Received UNSUBSCRIBE from", c.clientID)
				up := cp.(*packets.UnsubscribePacket)
				hrotti.RemoveSubscription(c, up.Topics[0])
				ua := packets.NewControlPacket(packets.UNSUBACK).(*packets.UnsubackPacket)
				ua.MessageID = up.MessageID
				c.outboundPriority <- ua
			//As part of the keepalive if the client doesn't have any messages to send us for as long as the
			//keepalive period it will send a ping request, so we send a ping response back
			case *packets.PingreqPacket:
				presp := packets.NewControlPacket(packets.PINGRESP).(*packets.PingrespPacket)
				c.outboundPriority <- presp
			}
		}
	}
}

func (c *Client) HandleFlow(msg packets.ControlPacket, hrotti *Hrotti) {
	switch msg.(type) {
	case *packets.PubrelPacket:
		hrotti.PersistStore.Replace(c.clientID, OUTBOUND, msg)
	case *packets.PubackPacket, *packets.PubcompPacket:
		hrotti.PersistStore.Delete(c.clientID, INBOUND, msg.UUID())
	}
	//send to channel if open, silently drop if channel closed
	select {
	case c.outboundPriority <- msg:
	default:
	}
}

func (c *Client) Send(hrotti *Hrotti) {
	//Send is part of the client waitgroup so call Done when the function returns.
	defer c.Done()
	for {
		//3 way blocking select
		select {
		//the stop channel has been closed so we should return
		case <-c.stop:
			return
		//the two value receive from a channel tells us whether the channel is closed
		//as reading from a closed channel always returns the empty value for the channel
		//type. ok == false means the channel is closed and the msg will be nil
		case msg, ok := <-c.outboundPriority:
			if ok {
				//Message IDs are not assigned until we're ready to send the message
				switch msg.(type) {
				case *packets.SubscribePacket:
					msg.(*packets.SubscribePacket).MessageID = c.getMsgID(msg.UUID())
				case *packets.UnsubscribePacket:
					msg.(*packets.UnsubscribePacket).MessageID = c.getMsgID(msg.UUID())
				}
				msg.Write(c.conn)
			}
		case msg, ok := <-c.outboundMessages:
			//ok == false means we were triggered because the channel
			//is closed, and the msg will be nil
			if ok {
				switch msg.Details().Qos {
				case 1, 2:
					msg.MessageID = c.getMsgID(msg.UUID())
				}
				msg.Write(c.conn)
			}
		}
	}
}
