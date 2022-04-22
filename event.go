package phx

// Event represents a phoenix channel event for a message.
type Event string

const (
	// MessageEvent represents a regular message on a topic.
	MessageEvent Event = "phx_message"

	// JoinEvent represents a successful join on a channel.
	JoinEvent Event = "phx_join"

	// CloseEvent represents the closing of a channel.
	CloseEvent Event = "phx_close"

	// ErrorEvent represents an error.
	ErrorEvent Event = "phx_error"

	// ReplyEvent represents a reply to a message sent on a topic.
	ReplyEvent Event = "phx_reply"

	// LeaveEvent represents leaving a channel and unsubscribing from a topic.
	LeaveEvent Event = "phx_leave"

	// HeartBeatEvent is a special message for heartbeats
	HeartBeatEvent Event = "heartbeat"
)
