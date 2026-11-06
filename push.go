package phx

import (
	"sync"
	"time"
)

type pushCallback func(response any)

type pushBinding struct {
	status   string
	callback pushCallback
}

// Push allows you to send an Event to the server and easily monitor for replies, errors or timeouts.
// A Push is typically created by Channel.Join, Channel.Leave and Channel.Push.
type Push struct {
	// Event is the string event you want to push to the server.
	Event string

	// Payload is whatever payload you want to attach to the push. Must be JSON serializable.
	Payload any

	// Timeout is the time to wait for a reply before triggering a "timeout" event.
	Timeout time.Duration

	mu           sync.RWMutex
	channel      *Channel
	Ref          Ref
	timeoutTimer *time.Timer
	callbacks    []*pushBinding
	sent         bool
	bindingRef   Ref
	reply        any
}

// NewPush gets a new Push ready to send and allows you to attach event handlers for replies, errors, timeouts.
func NewPush(channel *Channel, event string, payload any, timeout time.Duration) *Push {
	return &Push{
		channel:   channel,
		Event:     event,
		Payload:   payload,
		Timeout:   timeout,
		Ref:       0,
		callbacks: make([]*pushBinding, 0, 3),
	}
}

// Send will actually push the event to the server.
func (p *Push) Send() error {
	p.mu.Lock()
	p.resetLocked()
	ref := p.channel.socket.MakeRef()
	p.Ref = ref

	// Bind for the reply before the message goes out: the reply can be processed before PushMessage returns.
	bindingRef := p.channel.OnRef(ref, string(ReplyEvent), func(payload any) {
		// This runs in the Transports goroutine
		p.mu.Lock()
		defer p.mu.Unlock()

		p.cancelTimeout()
		p.channel.Off(p.bindingRef)
		p.reply = payload
		p.callCallbacks(payload)
	})
	p.bindingRef = bindingRef
	p.timeoutTimer = time.AfterFunc(p.Timeout, p.timeout)
	p.mu.Unlock()

	// Not under p.mu: JoinRef reads the Ref of the channel's join push, which may be this push.
	err := p.channel.socket.PushMessage(Message{
		Topic:   p.channel.topic,
		Event:   p.Event,
		Payload: p.Payload,
		Ref:     ref,
		JoinRef: p.channel.JoinRef(),
	})
	if err != nil {
		// Nothing was sent, so no reply will come for this binding.
		p.channel.Off(bindingRef)
		return err
	}

	p.mu.Lock()
	p.sent = true
	p.mu.Unlock()

	return nil
}

func (p *Push) IsSent() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.sent
}

// ref returns the Ref of the last Send, synchronised with Send.
func (p *Push) ref() Ref {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.Ref
}

// Receive registers the given event handler for the given status.
// Built in Events such as Join, Leave will respond with "ok", "error" and "timeout".
// Custom event handlers (handle_in/3) in your Channel on the server can respond with any string event they want.
// If a custom event handler (handle_in/3) does not reply (returns :noreply) then the only events that will trigger
// here are "error" and "timeout".
func (p *Push) Receive(status string, callback pushCallback) {
	p.mu.Lock()
	if p.reply != nil {
		replyStatus, replyResponse, ok := p.deconstructPayload(p.reply)
		if ok && replyStatus == status {
			p.mu.Unlock()
			callback(replyResponse)
			return
		}
	}

	p.callbacks = append(p.callbacks, &pushBinding{status: status, callback: callback})
	p.mu.Unlock()
}

func (p *Push) callCallbacks(payload any) {
	status, response, ok := p.deconstructPayload(payload)
	if ok {
		p.trigger(status, response)
	}
}

func (p *Push) deconstructPayload(payload any) (status string, response any, ok bool) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	statusRaw, ok := m["status"]
	if !ok {
		return
	}
	status, ok = statusRaw.(string)
	if !ok {
		return
	}
	response, ok = m["response"]
	if !ok {
		return
	}

	return
}

func (p *Push) trigger(status string, response any) {
	for _, callback := range p.callbacks {
		if callback.status == status {
			go callback.callback(response)
		}
	}
}

func (p *Push) cancelTimeout() {
	if p.timeoutTimer != nil {
		p.timeoutTimer.Stop()
		p.timeoutTimer = nil
	}
}

func (p *Push) timeout() {
	// This runs in the Timer's goroutine
	p.mu.Lock()
	defer p.mu.Unlock()

	p.trigger("timeout", nil)
}

// reset this push so that it will no longer timeout and won't process messages from the server.
func (p *Push) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.resetLocked()
}

func (p *Push) resetLocked() {
	p.cancelTimeout()
	if p.bindingRef != 0 {
		p.channel.Off(p.bindingRef)
	}
	p.Ref = 0
}
