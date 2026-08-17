package mqtt

import "testing"

func TestRawStateMessage(t *testing.T) {
	message := RawStateMessage("alphaess", []byte(`{"ok":true}`))

	if message.Topic != "alphaess/raw/state" {
		t.Fatalf("topic = %q", message.Topic)
	}
	if string(message.Payload) != `{"ok":true}` {
		t.Fatalf("payload = %q", message.Payload)
	}
	if !message.Retained {
		t.Fatal("retained = false, want true")
	}
}

func TestStatusMessage(t *testing.T) {
	message := StatusMessage("alphaess", "online")

	if message.Topic != "alphaess/status" {
		t.Fatalf("topic = %q", message.Topic)
	}
	if string(message.Payload) != "online" {
		t.Fatalf("payload = %q", message.Payload)
	}
	if !message.Retained {
		t.Fatal("retained = false, want true")
	}
}
