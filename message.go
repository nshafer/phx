package phx

import (
	"encoding/json"
	"strconv"
)

// Message is a message sent or received via the socket after encoding/decoding
type Message struct {
	JoinRef Ref
	Ref     Ref
	Topic   string
	Event   Event
	Payload any
}

func (m Message) MarshalJSON() ([]byte, error) {
	//fmt.Printf("Message.MarshalJSON %+v\n", m)
	jsonMessage := NewJSONMessage(m)
	data, err := json.Marshal(jsonMessage)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("Message.MarshalJSON %+v -> %s\n", m, data)
	return data, nil
}

func (m *Message) UnmarshalJSON(data []byte) error {
	//fmt.Printf("Message.UnMarshalJSON %s\n", data)
	var jm JSONMessage
	err := json.Unmarshal(data, &jm)
	if err != nil {
		return err
	}
	msg, err := jm.Message()
	if err != nil {
		return err
	}
	// make a copy
	*m = *msg
	//fmt.Printf("Message.UnMarshalJSON %s -> %+v\n", data, m)
	return nil
}

type JSONMessage struct {
	JoinRef string `json:"join_ref,omitempty"`
	Ref     string `json:"ref"`
	Topic   string `json:"topic"`
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

func NewJSONMessage(msg Message) *JSONMessage {
	jm := &JSONMessage{
		Ref:     strconv.FormatUint(uint64(msg.Ref), 10),
		Topic:   msg.Topic,
		Event:   string(msg.Event),
		Payload: msg.Payload,
	}
	if msg.JoinRef != 0 {
		jm.JoinRef = strconv.FormatUint(uint64(msg.JoinRef), 10)
	}
	return jm
}

func (jm *JSONMessage) Message() (*Message, error) {
	//fmt.Printf("JSONMessage.Message: %#v\n", jm)
	joinRef, err := ParseRef(jm.JoinRef)
	if err != nil {
		return nil, err
	}

	ref, err := ParseRef(jm.Ref)
	if err != nil {
		return nil, err
	}

	event, err := ParseEvent(jm.Event)
	if err != nil {
		return nil, err
	}

	msg := Message{
		JoinRef: joinRef,
		Ref:     ref,
		Topic:   jm.Topic,
		Event:   event,
		Payload: jm.Payload,
	}

	return &msg, nil
}
