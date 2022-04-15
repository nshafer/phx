package phx

import (
	"net/http"
	"net/url"
	"time"
)

type Transport interface {
	Connect(endPoint url.URL, requestHeader http.Header) error
	IsConnected() bool
	Disconnect()
}

type TransportHandler interface {
	OnConnOpen()
	OnConnClose()
	OnConnError(error)
	OnConnMessage(Message)
	ReconnectAfter(int) time.Duration
}
