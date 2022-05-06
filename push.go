package phx

import (
	"fmt"
	"time"
)

type pushCallback func(response any)

type pushBinding struct {
	status   string
	callback pushCallback
}

type Push struct {
	Event   Event
	Payload any
	Timeout time.Duration

	channel      *Channel
	Ref          Ref
	timeoutTimer *time.Timer
	callbacks    []*pushBinding
	sent         bool
}

func NewPush(channel *Channel, event Event, payload any, timeout time.Duration) *Push {
	return &Push{
		channel:   channel,
		Event:     event,
		Payload:   payload,
		Timeout:   timeout,
		Ref:       channel.socket.MakeRef(),
		callbacks: make([]*pushBinding, 0, 3),
	}
}

func (p *Push) Send() error {
	err := p.channel.socket.PushMessage(Message{
		Topic:   p.channel.Topic,
		Event:   p.Event,
		Payload: p.Payload,
		Ref:     p.Ref,
		JoinRef: p.channel.joinRef,
	})
	if err != nil {
		return err
	}
	p.sent = true

	p.channel.OnRef(p.Ref, ReplyEvent, func(payload any) {
		fmt.Println("Push.Send.OnRef callback", payload)
		p.callCallbacks(payload)
	})

	//p.timeoutTimer = time.AfterFunc(p.Timeout, func() { p.timeout() })
	return nil
}

func (p *Push) IsSent() bool {
	return p.sent
}

func (p *Push) Receive(status string, callback pushCallback) {
	p.callbacks = append(p.callbacks, &pushBinding{status: status, callback: callback})
}

func (p *Push) callCallbacks(payload any) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	status, ok := m["status"]
	if !ok {
		return
	}
	response, ok := m["response"]
	if !ok {
		return
	}

	for _, callback := range p.callbacks {
		if callback.status == status {
			callback.callback(response)
		}
	}
}

func (p *Push) timeout() {
	fmt.Println("Push.timeout!")
}
