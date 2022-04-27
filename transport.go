package phx

import (
	"net/http"
	"net/url"
	"time"
)

type ConnectionState int

const (
	ConnectionConnecting ConnectionState = iota
	ConnectionOpen
	ConnectionClosing
	ConnectionClosed
)

func (s ConnectionState) String() string {
	switch s {
	case ConnectionConnecting:
		return "connecting"
	case ConnectionOpen:
		return "open"
	case ConnectionClosing:
		return "closing"
	case ConnectionClosed:
		return "closed"
	}
	return "unknown"
}

type Transport interface {
	Connect(endPoint *url.URL, requestHeader http.Header) error
	Disconnect() error
	Reconnect() error
	IsConnected() bool
	ConnectionState() ConnectionState
	Send(msg Message)
}

type TransportHandler interface {
	onConnOpen()
	onConnClose()
	onConnError(error)
	onWriteError(error)
	onReadError(error)
	onConnMessage(Message)
	reconnectAfter(int) time.Duration
}
