package phx

// Message is a message sent or received via the Transport from the channel.
type Message struct {
	Topic   string      `json:"topic"`
	Event   Event       `json:"event"`
	Payload interface{} `json:"payload"`
	Ref     uint64      `json:"ref"`
	JoinRef uint64      `json:"join_ref,omitempty"`
}
