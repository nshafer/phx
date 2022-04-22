package phx

import (
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Socket struct {
	// implements TransportHandler
	EndPoint           string
	Params             map[string]string
	RequestHeader      http.Header
	Transport          Transport
	Logger             Logger
	ConnectTimeout     time.Duration
	ReconnectAfterFunc func(tries int) time.Duration
	HeartbeatInterval  time.Duration

	ref *atomicRef

	// heartbeat related state
	hbMsg   chan Message
	hbClose chan any
	hbRef   Ref
}

func NewSocket(endPoint string) *Socket {
	socket := &Socket{
		EndPoint:           endPoint,
		Logger:             NewNoopLogger(),
		ConnectTimeout:     defaultConnectTimeout,
		ReconnectAfterFunc: defaultReconnectAfterFunc,
		HeartbeatInterval:  defaultHeartbeatInterval,
		ref:                newAtomicRef(),
	}
	socket.Transport = NewWebsocket(websocket.DefaultDialer, socket)
	return socket
}

func (s *Socket) Connect() error {
	endPointUrl, err := url.Parse(s.EndPoint)
	if err != nil {
		s.Logger.Println(LogError, "socket", err)
		return err
	}
	err = s.Transport.Connect(*endPointUrl, s.RequestHeader)
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

func (s *Socket) Push(msg Message) {
	s.Logger.Printf(LogDebug, "push", "Sending message %+v", msg)
	s.Transport.Send(msg)
}

/*
 * TransportHandler callback methods
 */

func (s *Socket) reconnectAfter(tries int) time.Duration {
	return s.ReconnectAfterFunc(tries)
}

func (s *Socket) onConnOpen() {
	s.Logger.Printf(LogInfo, "transport", "Connected to %v", s.EndPoint)
	s.startHeartbeat()
}

func (s *Socket) onConnError(err error) {
	s.Logger.Printf(LogError, "transport", "Connection error: %s", err)
}

func (s *Socket) onWriteError(err error) {
	s.Logger.Printf(LogError, "transport", "Write error: %s", err)
}

func (s *Socket) onReadError(err error) {
	// Don't log errors when the connection was closed
	if !strings.Contains(err.Error(), "use of closed network connection") {
		s.Logger.Printf(LogError, "transport", "Read error: %s", err)
	}
}

func (s *Socket) onConnClose() {
	s.Logger.Printf(LogInfo, "transport", "Disconnected from %v", s.EndPoint)
	s.stopHeartbeat()
}

func (s *Socket) onConnMessage(msg Message) {
	s.Logger.Printf(LogDebug, "transport", "Received message: %+v", msg)

	if msg.Topic == "phoenix" && msg.Ref == s.hbRef {
		// Send this message to the heartbeat goroutine
		s.hbMsg <- msg
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
				s.hbRef = s.ref.nextRef()
				s.Logger.Println(LogDebug, "heartbeat", "Sending heartbeat", s.hbRef)
				s.Push(Message{Topic: "phoenix", Event: HeartBeatEvent, Payload: nil, Ref: s.hbRef})
			} else {
				s.Logger.Println(LogDebug, "heartbeat", "heartbeat timeout")
				_ = s.Transport.Reconnect()
			}
		}
	}
}
