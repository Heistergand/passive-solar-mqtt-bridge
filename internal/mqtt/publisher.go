package mqtt

import "context"

type Message struct {
	Topic    string
	Payload  []byte
	Retained bool
}

type Publisher interface {
	Connect(ctx context.Context) error
	PublishJSON(ctx context.Context, topic string, payload []byte, retained bool) error
	Close() error
}

func RawStateMessage(topicBase string, payload []byte) Message {
	return Message{
		Topic:    topicBase + "/raw/state",
		Payload:  payload,
		Retained: true,
	}
}

func StatusMessage(topicBase, status string) Message {
	return Message{
		Topic:    topicBase + "/status",
		Payload:  []byte(status),
		Retained: true,
	}
}
