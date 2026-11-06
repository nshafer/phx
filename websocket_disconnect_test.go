package phx

import (
	"testing"
	"time"
)

// TestDisconnectDuringReconnectReturns reproduces a deadlock between Disconnect and the transport's own reconnect.
// The connection manager is inside closeConn, waiting for a close handshake the server never answers, and will
// take the transport's mutex next. Disconnect used to take that mutex and then send on an unbuffered channel that
// only the (now blocked) connection manager receives, so neither could ever proceed.
func TestDisconnectDuringReconnectReturns(t *testing.T) {
	server := newTestServer(t)
	server.setSilent(true)

	socket := newTestSocket(t, server)
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "socket connected", socket.IsConnected)

	// The manager now sends a close frame and waits up to 2 s for the answer.
	reconnectAt := time.Now()
	if err := socket.Reconnect(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if socket.IsConnected() || time.Since(reconnectAt) > time.Second {
		t.Fatal("missed the close-wait window of the reconnect; the deadlock was not exercised")
	}

	returned := make(chan error, 1)
	go func() { returned <- socket.Disconnect() }()

	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Disconnect: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Disconnect blocked while the transport was reconnecting")
	}

	// The disconnect must still take effect once the reconnect's close handshake has timed out.
	eventually(t, 15*time.Second, "socket closed", func() bool {
		return socket.ConnectionState() == ConnectionClosed
	})
}

// TestDisconnectClosesConnectedSocket covers the plain path: Disconnect on an open connection closes it.
func TestDisconnectClosesConnectedSocket(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "socket connected", socket.IsConnected)
	// Give the reader goroutine (it polls every busyWait) time to start reading, so it sees the server's close reply.
	time.Sleep(2 * busyWait)

	if err := socket.Disconnect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "socket closed", func() bool {
		return socket.ConnectionState() == ConnectionClosed
	})
}
