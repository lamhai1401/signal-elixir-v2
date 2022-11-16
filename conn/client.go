package conn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/lamhai1401/channel-v2/client"
	"github.com/lamhai1401/gologs/logs"
)

// ErrTimeOut linter
var ErrTimeOut = fmt.Errorf("connection Timeout")

const (
	maxMsgChann   = 1024 * 2
	errCtxTimeout = "context deadline exceeded"
	errCtxCancel  = "context canceled"
)

// Result linter
type Result struct {
	Response *Resp  `json:"response"`
	Status   string `json:"status"`
}

// Resp linter
type Resp struct {
	Reason string `json:"reason"`
}

// MsgSender linter
type MsgSender struct {
	ref    *string
	msg    *client.Message
	result chan interface{}
}

// Subscriber save all subcriber info
type Subscriber struct {
	Topic        string // channel name
	NeedSendping bool   // need to sendping or not
	Chann        *client.Chan
}

func JsonParsing(source interface{}) func(dest interface{}) error {
	return func(dest interface{}) error {
		bytes, err := json.Marshal(source)
		if err != nil {
			return err
		}

		err = json.Unmarshal(bytes, &dest)
		if err != nil {
			return err
		}
		bytes = nil
		return nil
	}
}

// Connection linter
type Connection struct {
	nodeID            string                 // nodeID to verify
	connectionChannel string                 // to handle connection state so that reconnect because the heartbeat in lib not good for check alive
	token             string                 // for auth
	id                string                 // for log with id
	url               string                 // connect url
	conn              client.Connection      // elixir connection
	sendMsgChann      chan *MsgSender        // to send msg chann
	params            map[string]string      // connection params
	lastResponse      map[string]time.Time   // save to last msg response to check heng request for each topic
	receiveMsgChann   chan *client.Message   // for manager to callback
	subscribers       map[string]*Subscriber // save to list of subscribe channel
	isClosed          bool                   // check close
	ctx               context.Context        // handle close
	cancel            func()                 // for call cancel
	mutex             sync.RWMutex
}

// NewConnection connecting to signal with give url, callback to handle internal data from signal
func NewConnection(id, nodeID, token, url string, params map[string]string) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Connection{
		id:              id,
		url:             url,
		token:           token,
		nodeID:          nodeID,
		sendMsgChann:    make(chan *MsgSender, maxMsgChann),
		receiveMsgChann: make(chan *client.Message, maxMsgChann),
		lastResponse:    make(map[string]time.Time),
		subscribers:     make(map[string]*Subscriber),
		isClosed:        false,
		ctx:             ctx,
		params:          params,
		cancel:          cancel,
	}

	return conn
}

// Start linter
func (c *Connection) Start(sub *Subscriber) error {
	limit := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if limit == 100 {
			str := fmt.Sprintf("Try to connect elixir %d times", limit)
			c.Error(str)
			return fmt.Errorf(str)
		}

		err := c.connect(sub.Topic, c.GetParams())
		if err != nil {
			c.Error("elixir connecting err: ", err.Error())
			limit++
			continue
		}
		break
	}
	return nil
}

// connect to connect to elixer server
func (c *Connection) connect(connectionChannel string, params map[string]string) error {
	var err error

	// set up chann
	tempChann := make(chan int, 1)
	errChann := make(chan error, 1)

	// set up ctx
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(getTimeout())*time.Second)
	defer cancel()

	// set up auth
	values := make(url.Values)
	if c.token != "" {
		values["token"] = []string{c.token}
	}
	for k, v := range params {
		values[k] = []string{v}
	}

	// declare func
	connectFunc := func() {
		connection, err := client.Connect(c.url, values)
		if err != nil {
			// c.Error(fmt.Sprintf("connecting err: %v", err.Error()))
			errChann <- err
			return
		}
		// set successful connection
		c.setConn(connection)
		tempChann <- 1
	}

	ctxFunc := func() {
		<-ctx.Done()
		if ctx.Err().Error() == errCtxCancel {
			return
		}
		if ctx.Err().Error() == errCtxTimeout {
			fmt.Printf("%s signal connect timeout. \n", c.nodeID)
			errChann <- ErrTimeOut
		}
	}

	// start func
	go connectFunc()
	go ctxFunc()

	select {
	case <-tempChann:
		c.Info(fmt.Sprintf("%s Connecting to %s", c.nodeID, c.url))
	case connErr := <-errChann:
		return connErr
	}

	c.setClose(false)

	// subscribe to connectionChannel
	err = c.subcribeConnectionChannel(connectionChannel)
	if err != nil {
		return err
	}

	// start to check last response for keep alive every channel
	timeout := time.Duration(getTimeout()) * time.Second
	interval := time.Duration(getInterval()) * time.Second
	go c.serve(timeout, interval)
	go c.sendLoop()

	return err
}

// subcribeConnectionChannel connection main channel to handle connection error or not
func (c *Connection) subcribeConnectionChannel(id string) error {
	err := c.Subscribe(&Subscriber{
		Topic:        id,
		NeedSendping: true,
	})
	if err != nil {
		return err
	}

	c.setConnectionChannel(id)
	c.Info("Subscribe connection channel", id)
	c.setLastResponse(c.getConnectionChannel(), time.Now())
	return nil
}

// Subscribe linter
func (c *Connection) Subscribe(sub *Subscriber) error {

	callBack := func(channel <-chan *client.Message) {
		var open bool
		var data *client.Message
		cancelFunc := c.getCTX().Done()

		for {
			select {
			case data, open = <-channel:
				if !open {
					c.Warn(fmt.Sprintf("%s channel was closed", sub.Topic))
					return
				}
				c.setLastResponse(sub.Topic, time.Now())
				c.Debug("Receive msg for topic:", sub.Topic)
				c.receiveMsgChann <- data
				data = nil
			case <-cancelFunc:
				c.Warn(fmt.Sprintf("Channel %s was closed because connection", sub.Topic))
				return
			}
		}
	}

	go func() {
		// var respBin []byte
		var resp *client.Message
		var err error
		var ch *client.Chan
		var conn client.Connection
		var data *Result

		connectionTime := 10
		count := 0

		cancel := make(chan int, 1)

		for {
			select {
			case <-cancel:
				logs.Info(fmt.Sprintf("%s subscribe success", sub.Topic))
				return
			default:
				data = nil
				resp = nil
				conn = nil
				err = nil
				ch = nil

				if count == connectionTime {
					logs.Warn(fmt.Sprintf("%s try to subscribe 10 times and not work, call os.exit", sub.Topic))
					os.Exit(0)
				}

				// unsub old topic
				c.UnSubscribe(sub.Topic)

				conn = c.getConn()
				if conn == nil {
					logs.Warn("current connection is nil")
					count++
					continue
				}

				// join new topic
				ch, err = conn.Chan(sub.Topic)
				if err != nil {
					logs.Error(sub.Topic, " subscribe error: ", err.Error())
					count++
					continue
				}

				result, err := ch.Join()
				if err != nil {
					logs.Error(err.Error(), "stop loop subscribe")
					count++
					continue
				}

				// check join result
				resp = <-result.Pull()

				// err = mapstructure.Decode(resp.Payload, &data)
				err = JsonParsing(resp.Payload)(&data)
				if err != nil {
					logs.Error("parsing error", err.Error())
					count++
					continue
				}

				switch data.Status {
				case "ok":
					// save connection channel to subcriber
					c.setLastResponse(sub.Topic, time.Now())
					sub.Chann = ch
					c.setSubcribers(sub.Topic, sub)
					go callBack(ch.OnMessage().Pull())
					cancel <- 1
				case "error":
					logs.Error(fmt.Sprintf("join topic (%s) err: %v", sub.Topic, data.Response))
					if data.Response.Reason == "unauthorized" {
						logs.Warn("join room unauthorized, call os.exit")
						os.Exit(0)
					}
					count++
					continue
				default:
					count++
					continue
				}
			}
		}
	}()

	return nil
}

// UnSubscribe linter
func (c *Connection) UnSubscribe(topic string) {
	if user := c.getSubcriber(topic); user != nil {
		c.deleteSubcriber(topic)
		puller, err := user.Chann.Leave()
		if err != nil {
			c.Error(fmt.Sprintf("UnSubscribe topic %s err: %s", topic, err.Error()))
		} else {
			defer puller.Close()
			c.Info("Leave topic ", topic, <-puller.Pull())
		}
	}
}

func (c *Connection) sendLoop() {
	cancel := c.getCTX().Done()
	for {
		select {
		case msg := <-c.sendMsgChann:
			c.send(msg)
		case <-cancel:
			c.Warn("send loop to keep send msg was closed")
			return
		}
	}
}

func (c *Connection) serve(timeout, interval time.Duration) {
	ticker := time.NewTicker(interval)
	logs.Debug(fmt.Sprintf("Starting keepAlive with timeout/interval (%v/%v)", timeout/time.Second, interval/time.Second))
	cancel := c.getCTX().Done()

	for {
		select {
		case <-ticker.C:
			// send ping interval
			go c.processSendPing(c.getAllTopics(), timeout)
		case msg := <-c.sendMsgChann:
			go c.send(msg)
		case <-cancel:
			c.Warn("serve loop to keep alive was closed")
			return
		}
	}
}

func (c *Connection) processSendPing(lst []string, timeOut time.Duration) {
	for _, topic := range lst {
		// get sub info
		info := c.getSubcriber(topic)
		if info == nil {
			continue
		}

		// check need send ping
		if !info.NeedSendping {
			logs.Debug(topic, "Dont need to send ping")
			continue
		}

		if err := c.sendPing(topic); err != nil {
			c.Error(fmt.Errorf("ping to %s channel error: %s", topic, err.Error()))
			if topic == c.getConnectionChannel() {
				c.onPingErr(c.nodeID)
				return
			}
			c.Info(topic, "send ping error: ", err.Error())
		} else {
			c.Info(fmt.Sprintf("send ping to %s channel at: %v", topic, time.Now().UTC()))
		}

		// check last response, if good send ping, else re-subcribes
		resp := c.getLastResponse(topic)
		if resp.Nanosecond() == 0 {
			c.Warn(fmt.Sprintf("%s topic channel have not last response", topic))
		} else if time.Since(resp) > timeOut {
			// check if it is node channel, close elixir connection, the reconnect will be fire at line 162
			if topic == c.getConnectionChannel() {
				defer c.onPingErr(c.nodeID)
				return
			}

			// re-john channel again
			err := c.Subscribe(info)
			if err != nil {
				c.Error(topic, "join channel err", err.Error())
			} else {
				c.Info(topic, "join channel success")
			}
		}
	}
}

func (c *Connection) onPingErr(topic string) {
	if !c.checkClose() {
		c.Reconnect(c.GetParams())
		return
	}
	c.Warn(topic, "Elixir was close by user. No need to reconnect")
}

func (c *Connection) sendPing(topic string) error {
	msg := &client.Message{
		Topic:   topic,
		Event:   "ping",
		Payload: map[string]string{"topic": topic, "event": "ping"},
	}

	chann := c.Send(c.GetRef(&topic), msg)
	result := <-chann
	switch v := result.(type) {
	case error:
		return v
	default:
		return nil
	}
}

// Send return a ref
func (c *Connection) Send(ref *string, msg *client.Message) chan interface{} {
	result := make(chan interface{}, 1)
	c.sendMsgChann <- &MsgSender{
		ref:    ref,
		msg:    msg,
		result: result,
	}
	return result
}

// GetRef return a ref
func (c *Connection) GetRef(topic *string) *string {
	var ref string
	if user := c.getSubcriber(*topic); user != nil {
		ref = user.Chann.GetRef()
	}
	return &ref
}

// Reconnect linter
func (c *Connection) Reconnect(params map[string]string) error {
	// close connection
	c.Close()

	// restart
	if err := c.Start(&Subscriber{
		Topic:        c.getConnectionChannel(),
		NeedSendping: true,
	}); err != nil {
		return err
	}

	return nil
}

// Close linter
func (c *Connection) Close() {
	if !c.checkClose() {
		c.setClose(true)
		c.cancel()                                              // signal to all loop return
		ctx, cancel := context.WithCancel(context.Background()) // add new ctx
		c.setCTX(ctx)                                           // setting new context to avoid channel ctx.done() always return value
		c.setCancel(cancel)                                     // setting new context cancel func so that can call it again
	}
	if conn := c.getConn(); conn != nil {
		c.setConn(nil)
		err := conn.Close()
		if err != nil {
			c.Error("Close elixir connection err: ", err.Error())
		}
	}
}

// Reading linter
func (c *Connection) Reading() chan *client.Message {
	return c.receiveMsgChann
}
