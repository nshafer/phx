package phx

import (
	"sync"
	"testing"
	"time"
)

type nopHandler struct{}

func (nopHandler) onConnOpen()                      {}
func (nopHandler) onConnClose()                     {}
func (nopHandler) onConnError(error)                {}
func (nopHandler) onWriteError(error)               {}
func (nopHandler) onReadError(error)                {}
func (nopHandler) onConnMessage([]byte)             {}
func (nopHandler) reconnectAfter(int) time.Duration { return time.Hour }

// newStartedWebsocket builds a Websocket in the same state startup() leaves it
// in, but without the connection goroutines, so the internal signalling can be
// exercised deterministically.
func newStartedWebsocket() *Websocket {
	w := NewWebsocket(nopHandler{})
	w.done = make(chan any)
	w.close = make(chan bool)
	w.closeMsg = make(chan bool)
	w.reconnect = make(chan bool)
	w.send = make(chan []byte, messageQueueLength)
	w.setStarted(true)
	return w
}

// drain stands in for connectionManager's select loop: it consumes signal
// sends until the websocket is shut down.
func drain(w *Websocket, ch chan bool) {
	for {
		select {
		case <-ch:
		case <-w.done:
			return
		}
	}
}

// TestSendReconnectDuringShutdownDoesNotPanic reproduces the crash observed in
// production: connectionReader handling a read error calls sendReconnect()
// concurrently with shutdown() closing the internal channels, panicking with
// "send on closed channel" and killing the whole process.
func TestSendReconnectDuringShutdownDoesNotPanic(t *testing.T) {
	for i := 0; i < 10000; i++ {
		w := newStartedWebsocket()
		go drain(w, w.reconnect)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			w.sendReconnect()
		}()
		go func() {
			defer wg.Done()
			w.shutdown()
		}()
		wg.Wait()
	}
}

// TestSendCloseDuringShutdownDoesNotPanic covers the same race for the close
// signal path (Disconnect racing an internal shutdown).
func TestSendCloseDuringShutdownDoesNotPanic(t *testing.T) {
	for i := 0; i < 10000; i++ {
		w := newStartedWebsocket()
		go drain(w, w.close)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			w.sendClose()
		}()
		go func() {
			defer wg.Done()
			w.shutdown()
		}()
		wg.Wait()
	}
}

// TestSendAfterShutdownReturnsError ensures a Send after shutdown fails
// cleanly instead of panicking or blocking forever.
func TestSendAfterShutdownReturnsError(t *testing.T) {
	w := newStartedWebsocket()
	w.shutdown()

	errCh := make(chan error, 1)
	go func() { errCh <- w.Send([]byte("hello")) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error sending after shutdown, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked after shutdown")
	}
}

// TestShutdownIsIdempotent guards against a double close of the done channel
// when shutdown is reached twice (e.g. Disconnect racing connectionManager).
func TestShutdownIsIdempotent(t *testing.T) {
	w := newStartedWebsocket()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.shutdown()
		}()
	}
	wg.Wait()
}
