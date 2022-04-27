package phx

import (
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Socket struct {
	EndPoint           *url.URL
	Params             map[string]string
	RequestHeader      http.Header
	Transport          Transport
	Logger             Logger
	ConnectTimeout     time.Duration
	ReconnectAfterFunc func(tries int) time.Duration
	HeartbeatInterval  time.Duration

	// miscellaneous private members
	ref              *atomicRef
	openCallbacks    map[Ref]func()
	closeCallbacks   map[Ref]func()
	errorCallbacks   map[Ref]func(error)
	messageCallbacks map[Ref]func(Message)

	// heartbeat related state
	hbMsg   chan Message
	hbClose chan any
	hbRef   Ref
}

func NewSocket(endPoint *url.URL) *Socket {
	socket := &Socket{
		EndPoint:           endPoint,
		Logger:             NewNoopLogger(),
		ConnectTimeout:     defaultConnectTimeout,
		ReconnectAfterFunc: defaultReconnectAfterFunc,
		HeartbeatInterval:  defaultHeartbeatInterval,
		ref:                newAtomicRef(),
		openCallbacks:      make(map[Ref]func()),
		closeCallbacks:     make(map[Ref]func()),
		errorCallbacks:     make(map[Ref]func(error)),
		messageCallbacks:   make(map[Ref]func(Message)),
	}
	socket.Transport = NewWebsocket(websocket.DefaultDialer, socket)
	return socket
}

func (s *Socket) Connect() error {
	err := s.Transport.Connect(s.EndPoint, s.RequestHeader)
	if err != nil {
		s.Logger.Println(LogError, "socket", err)
		return err
	}

	return nil
}

func (s *Socket) Disconnect() error {
	err := s.Transport.Disconnect()
	if err != nil {
		s.Logger.Println(LogError, "socket", err)
		return err
	}

	return nil
}

func (s *Socket) Reconnect() error {
	err := s.Transport.Reconnect()
	if err != nil {
		s.Logger.Println(LogError, "socket", err)
		return err
	}

	return nil
}

func (s *Socket) IsConnected() bool {
	return s.Transport != nil && s.Transport.IsConnected()
}

func (s *Socket) ConnectionState() ConnectionState {
	if s.Transport != nil {
		return s.Transport.ConnectionState()
	} else {
		return ConnectionClosed

	}
}

func (s *Socket) Push(topic string, event Event, payload any, joinRef Ref) Ref {
	ref := s.MakeRef()

	s.PushMessage(Message{
		Topic:   topic,
		Event:   event,
		Payload: payload,
		Ref:     ref,
		JoinRef: joinRef,
	})

	return ref
}

func (s *Socket) PushMessage(msg Message) {
	s.Logger.Printf(LogDebug, "push", "Sending message %+v", msg)
	s.Transport.Send(msg)
}

func (s *Socket) MakeRef() Ref {
	return s.ref.nextRef()
}

func (s *Socket) OnOpen(callback func()) Ref {
	ref := s.MakeRef()
	s.openCallbacks[ref] = callback
	return ref
}

func (s *Socket) OnClose(callback func()) Ref {
	ref := s.MakeRef()
	s.closeCallbacks[ref] = callback
	return ref
}

func (s *Socket) OnError(callback func(error)) Ref {
	ref := s.MakeRef()
	s.errorCallbacks[ref] = callback
	return ref
}

func (s *Socket) OnMessage(callback func(Message)) Ref {
	ref := s.MakeRef()
	s.messageCallbacks[ref] = callback
	return ref
}

func (s *Socket) Off(ref Ref) {
	_, ok := s.openCallbacks[ref]
	if ok {
		delete(s.openCallbacks, ref)
		return
	}

	_, ok = s.closeCallbacks[ref]
	if ok {
		delete(s.closeCallbacks, ref)
		return
	}

	_, ok = s.errorCallbacks[ref]
	if ok {
		delete(s.errorCallbacks, ref)
		return
	}

	_, ok = s.messageCallbacks[ref]
	if ok {
		delete(s.messageCallbacks, ref)
		return
	}
}

// implements TransportHandler

func (s *Socket) reconnectAfter(tries int) time.Duration {
	return s.ReconnectAfterFunc(tries)
}

func (s *Socket) onConnOpen() {
	s.Logger.Printf(LogInfo, "transport", "Connected to %v", s.EndPoint)
	s.startHeartbeat()
	for _, cb := range s.openCallbacks {
		cb()
	}
}

func (s *Socket) onConnClose() {
	s.Logger.Printf(LogInfo, "transport", "Disconnected from %v", s.EndPoint)
	s.stopHeartbeat()
	for _, cb := range s.closeCallbacks {
		cb()
	}
}

func (s *Socket) callErrorCallbacks(err error) {
	for _, cb := range s.errorCallbacks {
		cb(err)
	}
}

func (s *Socket) onConnError(err error) {
	s.Logger.Printf(LogError, "transport", "Connection error: %s", err)
	s.callErrorCallbacks(err)
}

func (s *Socket) onWriteError(err error) {
	s.Logger.Printf(LogError, "transport", "Write error: %s", err)
	s.callErrorCallbacks(err)
}

func (s *Socket) onReadError(err error) {
	// Don't log errors when the connection was closed
	if !strings.Contains(err.Error(), "use of closed network connection") {
		s.Logger.Printf(LogError, "transport", "Read error: %s", err)
		s.callErrorCallbacks(err)
	}
}

func (s *Socket) onConnMessage(msg Message) {
	s.Logger.Printf(LogDebug, "transport", "Received message: %+v", msg)

	if msg.Topic == "phoenix" && msg.Ref == s.hbRef {
		// Send this message to the heartbeat goroutine
		s.hbMsg <- msg
		return
	}

	for _, cb := range s.messageCallbacks {
		cb(msg)
	}
}

/*
 * Heartbeat related functionality
 */
func (s *Socket) startHeartbeat() {
	s.hbClose = make(chan any)
	s.hbMsg = make(chan Message)
	go s.heartbeat()
}

func (s *Socket) stopHeartbeat() {
	close(s.hbClose)
	close(s.hbMsg)
}

func (s *Socket) heartbeat() {
	s.Logger.Println(LogDebug, "heartbeat", "heartbeat goroutine started")

	defer func() {
		s.Logger.Println(LogDebug, "heartbeat", "heartbeat goroutine stopped")
	}()

	s.hbRef = 0

	for {
		timer := time.NewTimer(s.HeartbeatInterval)

		select {
		case <-s.hbClose:
			timer.Stop()
			return
		case msg := <-s.hbMsg:
			s.Logger.Println(LogDebug, "heartbeat", "Got heartbeat message", msg)
			timer.Stop()
			s.hbRef = 0
		case <-timer.C:
			if !s.Transport.IsConnected() {
				continue
			}
			if s.hbRef == 0 {
				s.hbRef = s.MakeRef()
				s.Logger.Println(LogDebug, "heartbeat", "Sending heartbeat", s.hbRef)
				s.PushMessage(Message{Topic: "phoenix", Event: HeartBeatEvent, Payload: nil, Ref: s.hbRef})
			} else {
				s.Logger.Println(LogDebug, "heartbeat", "heartbeat timeout")
				_ = s.Transport.Reconnect()
			}
		}
	}
}
