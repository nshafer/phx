package phx

import (
	"testing"
	"time"
)

// TestHeartbeatSurvivesReconnects reconnects a socket a few times. Each connection must keep sending heartbeats and
// processing their replies (a missing reply makes the socket reconnect). Run with -race: every reconnect stops the
// heartbeat goroutine of the previous connection and starts a new one while the reader goroutine routes heartbeat
// replies to it.
//
// Note: without -race this test passes on the unfixed code too; the race detector is what catches the bug.
func TestHeartbeatSurvivesReconnects(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	// Above busyWait, which bounds how fast the writer and reader goroutines pick up a fresh connection.
	socket.HeartbeatInterval = 3 * busyWait / 2
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	server.nextConn(t)

	for i := 0; i < 3; i++ {
		before := server.heartbeatCount()
		eventually(t, 5*time.Second, "heartbeats on the connection", func() bool {
			return server.heartbeatCount() >= before+2
		})
		if err := socket.Reconnect(); err != nil {
			t.Fatal(err)
		}
		server.nextConn(t)
		eventually(t, 5*time.Second, "socket reconnected", socket.IsConnected)
	}
}
