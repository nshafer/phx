package phx

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type channelCallback func(payload any)

type channelBinding struct {
	ref      Ref
	event    Event
	callback channelCallback
}

type Channel struct {
	Topic       string
	Params      map[string]string
	JoinTimeout time.Duration
	PushTimeout time.Duration

	// private
	mu           sync.RWMutex
	socket       *Socket
	state        ChannelState
	refGenerator *atomicRef
	joinRef      Ref
	joinedOnce   bool
	bindings     map[Ref]*channelBinding
}

func NewChannel(topic string, params map[string]string, socket *Socket) *Channel {
	return &Channel{
		Topic:        topic,
		Params:       params,
		JoinTimeout:  defaultJoinTimeout,
		PushTimeout:  defaultPushTimeout,
		socket:       socket,
		state:        ChannelClosed,
		refGenerator: newAtomicRef(),
		bindings:     make(map[Ref]*channelBinding),
	}
}

func (c *Channel) State() ChannelState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

func (c *Channel) Join() (*Push, error) {
	if c.joinedOnce {
		return nil, errors.New("tried to join multiple times. 'Join' can only be called a single time")
	}
	if !c.socket.IsConnectedOrConnecting() {
		return nil, errors.New("cannot join before connecting the socket")
	}
	push := NewPush(c, JoinEvent, c.Params, c.JoinTimeout)
	c.joinRef = push.Ref
	push.Receive("ok", func(response any) {
		fmt.Println("Channel.Join push got reply", response)
		c.state = ChannelJoined
	})
	push.Receive("error", func(response any) {
		fmt.Println("Channel.Join push got reply", response)
		c.state = ChannelErrored
	})

	err := push.Send()
	if err != nil {
		return nil, err
	}
	c.joinedOnce = true

	return push, nil
}

func (c *Channel) IsJoined() bool {
	return c.state == ChannelJoined || c.state == ChannelJoining
}

func (c *Channel) Push(event Event, payload any) (*Push, error) {
	push := NewPush(c, event, payload, c.PushTimeout)
	err := push.Send()
	return push, err
}

// On will call the given callback for all matching events
func (c *Channel) On(event Event, callback channelCallback) (bindingRef Ref) {
	bindingRef = c.refGenerator.nextRef()
	c.bindings[bindingRef] = &channelBinding{
		event:    event,
		callback: callback,
	}
	return
}

// OnRef will call the given callback for all matching events only if the ref also matches
func (c *Channel) OnRef(ref Ref, event Event, callback channelCallback) (bindingRef Ref) {
	bindingRef = c.refGenerator.nextRef()
	c.bindings[ref] = &channelBinding{
		ref:      ref,
		event:    event,
		callback: callback,
	}
	return
}

// Off removes the callback for the given bindingRef, as returned by On or OnRef.
func (c *Channel) Off(bindingRef Ref) {
	delete(c.bindings, bindingRef)
}

// Clear removes all bindings for the given event
func (c *Channel) Clear(event Event) {
	for ref, binding := range c.bindings {
		if binding.event == event {
			delete(c.bindings, ref)
		}
	}
}

func (c *Channel) setState(state ChannelState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.state = state
}

func (c *Channel) process(msg *Message) {
	fmt.Println("Channel.process", msg)

	if msg.Topic != c.Topic {
		return
	}

	if msg.JoinRef != 0 && msg.JoinRef != c.joinRef {
		c.socket.Logger.Println(LogWarning, "channel", "dropping stale message", msg)
		return
	}

	// For a given channelBinding to get called it must match the event and either have ref == 0 or match the ref
	for _, binding := range c.bindings {
		if binding.event == msg.Event && (binding.ref == 0 || binding.ref == msg.Ref) {
			binding.callback(msg.Payload)
		}
	}
}
