// This file is the shared test helper of a series of independent fixes (Disconnect deadlock, push reply binding
// race, heartbeat race, ordered channel callbacks). It is kept identical on every branch of the series so they merge
// cleanly in any order, so a branch may leave some helpers (heartbeatCount, setSilent, ...) unused.

package phx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testServer is a minimal Phoenix Channels server (JSON serializer v2) for tests. It answers joins, leaves and
// heartbeats itself and hands every other client frame to the test through testConn.frames.
type testServer struct {
	t       *testing.T
	srv     *httptest.Server
	conns   chan *testConn
	release chan struct{}

	mu         sync.Mutex
	silent     bool
	heartbeats int
}

// testConn is the server side of one client connection.
type testConn struct {
	ws     *websocket.Conn
	wmu    sync.Mutex
	frames chan testFrame
}

// testFrame is one Phoenix V2 frame: [join_ref, ref, topic, event, payload].
type testFrame struct {
	JoinRef string
	Ref     string
	Topic   string
	Event   string
	Payload any
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	s := &testServer{
		t:       t,
		conns:   make(chan *testConn, 64),
		release: make(chan struct{}),
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		s.mu.Lock()
		silent := s.silent
		s.mu.Unlock()

		if silent {
			// Never read: gorilla only answers a close frame while reading, so the client's close handshake is
			// never acknowledged.
			<-s.release
			return
		}

		c := &testConn{ws: ws, frames: make(chan testFrame, 1000)}
		select {
		case s.conns <- c:
		default:
		}
		s.serve(c)
	}))
	t.Cleanup(func() {
		close(s.release)
		s.srv.CloseClientConnections()
		s.srv.Close()
	})
	return s
}

// URL is the socket endpoint to give to NewSocket.
func (s *testServer) URL() *url.URL {
	u, _ := url.Parse("ws" + strings.TrimPrefix(s.srv.URL, "http") + "/socket")
	return u
}

// setSilent makes connections accepted from now on never read nor answer anything.
func (s *testServer) setSilent(silent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.silent = silent
}

func (s *testServer) heartbeatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeats
}

// nextConn waits for the next (non-silent) client connection.
func (s *testServer) nextConn(t *testing.T) *testConn {
	t.Helper()
	select {
	case c := <-s.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no client connected to the test server")
		return nil
	}
}

func (s *testServer) serve(c *testConn) {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil || len(raw) != 5 {
			s.t.Errorf("test server: bad frame %s", data)
			return
		}
		var f testFrame
		_ = json.Unmarshal(raw[0], &f.JoinRef)
		_ = json.Unmarshal(raw[1], &f.Ref)
		_ = json.Unmarshal(raw[2], &f.Topic)
		_ = json.Unmarshal(raw[3], &f.Event)
		_ = json.Unmarshal(raw[4], &f.Payload)

		switch f.Event {
		case string(HeartBeatEvent):
			s.mu.Lock()
			s.heartbeats++
			s.mu.Unlock()
			_ = c.reply(f, "ok", map[string]any{})
		case string(JoinEvent), string(LeaveEvent):
			_ = c.reply(f, "ok", map[string]any{})
		default:
			c.frames <- f
		}
	}
}

// send writes one frame to the client.
func (c *testConn) send(f testFrame) error {
	var joinRef, ref any
	if f.JoinRef != "" {
		joinRef = f.JoinRef
	}
	if f.Ref != "" {
		ref = f.Ref
	}
	data, err := json.Marshal([]any{joinRef, ref, f.Topic, f.Event, f.Payload})
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// reply answers the client frame f with a phx_reply.
func (c *testConn) reply(f testFrame, status string, response any) error {
	return c.send(testFrame{
		JoinRef: f.JoinRef,
		Ref:     f.Ref,
		Topic:   f.Topic,
		Event:   string(ReplyEvent),
		Payload: map[string]any{"status": status, "response": response},
	})
}

// next waits for the next client frame that the server did not answer itself.
func (c *testConn) next(t *testing.T) testFrame {
	t.Helper()
	select {
	case f := <-c.frames:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame from the client")
		return testFrame{}
	}
}

// newTestSocket returns a Socket for the test server with its own Dialer (websocket.DefaultDialer is
// process-global and Connect writes to it). The socket is disconnected when the test ends, without waiting.
func newTestSocket(t *testing.T, s *testServer) *Socket {
	t.Helper()
	socket := NewSocket(s.URL())
	ws := NewWebsocket(socket)
	ws.Dialer = &websocket.Dialer{}
	socket.Transport = ws
	socket.ConnectTimeout = 2 * time.Second
	t.Cleanup(func() { go func() { _ = socket.Disconnect() }() })
	return socket
}

// eventually polls cond until it is true, or fails the test after timeout.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
