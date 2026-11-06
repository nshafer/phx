package phx

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestServerDropWhileWritingReconnects covers the server dropping a connection the client is writing on. The writer
// goroutine uses the transport's connection while the reader, on its read error, clears it and the connection
// manager closes it and dials a new one. Every access to the connection must be ordered by the transport's mutex,
// which the race detector checks when the test runs with -race. It drives the transport without a Socket, so the
// Socket's own heartbeat goroutines are not part of it.
func TestServerDropWhileWritingReconnects(t *testing.T) {
	server := newTestServer(t)
	ws := NewWebsocket(connRaceHandler{})
	ws.Dialer = &websocket.Dialer{}
	if err := ws.Connect(server.URL(), nil, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { go func() { _ = ws.Disconnect() }() })
	conn := server.nextConn(t)

	// Keep the client writing while the server drops the connection.
	stop := make(chan struct{})
	writing := make(chan struct{})
	go func() {
		defer close(writing)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = ws.Send([]byte(`[null,null,"room:lobby","ping",{}]`))
		}
	}()
	defer func() {
		close(stop)
		select {
		case <-writing:
		case <-time.After(5 * time.Second):
			t.Error("client writer did not stop")
		}
	}()

	// Each drop is a chance for the race detector to see unordered accesses, so drop a few times. Every time the
	// client must reconnect and keep writing on the new connection.
	for drop := 0; drop < 5; drop++ {
		if f := conn.next(t); f.Event != "ping" {
			t.Fatalf("server got %q after %d drops, want ping", f.Event, drop)
		}
		_ = conn.ws.Close()
		conn = server.nextConn(t)
	}
	if f := conn.next(t); f.Event != "ping" {
		t.Fatalf("server got %q after the last reconnect, want ping", f.Event)
	}
}

// connRaceHandler is a TransportHandler that ignores all activity and reconnects quickly.
type connRaceHandler struct{}

func (connRaceHandler) onConnOpen()                      {}
func (connRaceHandler) onConnClose()                     {}
func (connRaceHandler) onConnError(error)                {}
func (connRaceHandler) onWriteError(error)               {}
func (connRaceHandler) onReadError(error)                {}
func (connRaceHandler) onConnMessage([]byte)             {}
func (connRaceHandler) reconnectAfter(int) time.Duration { return 10 * time.Millisecond }
