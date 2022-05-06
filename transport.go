package phx

import (
	"net/http"
	"net/url"
	"time"
)

type Transport interface {
	Connect(endPoint *url.URL, requestHeader http.Header) error
	Disconnect() error
	Reconnect() error
	IsConnected() bool
	ConnectionState() ConnectionState
	Send([]byte) error
}

type TransportHandler interface {
	onConnOpen()
	onConnClose()
	onConnError(error)
	onWriteError(error)
	onReadError(error)
	onConnMessage([]byte)
	reconnectAfter(int) time.Duration
}
