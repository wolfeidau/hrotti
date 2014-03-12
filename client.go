package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

type Client struct {
	sync.RWMutex
	MessageIds
	clientId         string
	conn             net.Conn
	bufferedConn     *bufio.ReadWriter
	rootNode         *Node
	keepAlive        uint
	connected        bool
	topicSpace       string
	outboundMessages *msgQueue
	stop             chan bool
	lastSeen         time.Time
	cleanSession     bool
	willMessage      *publishPacket
}

func NewClient(conn net.Conn, bufferedConn *bufio.ReadWriter, clientId string) *Client {
	c := &Client{}
	c.conn = conn
	c.bufferedConn = bufferedConn
	c.clientId = clientId
	c.stop = make(chan bool)
	c.outboundMessages = NewMsgQueue(1000)
	c.rootNode = rootNode

	return c
}

func (c *Client) Remove() {
	if c.cleanSession {
		clients.Lock()
		delete(clients.list, c.clientId)
		clients.Unlock()
	}
}

func (c *Client) Stop(sendWill bool) {
	close(c.stop)
	c.conn.Close()
	if sendWill && c.willMessage != nil {
		go c.rootNode.DeliverMessage(strings.Split(c.willMessage.topicName, "/"), c.willMessage)
	}
	c.connected = false
}

func (c *Client) Start(cp *connectPacket) {
	if cp.cleanSession == 1 {
		c.cleanSession = true
	}
	if cp.willFlag == 1 {
		pp := New(PUBLISH).(*publishPacket)
		pp.FixedHeader.Qos = cp.willQos
		pp.FixedHeader.Retain = cp.willRetain
		pp.topicName = cp.willTopic
		pp.payload = cp.willMessage

		c.willMessage = pp
	} else {
		c.willMessage = nil
	}
	go c.Receive()
	go c.Send()
	ca := New(CONNACK).(*connackPacket)
	ca.returnCode = CONN_ACCEPTED
	c.outboundMessages.Push(ca)
	c.connected = true
}

func (c *Client) resetLastSeenTime() {
	c.lastSeen = time.Now()
}

func validateClientId(clientId string) bool {
	return true
}

func (c *Client) SetRootNode(node *Node) {
	c.rootNode = node
}

func (c *Client) AddSubscription(topic string, qos uint) {
	complete := make(chan bool)
	defer close(complete)
	c.rootNode.AddSub(c, strings.Split(topic, "/"), qos, complete)
	<-complete
	return
}

func (c *Client) RemoveSubscription(topic string) (bool, error) {
	complete := make(chan bool)
	defer close(complete)
	c.rootNode.DeleteSub(c, strings.Split(topic, "/"), complete)
	<-complete
	return true, nil
}

func (c *Client) Receive() {
	for {
		var cph FixedHeader
		var err error
		var body []byte
		var typeByte byte

		typeByte, err = c.bufferedConn.ReadByte()
		if err != nil {
			break
		}
		cph.unpack(typeByte)
		cph.remainingLength = decodeLength(c.bufferedConn)

		if cph.remainingLength > 0 {
			body = make([]byte, cph.remainingLength)
			_, err = io.ReadFull(c.bufferedConn, body)
			if err != nil {
				break
			}
		}

		switch cph.MessageType {
		case DISCONNECT:
			fmt.Println("Received DISCONNECT from", c.clientId)
			dp := New(DISCONNECT).(*disconnectPacket)
			dp.FixedHeader = cph
			dp.Unpack(body)
			c.Stop(false)
			c.Remove()
			continue
		case PUBLISH:
			//fmt.Println("Received PUBLISH from", c.clientId)
			pp := New(PUBLISH).(*publishPacket)
			pp.FixedHeader = cph
			pp.Unpack(body)
			c.rootNode.DeliverMessage(strings.Split(pp.topicName, "/"), pp)
			switch pp.Qos {
			case 1:
				pa := New(PUBACK).(*pubackPacket)
				pa.messageId = pp.messageId
				//c.outboundMessages <- pa
				c.outboundMessages.PushHead(pa)
			case 2:
				pr := New(PUBREC).(*pubrecPacket)
				pr.messageId = pp.messageId
				//c.outboundMessages <- pr
				c.outboundMessages.PushHead(pr)
			}
		case PUBACK:
			pa := New(PUBACK).(*pubackPacket)
			pa.FixedHeader = cph
			pa.Unpack(body)
		case PUBREC:
			pr := New(PUBREC).(*pubrecPacket)
			pr.FixedHeader = cph
			pr.Unpack(body)
		case PUBREL:
			pr := New(PUBREL).(*pubrelPacket)
			pr.FixedHeader = cph
			pr.Unpack(body)
			pc := New(PUBCOMP).(*pubcompPacket)
			pc.messageId = pr.messageId
			c.outboundMessages.PushHead(pc)
		case PUBCOMP:
			pc := New(PUBCOMP).(*pubcompPacket)
			pc.FixedHeader = cph
			pc.Unpack(body)
		case SUBSCRIBE:
			fmt.Println("Received SUBSCRIBE from", c.clientId)
			sp := New(SUBSCRIBE).(*subscribePacket)
			sp.FixedHeader = cph
			sp.Unpack(body)
			c.AddSubscription(sp.topics[0], sp.qoss[0])
			sa := New(SUBACK).(*subackPacket)
			sa.messageId = sp.messageId
			sa.grantedQoss = append(sa.grantedQoss, byte(sp.qoss[0]))
			c.outboundMessages.PushHead(sa)
		case UNSUBSCRIBE:
			fmt.Println("Received UNSUBSCRIBE from", c.clientId)
			up := New(UNSUBSCRIBE).(*unsubscribePacket)
			up.FixedHeader = cph
			up.Unpack(body)
			c.RemoveSubscription(up.topics[0])
			ua := New(UNSUBACK).(*unsubackPacket)
			ua.messageId = up.messageId
			c.outboundMessages.PushHead(ua)
		case PINGREQ:
			presp := New(PINGRESP).(*pingrespPacket)
			c.outboundMessages.PushHead(presp)
		}
	}
	select {
	case <-c.stop:
		return
	default:
		fmt.Println("Error on socket read", c.clientId)
		c.Stop(true)
		c.Remove()
		return
	}
}

func (c *Client) Send() {
	for {
		select {
		case <-c.outboundMessages.ready:
			msg := c.outboundMessages.Pop()
			c.conn.Write(msg.Pack())
		case <-c.stop:
			return
		}
	}
}
