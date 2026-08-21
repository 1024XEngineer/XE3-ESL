package clientaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestActionValidatesGenericEnvelopeAndClonesPayload(t *testing.T) {
	payload := json.RawMessage(`{"resource_id":"one"}`)
	action, err := New("open_resource.v1", payload)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	payload[2] = 'X'
	if string(action.Payload) != `{"resource_id":"one"}` {
		t.Fatalf("Action payload aliased caller: %s", action.Payload)
	}
	cloned := CloneItems([]Action{action})
	cloned[0].Payload[2] = 'Y'
	if bytes.Equal(cloned[0].Payload, action.Payload) {
		t.Fatal("CloneItems() retained payload alias")
	}
}

func TestActionRejectsInvalidTypeAndUnboundedOrNonObjectPayload(t *testing.T) {
	tests := []Action{
		{Type: "practice action", Payload: json.RawMessage(`{}`)},
		{Type: "practice.v1", Payload: json.RawMessage(`[]`)},
		{Type: "practice.v1", Payload: json.RawMessage(`{} trailing`)},
		{Type: "practice.v1", Payload: json.RawMessage("{" +
			`"value":"` + string(bytes.Repeat([]byte{'a'}, MaxPayloadBytes)) + `"}`)},
	}
	for _, action := range tests {
		if err := Validate(action); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Validate(%q) error = %v", action.Type, err)
		}
	}
}
