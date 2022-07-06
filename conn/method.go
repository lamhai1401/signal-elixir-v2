package conn

import (
	"context"
	"fmt"
	"time"

	"github.com/lamhai1401/channel-v2/client"
	"github.com/lamhai1401/gologs/logs"
)

// Debug linter
func (c *Connection) Debug(v ...interface{}) {
	// logs.Debug(fmt.Sprintf("[%s]", c.id), v)
	logs.Debug(c.id, v)
}

// DebugSpew linter
func (c *Connection) DebugSpew(v ...interface{}) {
	// logs.DebugSpew(fmt.Sprintf("[%s]", c.id), v)
	logs.DebugSpew(c.id, v)
}

// Info linter
func (c *Connection) Info(v ...interface{}) {
	// logs.Info(fmt.Sprintf("[%s]", c.id), v)
	logs.Info(c.id, v)
}

// Error linter
func (c *Connection) Error(v ...interface{}) {
	// logs.Error(fmt.Sprintf("[%s]", c.id), v)
	logs.Error(c.id, v)
}

// Warn linter
func (c *Connection) Warn(v ...interface{}) {
	// logs.Warn(fmt.Sprintf("[%s]", c.id), v)
	logs.Warn(c.id, v)
}

// GetParams linter
func (c *Connection) GetParams() map[string]string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.params
}

func (c *Connection) getConn() client.Connection {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.conn
}

func (c *Connection) setConn(conn client.Connection) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.conn = conn
}

func (c *Connection) checkClose() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isClosed
}

func (c *Connection) setClose(state bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.isClosed = state
}

func (c *Connection) getConnectionChannel() string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connectionChannel
}

func (c *Connection) setConnectionChannel(n string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.connectionChannel = n
}

func (c *Connection) getLastResponse(topic string) time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastResponse[topic]
}

func (c *Connection) setLastResponse(topic string, t time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.lastResponse[topic] = t
}

func (c *Connection) getCTX() context.Context {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.ctx
}

func (c *Connection) setCTX(ctx context.Context) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.ctx = ctx
}

func (c *Connection) getCancel() func() {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.cancel
}

func (c *Connection) setCancel(f func()) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.cancel = f
}

func (c *Connection) getSubcribers() map[string]*Subscriber {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.subscribers
}

func (c *Connection) setSubcribers(topic string, state *Subscriber) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.subscribers[topic] = state
}

func (c *Connection) deleteSubcriber(topic string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.subscribers, topic)
}

func (c *Connection) getSubcriber(topic string) *Subscriber {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.subscribers[topic]
}

func (c *Connection) getAllTopics() []string {
	temp := make([]string, 0)
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	for k := range c.subscribers {
		temp = append(temp, k)
	}
	return temp
}

// Send return a ref
func (c *Connection) send(msg *MsgSender) {
	topic := msg.msg.Topic
	user := c.getSubcriber(topic)
	if user == nil {
		msg.result <- fmt.Errorf("%s channel is not exist", topic)
	} else {
		err := user.Chann.PushWithRef(msg.ref, &msg.msg.Event, msg.msg.Payload)
		if err != nil {
			msg.result <- err
		} else {
			msg.result <- fmt.Sprintf("%s send msg success", msg.msg.Topic)
		}
	}
}
