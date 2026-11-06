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

// TestJoinAfterFailedDialSendsOneJoin covers a channel that was never joined when the socket's first dial fails, and
// that the caller joins as soon as the connection opens. The failed dial must not mark the channel errored, or the
// channel's own OnOpen hook rejoins it at the same moment and the server receives the same phx_join twice.
func TestJoinAfterFailedDialSendsOneJoin(t *testing.T) {
	const topic = "room:lobby"
	for i := 0; i < 10; i++ {
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
		eventually(t, 5*time.Second, "failed dial reported", func() bool {
			return atomic.LoadInt32(&dialErrors) > 0
		})
		time.Sleep(20 * time.Millisecond) // let the channel's own socket OnError hook run
		if state := channel.State(); state != ChannelClosed {
			t.Fatalf("iteration %d: never-joined channel is %v after a failed dial, want %v", i, state, ChannelClosed)
		}

		server.up.Store(true)
		close(retry)
		if err := <-joinErr; err != nil {
			t.Fatalf("iteration %d: Join: %v", i, err)
		}
		eventually(t, 5*time.Second, "channel joined", channel.IsJoined)
		time.Sleep(20 * time.Millisecond) // a duplicate join goes out at the same moment as the first
		if n := server.joinCount(topic); n != 1 {
			t.Fatalf("iteration %d: server received %d phx_join for %q, want 1", i, n, topic)
		}
		_ = socket.Disconnect()
	}
}
