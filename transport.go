package phx

import (
	"net/http"
	"net/url"
	"time"
)

type Transport interface {
	Connect(endPoint url.URL, requestHeader http.Header) error
	Disconnect() error
	Reconnect() error
	IsConnected() bool
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
