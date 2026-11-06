package phx

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

// replyingTransport is a Transport whose server answers every pushed message with an "ok" reply before Send
// returns, which is the fastest a real reply can possibly arrive.
type replyingTransport struct {
	socket *Socket
}

func (r *replyingTransport) Connect(*url.URL, http.Header, time.Duration) error { return nil }
func (r *replyingTransport) Disconnect() error                                  { return nil }
func (r *replyingTransport) Reconnect() error                                   { return nil }
func (r *replyingTransport) IsConnected() bool                                  { return true }
func (r *replyingTransport) ConnectionState() ConnectionState                   { return ConnectionOpen }

func (r *replyingTransport) Send(data []byte) error {
	msg, err := r.socket.Serializer.decode(data)
	if err != nil {
		return err
	}
	reply, err := r.socket.Serializer.encode(&Message{
		JoinRef: msg.JoinRef,
		Ref:     msg.Ref,
		Topic:   msg.Topic,
		Event:   string(ReplyEvent),
		Payload: map[string]any{"status": "ok", "response": msg.Event},
	})
	if err != nil {
		return err
	}
	r.socket.onConnMessage(reply)
	return nil
}

// TestPushReceivesReplyThatArrivesBeforeSendReturns: a reply can be processed by the reader goroutine before
// Push.Send has returned. Send used to register the reply binding only after the frame was handed to the transport,
// so such a reply found no binding and was dropped, and the push timed out instead.
func TestPushReceivesReplyThatArrivesBeforeSendReturns(t *testing.T) {
	socket := NewSocket(&url.URL{Scheme: "ws", Host: "example.invalid", Path: "/socket"})
	socket.Transport = &replyingTransport{socket: socket}
	channel := socket.Channel("room:1", nil)

	join, err := channel.Join()
	if err != nil {
		t.Fatal(err)
	}
	joined := make(chan any, 1)
	join.Receive("ok", func(response any) { joined <- response })

	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("join reply was dropped")
	}
}

// TestPushRepliesUnderConcurrentPushes pushes from several goroutines against a real server that replies at once.
// Every push must get its own reply; run with -race to check the push's reply bookkeeping is synchronised.
func TestPushRepliesUnderConcurrentPushes(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	channel := socket.Channel("room:1", nil)
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	conn := server.nextConn(t)
	go func() {
		for {
			f, ok := <-conn.frames
			if !ok {
				return
			}
			_ = conn.reply(f, "ok", f.Payload)
		}
	}()

	join, err := channel.Join()
	if err != nil {
		t.Fatal(err)
	}
	joined := make(chan struct{})
	join.Receive("ok", func(any) { close(joined) })
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("not joined")
	}

	const pushes = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[string]bool{}
	for i := 0; i < pushes; i++ {
		wg.Add(1)
		go func(i int) {
			// Handlers are attached before Send, as Join does, so nothing touches the push after Send.
			push := NewPush(channel, "ping", fmt.Sprint(i), 5*time.Second)
			push.Receive("ok", func(response any) {
				mu.Lock()
				got[fmt.Sprint(response)] = true
				mu.Unlock()
				wg.Done()
			})
			if err := push.Send(); err != nil {
				t.Error(err)
				wg.Done()
			}
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("got %d of %d replies", len(got), pushes)
	}
}
