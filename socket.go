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

	ref    *atomicRef
	hbchan chan any
}

func NewSocket(endPoint string) *Socket {
	socket := &Socket{
		EndPoint:           endPoint,
		Logger:             NewNoopLogger(),
		ConnectTimeout:     defaultConnectTimeout,
		ReconnectAfterFunc: defaultReconnectAfterFunc,
		HeartbeatInterval:  defaultHeartbeat,
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
	s.Transport.Disconnect()
	return nil
}

func (s *Socket) IsConnected() bool {
	return s.Transport != nil && s.Transport.IsConnected()
}

func (s *Socket) ReconnectAfter(tries int) time.Duration {
	return s.ReconnectAfterFunc(tries)
}

func (s *Socket) OnConnOpen() {
	s.Logger.Printf(LogInfo, "websocket", "Connected to %v", s.EndPoint)
	s.startHeartbeat()
}

func (s *Socket) OnConnError(err error) {
	s.Logger.Printf(LogError, "websocket", "Connection error: %s", err)
}

func (s *Socket) OnWriteError(err error) {
	s.Logger.Printf(LogError, "websocket", "Write error: %s", err)
}

func (s *Socket) OnReadError(err error) {
	// Don't log errors when the connection was closed
	if !strings.Contains(err.Error(), "use of closed network connection") {
		s.Logger.Printf(LogError, "websocket", "Read error: %s", err)
	}
}

func (s *Socket) OnConnClose() {
	s.Logger.Printf(LogInfo, "websocket", "Disconnected from %v", s.EndPoint)
	s.stopHeartbeat()
}

func (s *Socket) OnConnMessage(msg Message) {
	s.Logger.Printf(LogDebug, "websocket", "Received message: %+v", msg)
}

func (s *Socket) startHeartbeat() {
	s.hbchan = make(chan any)
	go s.heartbeat()
}

func (s *Socket) stopHeartbeat() {
	close(s.hbchan)
}

func (s *Socket) heartbeat() {
	s.Logger.Println(LogDebug, "heartbeat", "heartbeat goroutine started")
	ticker := time.NewTicker(s.HeartbeatInterval)
	defer func() {
		ticker.Stop()
		s.Logger.Println(LogDebug, "heartbeat", "heartbeat goroutine stopped")
	}()

	for {
		select {
		case <-s.hbchan:
			return
		case <-ticker.C:
			if !s.Transport.IsConnected() {
				time.Sleep(busyWait)
				continue
			}
			//w.socket.Logger.Printf(LogDebug, "websocket", "Sending heartbeat")
			ref := s.ref.nextRef()
			s.Logger.Println(LogDebug, "heartbeat", "Sending heartbeat", ref)
			s.Transport.Send(Message{Topic: "phoenix", Event: "heartbeat", Payload: nil, Ref: ref})
		}
	}
}
