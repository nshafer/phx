package phx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// joinCountServer is a Phoenix Channels server (JSON serializer v2) that refuses the websocket upgrade while it is
// down, and once up answers joins and heartbeats with "ok" and counts the phx_join frames per topic.
type joinCountServer struct {
	srv   *httptest.Server
	up    atomic.Bool
	mu    sync.Mutex
	joins map[string]int
}

func newJoinCountServer(t *testing.T) *joinCountServer {
	t.Helper()
	s := &joinCountServer{joins: make(map[string]int)}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.up.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var f []any
			if err := json.Unmarshal(data, &f); err != nil || len(f) != 5 {
				return
			}
			topic, _ := f[2].(string)
			event, _ := f[3].(string)
			if event == string(JoinEvent) {
				s.mu.Lock()
				s.joins[topic]++
				s.mu.Unlock()
			}
			if event == string(JoinEvent) || event == string(HeartBeatEvent) {
				reply, _ := json.Marshal([]any{f[0], f[1], topic, string(ReplyEvent),
					map[string]any{"status": "ok", "response": map[string]any{}}})
				if err := ws.WriteMessage(websocket.TextMessage, reply); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(func() {
		s.srv.CloseClientConnections()
		s.srv.Close()
	})
	return s
}

func (s *joinCountServer) URL() *url.URL {
	u, _ := url.Parse("ws" + strings.TrimPrefix(s.srv.URL, "http") + "/socket")
	return u
}

func (s *joinCountServer) joinCount(topic string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.joins[topic]
}

// failedDialScenario starts a socket whose first dial fails, with a channel that was never joined. The caller joins as
// soon as the connection opens. It calls afterFailedDial once the failed dial was reported and the channel's own
// socket OnError hook had time to run, and afterJoin once the join was acknowledged.
func failedDialScenario(t *testing.T, topic string, afterFailedDial func(*Channel), afterJoin func(*joinCountServer)) {
	t.Helper()
	server := newJoinCountServer(t)

	socket := NewSocket(server.URL())
	ws := NewWebsocket(socket)
	ws.Dialer = &websocket.Dialer{}
	socket.Transport = ws
	socket.ConnectTimeout = 2 * time.Second
	// Hold the transport's retry after the first failed dial until the server is up.
	retry := make(chan struct{})
	socket.ReconnectAfterFunc = func(int) time.Duration {
		<-retry
		return 0
	}

	channel := NewChannel(topic, nil, socket)
	joinErr := make(chan error, 1)
	socket.OnOpen(func() {
		_, err := channel.Join()
		joinErr <- err
	})
	var dialErrors int32
	socket.OnError(func(error) { atomic.AddInt32(&dialErrors, 1) })

	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = socket.Disconnect() }()
	eventually(t, 5*time.Second, "failed dial reported", func() bool {
		return atomic.LoadInt32(&dialErrors) > 0
	})
	time.Sleep(20 * time.Millisecond) // let the channel's own socket OnError hook run
	afterFailedDial(channel)

	server.up.Store(true)
	close(retry)
	if err := <-joinErr; err != nil {
		t.Fatalf("Join: %v", err)
	}
	eventually(t, 5*time.Second, "channel joined", channel.IsJoined)
	time.Sleep(20 * time.Millisecond) // a duplicate join goes out at the same moment as the first
	afterJoin(server)
}

// TestFailedDialKeepsNeverJoinedChannelClosed checks the state change: a failed dial is no error of a channel that
// was never joined. It stays closed and fires no OnError, matching phoenix.js and the socket's OnClose hook.
func TestFailedDialKeepsNeverJoinedChannelClosed(t *testing.T) {
	for i := 0; i < 10; i++ {
		failedDialScenario(t, "room:lobby", func(channel *Channel) {
			if state := channel.State(); state != ChannelClosed {
				t.Fatalf("iteration %d: never-joined channel is %v after a failed dial, want %v", i, state, ChannelClosed)
			}
		}, func(*joinCountServer) {})
	}
}

// TestJoinAfterFailedDialSendsOneJoin counts the phx_join frames the server receives. It deliberately has no
// assertion on the channel state after the failed dial. With the old OnError hook the channel is errored and its own
// OnOpen hook rejoins it at the same moment as the caller's Join. Both sends race, so the duplicate is probabilistic
// (about 1-3 of 30 runs fail against the old code), but a failure here is always a duplicate phx_join.
func TestJoinAfterFailedDialSendsOneJoin(t *testing.T) {
	const topic = "room:lobby"
	for i := 0; i < 10; i++ {
		failedDialScenario(t, topic, func(*Channel) {}, func(server *joinCountServer) {
			if n := server.joinCount(topic); n != 1 {
				t.Fatalf("iteration %d: server received %d phx_join for %q, want 1", i, n, topic)
			}
		})
	}
}

// TestJoinErrorOrder checks that a channel that is already joined or joining reports that, also when the socket is
// not connected, rather than the missing connection.
func TestJoinErrorOrder(t *testing.T) {
	u, _ := url.Parse("ws://127.0.0.1:1/socket")
	for _, tc := range []struct {
		state ChannelState
		want  string
	}{
		{ChannelJoined, "already joined"},
		{ChannelJoining, "join already in progress"},
		{ChannelClosed, "cannot join before connecting the socket"},
	} {
		channel := NewChannel("room:lobby", nil, NewSocket(u))
		channel.setState(tc.state)
		_, err := channel.Join()
		if err == nil || err.Error() != tc.want {
			t.Errorf("state %v on a disconnected socket: Join error = %v, want %q", tc.state, err, tc.want)
		}
	}
}
