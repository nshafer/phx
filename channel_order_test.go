package phx

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// joinTestChannel connects socket and joins channel, returning the server side of the connection.
func joinTestChannel(t *testing.T, server *testServer, socket *Socket, channels ...*Channel) *testConn {
	t.Helper()
	if err := socket.Connect(); err != nil {
		t.Fatal(err)
	}
	conn := server.nextConn(t)
	for _, channel := range channels {
		join, err := channel.Join()
		if err != nil {
			t.Fatal(err)
		}
		joined := make(chan struct{})
		join.Receive("ok", func(any) { close(joined) })
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Fatalf("channel %s not joined", channel.Topic())
		}
	}
	return conn
}

// TestOrderedCallbacksRunInFrameOrder: with OrderedCallbacks, the handlers of one channel see its events in the
// order the server sent them and never run concurrently.
func TestOrderedCallbacksRunInFrameOrder(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	channel := socket.Channel("room:1", nil)
	channel.OrderedCallbacks = true

	const events = 100
	var mu sync.Mutex
	var got []string
	active, maxActive := 0, 0
	done := make(chan struct{})
	channel.On("seq", func(payload any) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		// Uneven handler durations, so that handlers running in parallel would finish out of order.
		if n := len(fmt.Sprint(payload)); n%2 == 0 {
			time.Sleep(time.Millisecond)
		}

		mu.Lock()
		active--
		got = append(got, fmt.Sprint(payload))
		if len(got) == events {
			close(done)
		}
		mu.Unlock()
	})

	conn := joinTestChannel(t, server, socket, channel)
	for i := 0; i < events; i++ {
		if err := conn.send(testFrame{Topic: "room:1", Event: "seq", Payload: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not all events were handled")
	}
	mu.Lock()
	defer mu.Unlock()
	for i, payload := range got {
		if payload != fmt.Sprint(i) {
			t.Fatalf("events handled out of order: %v", got)
		}
	}
	if maxActive != 1 {
		t.Errorf("up to %d handlers of one channel ran at the same time, want 1", maxActive)
	}
}

// TestOrderedCallbackDoesNotBlockOtherChannels: a handler that is still running only holds up later events of its
// own channel, not the socket's reader nor another channel.
func TestOrderedCallbackDoesNotBlockOtherChannels(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	slow := socket.Channel("room:slow", nil)
	slow.OrderedCallbacks = true
	other := socket.Channel("room:other", nil)
	other.OrderedCallbacks = true

	release := make(chan struct{})
	handled := make(chan struct{})
	slow.On("work", func(any) {
		select {
		case <-release:
			close(handled)
		case <-time.After(5 * time.Second):
		}
	})
	other.On("ping", func(any) { close(release) })

	conn := joinTestChannel(t, server, socket, slow, other)
	_ = conn.send(testFrame{Topic: "room:slow", Event: "work", Payload: map[string]any{}})
	_ = conn.send(testFrame{Topic: "room:other", Event: "ping", Payload: map[string]any{}})

	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("the other channel's handler did not run while the first channel's handler was busy")
	}
}

// TestOrderedCallbackCanWaitForPushReply: replies to pushes are not queued behind the channel's handlers, so a
// handler can push and wait for the reply.
func TestOrderedCallbackCanWaitForPushReply(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	channel := socket.Channel("room:1", nil)
	channel.OrderedCallbacks = true

	replied := make(chan struct{})
	channel.On("ask", func(any) {
		push, err := channel.Push("answer", map[string]any{})
		if err != nil {
			t.Error(err)
			return
		}
		got := make(chan struct{})
		push.Receive("ok", func(any) { close(got) })
		select {
		case <-got:
			close(replied)
		case <-time.After(3 * time.Second):
		}
	})

	conn := joinTestChannel(t, server, socket, channel)
	_ = conn.send(testFrame{Topic: "room:1", Event: "ask", Payload: map[string]any{}})
	f := conn.next(t)
	_ = conn.reply(f, "ok", map[string]any{})

	select {
	case <-replied:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never saw the reply to its push")
	}
}

// TestOrderedCallbacksDoNotDelayChannelStateHandling: the Channel's own handling of phx_error (state change, rejoin
// timer) is not queued behind user callbacks, so a slow handler does not hold up error handling and rejoin.
func TestOrderedCallbacksDoNotDelayChannelStateHandling(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	channel := socket.Channel("room:1", nil)
	channel.OrderedCallbacks = true

	release := make(chan struct{})
	defer close(release)
	channel.On("work", func(any) { <-release })

	conn := joinTestChannel(t, server, socket, channel)
	_ = conn.send(testFrame{Topic: "room:1", Event: "work", Payload: map[string]any{}})
	_ = conn.send(testFrame{Topic: "room:1", Event: string(ErrorEvent), Payload: map[string]any{}})

	eventually(t, 3*time.Second, "channel errored while a user handler is still running", channel.IsErrored)
}

// TestOrderedCallbacksOfOneEventRunInRegistrationOrder: several callbacks bound to the same event run in the order
// they were registered.
func TestOrderedCallbacksOfOneEventRunInRegistrationOrder(t *testing.T) {
	server := newTestServer(t)
	socket := newTestSocket(t, server)
	channel := socket.Channel("room:1", nil)
	channel.OrderedCallbacks = true

	const callbacks = 20
	var mu sync.Mutex
	var got []int
	done := make(chan struct{})
	for i := 0; i < callbacks; i++ {
		i := i
		channel.On("multi", func(any) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, i)
			if len(got) == callbacks {
				close(done)
			}
		})
	}

	conn := joinTestChannel(t, server, socket, channel)
	_ = conn.send(testFrame{Topic: "room:1", Event: "multi", Payload: map[string]any{}})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not all callbacks ran")
	}
	mu.Lock()
	defer mu.Unlock()
	for i, n := range got {
		if n != i {
			t.Fatalf("callbacks ran in order %v, want registration order", got)
		}
	}
}
