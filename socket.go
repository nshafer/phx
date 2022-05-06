package phx

import (
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// quick toggle while debugging
const startHeartbeat = true

type Socket struct {
	EndPoint           *url.URL
	RequestHeader      http.Header
	Transport          Transport
	Logger             Logger
	ConnectTimeout     time.Duration
	ReconnectAfterFunc func(tries int) time.Duration
	HeartbeatInterval  time.Duration
	Serializer         Serializer

	// miscellaneous private members
	refGenerator     *atomicRef
	openCallbacks    map[Ref]func()
	closeCallbacks   map[Ref]func()
	errorCallbacks   map[Ref]func(error)
	messageCallbacks map[Ref]func(Message)

	// Channel related state
	channels []*Channel

	// heartbeat related state
	hbMsg   chan *Message
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
		Serializer:         NewJSONSerializerV2(),
		refGenerator:       newAtomicRef(),
		openCallbacks:      make(map[Ref]func()),
		closeCallbacks:     make(map[Ref]func()),
		errorCallbacks:     make(map[Ref]func(error)),
		messageCallbacks:   make(map[Ref]func(Message)),
		channels:           make([]*Channel, 0),
	}
	socket.Transport = NewWebsocket(websocket.DefaultDialer, socket)
	return socket
}

func (s *Socket) Connect() error {
	// Add the 'vsn' query parameter to the connection url
	q := s.EndPoint.Query()
	q.Set("vsn", s.Serializer.vsn())
	s.EndPoint.RawQuery = q.Encode()

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

func (s *Socket) IsConnectedOrConnecting() bool {
	return s.Transport != nil &&
		(s.Transport.ConnectionState() == ConnectionConnecting || s.Transport.ConnectionState() == ConnectionOpen)
}

func (s *Socket) ConnectionState() ConnectionState {
	if s.Transport != nil {
		return s.Transport.ConnectionState()
	} else {
		return ConnectionClosed

	}
}

func (s *Socket) Push(topic string, event Event, payload any, joinRef Ref) (Ref, error) {
	ref := s.MakeRef()

	err := s.PushMessage(Message{
		Topic:   topic,
		Event:   event,
		Payload: payload,
		Ref:     ref,
		JoinRef: joinRef,
	})

	return ref, err
}

func (s *Socket) PushMessage(msg Message) error {
	data, err := s.Serializer.encode(&msg)
	if err != nil {
		return err
	}

	err = s.Transport.Send(data)
	if err != nil {
		return err
	}

	s.Logger.Printf(LogDebug, "push", "Sent message %+v", msg)
	return nil
}

func (s *Socket) MakeRef() Ref {
	return s.refGenerator.nextRef()
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

func (s *Socket) Channel(topic string, params map[string]string) *Channel {
	channel := NewChannel(topic, params, s)
	s.channels = append(s.channels, channel)
	return channel
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
	s.Logger.Printf(LogInfo, "socket", "Disconnected from %v", s.EndPoint)
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
	s.Logger.Printf(LogError, "socket", "Connection error: %s", err)
	s.callErrorCallbacks(err)
}

func (s *Socket) onWriteError(err error) {
	s.Logger.Printf(LogError, "socket", "Write error: %s", err)
	s.callErrorCallbacks(err)
}

func (s *Socket) onReadError(err error) {
	// Don't log errors when the connection was closed
	if !strings.Contains(err.Error(), "use of closed network connection") {
		s.Logger.Printf(LogError, "socket", "Read error: %s", err)
		s.callErrorCallbacks(err)
	}
}

func (s *Socket) onConnMessage(data []byte) {
	msg, err := s.Serializer.decode(data)
	if err != nil {
		s.Logger.Println(LogError, "socket", "could not decode data to Message:", err)
		return
	}

	s.Logger.Printf(LogDebug, "socket", "Received message: %+v", msg)

	if msg.Topic == "phoenix" && msg.Ref == s.hbRef {
		// Send this message to the heartbeat goroutine
		s.hbMsg <- msg
		return
	}

	for _, cb := range s.messageCallbacks {
		cb(*msg)
	}

	for _, channel := range s.channels {
		channel.process(msg)
	}
}

/*
 * Heartbeat related functionality
 */
func (s *Socket) startHeartbeat() {
	s.hbClose = make(chan any)
	s.hbMsg = make(chan *Message)
	if startHeartbeat {
		go s.heartbeat()
	}
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
				err := s.PushMessage(Message{Topic: "phoenix", Event: HeartBeatEvent, Payload: nil, Ref: s.hbRef})
				if err != nil {
					s.Logger.Println(LogError, "heartbeat", "Error when sending heartbeat", err)
				}
			} else {
				s.Logger.Println(LogDebug, "heartbeat", "heartbeat timeout")
				_ = s.Transport.Reconnect()
			}
		}
	}
}
