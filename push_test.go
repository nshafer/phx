package phx

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	mu    sync.Mutex
	state ConnectionState
	sends [][]byte
}

func (f *fakeTransport) Connect(endPoint *url.URL, requestHeader http.Header, connectTimeout time.Duration) error {
	f.state = ConnectionOpen
	return nil
}

func (f *fakeTransport) Disconnect() error {
	f.state = ConnectionClosed
	return nil
}

func (f *fakeTransport) Reconnect() error {
	f.state = ConnectionOpen
	return nil
}

func (f *fakeTransport) IsConnected() bool {
	return f.state == ConnectionOpen
}

func (f *fakeTransport) ConnectionState() ConnectionState {
	return f.state
}

func (f *fakeTransport) Send(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, append([]byte(nil), data...))
	return nil
}

func channelBindingsCount(c *Channel) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.bindings)
}

func TestPushTimeoutUnbinds(t *testing.T) {
	endpoint, err := url.Parse("ws://example.com/socket")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	socket := NewSocket(endpoint)
	socket.Transport = &fakeTransport{state: ConnectionOpen}

	channel := socket.Channel("topic", nil)
	channel.setJoinPush(&Push{Ref: 1, channel: channel})
	channel.PushTimeout = 100 * time.Millisecond

	initialBindings := channelBindingsCount(channel)

	push, err := channel.Push("event", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	if got := channelBindingsCount(channel); got != initialBindings+1 {
		t.Fatalf("expected %d bindings before timeout, got %d", initialBindings+1, got)
	}

	done := make(chan struct{})
	push.Receive("timeout", func(any) {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout callback not called")
	}

	if got := channelBindingsCount(channel); got != initialBindings {
		t.Fatalf("expected bindings restored to %d after timeout, got %d", initialBindings, got)
	}
}
