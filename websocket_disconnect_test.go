package phx

import (
	"sync/atomic"
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
	var closes int32
	socket.OnClose(func() { atomic.AddInt32(&closes, 1) })
	ws := socket.Transport.(*Websocket)
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "socket connected", socket.IsConnected)

	// The manager now sends a close frame and waits 2 s for an answer that never comes.
	if err := socket.Reconnect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "reconnect waiting for the close handshake", ws.isWaitingForClose)
	inWindow := time.Now()

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
	if time.Since(inWindow) > time.Second {
		t.Skip("too slow to call Disconnect within the reconnect's 2 s close wait; the deadlock was not exercised")
	}

	// The disconnect must still take effect once the reconnect's close handshake has timed out.
	eventually(t, 15*time.Second, "socket closed", func() bool {
		return socket.ConnectionState() == ConnectionClosed
	})
	// The reconnect closed the connection; the disconnect must not close it a second time.
	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&closes); n != 1 {
		t.Fatalf("OnClose callbacks ran %d times, want 1", n)
	}
}

// TestDisconnectRacingReadErrorClosesOnce covers Disconnect racing the reader goroutine: Disconnect has seen an open
// connection and asked the manager to close it, and before the manager acts the reader hits a read error and drops
// the connection (its own reconnect request is refused because the transport is closing). The manager must still
// run the close handling, so the socket's OnClose callbacks fire and its heartbeat stops.
func TestDisconnectRacingReadErrorClosesOnce(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	var closes int32
	socket.OnClose(func() { atomic.AddInt32(&closes, 1) })
	ws := socket.Transport.(*Websocket)
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "socket connected", socket.IsConnected)

	// Interleave both sides under the transport's mutex, so the manager can only look at the connection once both
	// have happened: first what sendClose does, then what the reader does on a read error.
	ws.mu.Lock()
	ws.closing = true
	signal(ws.close)
	conn := ws.conn
	ws.conn = nil
	ws.mu.Unlock()
	_ = conn.Close()

	eventually(t, 5*time.Second, "socket closed", func() bool {
		return socket.ConnectionState() == ConnectionClosed
	})
	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&closes); n != 1 {
		t.Fatalf("OnClose callbacks ran %d times, want 1", n)
	}
}

// TestDisconnectClosesConnectedSocket covers the plain path: Disconnect on an open connection closes it.
func TestDisconnectClosesConnectedSocket(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	var closes int32
	socket.OnClose(func() { atomic.AddInt32(&closes, 1) })
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
	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&closes); n != 1 {
		t.Fatalf("OnClose callbacks ran %d times, want 1", n)
	}
}
