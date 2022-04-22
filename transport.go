package phx

import (
	"net/http"
	"net/url"
	"time"
)

type Transport interface {
	Connect(endPoint url.URL, requestHeader http.Header) error
	Disconnect() error
	IsConnected() bool
	Send(msg Message)
}

type TransportHandler interface {
	OnConnOpen()
	OnConnClose()
	OnConnError(error)
	OnWriteError(error)
	OnReadError(error)
	OnConnMessage(Message)
	ReconnectAfter(int) time.Duration
}
