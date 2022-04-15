package phx

import (
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
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
}

func NewSocket(endPoint string) *Socket {
	socket := &Socket{
		EndPoint:           endPoint,
		Logger:             NewNoopLogger(),
		ConnectTimeout:     defaultConnectTimeout,
		ReconnectAfterFunc: defaultReconnectAfterFunc,
		HeartbeatInterval:  defaultHeartbeat,
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
}

func (s *Socket) OnConnError(err error) {
	s.Logger.Printf(LogError, "websocket", "Connection error: %s", err)
}

func (s *Socket) OnConnClose() {
	s.Logger.Printf(LogInfo, "websocket", "Disconnected from %v", s.EndPoint)
}

func (s *Socket) OnConnMessage(msg Message) {
	s.Logger.Printf(LogDebug, "websocket", "Received message: %+v", msg)
}
