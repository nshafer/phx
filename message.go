package phx

// Message is a message sent or received via the Transport from the channel.
type Message struct {
	Topic   string `json:"topic"`
	Event   Event  `json:"event"`
	Payload any    `json:"payload"`
	Ref     Ref    `json:"ref"`
	JoinRef Ref    `json:"join_ref,omitempty"`
}
