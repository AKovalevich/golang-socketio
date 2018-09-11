package gosocketio

import (
	"encoding/json"
	"sync"
	"reflect"
	"strconv"
	"strings"
	"log"

	"github.com/AKovalevich/golang-socketio/protocol"
)

const (
	OnConnection    = "connection"
	OnDisconnection = "disconnection"
	OnError         = "error"
)

/**
System handler function for internal event processing
*/
type systemHandler func(c *Channel)

/**
Contains maps of message processing functions
*/
type methods struct {
	messageHandlers     map[string]*caller
	messageHandlersLock sync.RWMutex

	onConnection    systemHandler
	onDisconnection systemHandler
}

/**
create messageHandlers map
*/
func (m *methods) initMethods() {
	m.messageHandlers = make(map[string]*caller)
}

/**
Add message processing function, and bind it to given method
*/
func (m *methods) On(method string, f interface{}) error {
	c, err := newCaller(f)
	if err != nil {
		return err
	}

	m.messageHandlersLock.Lock()
	defer m.messageHandlersLock.Unlock()
	m.messageHandlers[method] = c

	return nil
}

/**
Find message processing function associated with given method
*/
func (m *methods) findMethod(method string) (*caller, bool) {
	m.messageHandlersLock.RLock()
	defer m.messageHandlersLock.RUnlock()

	f, ok := m.messageHandlers[method]
	return f, ok
}

func (m *methods) callLoopEvent(c *Channel, event string) {
	if m.onConnection != nil && event == OnConnection {
		m.onConnection(c)
	}
	if m.onDisconnection != nil && event == OnDisconnection {
		m.onDisconnection(c)
	}

	f, ok := m.findMethod(event)
	if !ok {
		return
	}

	f.callFunc(c, &struct{}{})
}

/**
Check incoming message
On ack_resp - look for waiter
On ack_req - look for processing function and send ack_resp
On emit - look for processing function
*/
func (m *methods) processIncomingMessage(c *Channel, msg *protocol.Message) {
	switch msg.Type {
	case protocol.MessageTypeEmit:
		f, ok := m.findMethod(msg.Method)
		if !ok {
			log.Printf("Method %s not found", msg.Method)
			return
		}

		if !f.ArgsPresent {
			f.callFunc(c, &struct{}{})
			return
		}

		data := f.getArgs()
		args := strings.Trim(msg.Args, "[,],\n")
		argsString, err := strconv.Unquote(args)
		if err != nil {
			log.Printf(err.Error())
			return
		}
		err = json.Unmarshal([]byte(argsString), &data)
		if err != nil {
			log.Printf(err.Error())
			return
		}

		f.callFunc(c, data)

	case protocol.MessageTypeAckRequest:
		f, ok := m.findMethod(msg.Method)
		if !ok || !f.Out {
			return
		}

		var result []reflect.Value
		if f.ArgsPresent {
			//data type should be defined for unmarshall
			data := f.getArgs()
			argsString, err := strconv.Unquote(string(msg.Args))
			if err != nil {
				log.Printf(err.Error())
				return
			}
			err = json.Unmarshal([]byte(argsString), &data)
			if err != nil {
				log.Printf(err.Error())
				return
			}

			result = f.callFunc(c, data)
		} else {
			result = f.callFunc(c, &struct{}{})
		}

		ack := &protocol.Message{
			Type:  protocol.MessageTypeAckResponse,
			AckId: msg.AckId,
		}
		message, err := json.Marshal(result[0].Interface())
		if err != nil {
			log.Printf(err.Error())
			return
		}
		send(ack, c, string(message))

	case protocol.MessageTypeAckResponse:
		waiter, err := c.ack.getWaiter(msg.AckId)
		if err == nil {
			waiter <- msg.Args
		} else {
			log.Printf(err.Error())
		}
	}
}
