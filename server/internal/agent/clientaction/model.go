// Package clientaction defines trusted operations projected from completed
// Agent runs to authenticated clients. Actions are never part of model output.
package clientaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const (
	MaxItems        = 4
	MaxPayloadBytes = 16 * 1024
)

var (
	ErrInvalid  = errors.New("agent client action: invalid action")
	typePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,63}$`)
)

// Action is the generic runtime envelope. The owning business capability
// defines and validates the payload for its action type.
type Action struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func New(actionType string, payload json.RawMessage) (Action, error) {
	action := Action{Type: actionType, Payload: append(json.RawMessage(nil), payload...)}
	if err := Validate(action); err != nil {
		return Action{}, err
	}
	return action, nil
}

func Validate(action Action) error {
	if !typePattern.MatchString(action.Type) ||
		len(action.Payload) == 0 || len(action.Payload) > MaxPayloadBytes {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(action.Payload))
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func ValidateItems(items []Action) error {
	if len(items) > MaxItems {
		return ErrInvalid
	}
	for _, item := range items {
		if err := Validate(item); err != nil {
			return err
		}
	}
	return nil
}

func CloneItems(items []Action) []Action {
	cloned := make([]Action, len(items))
	for index, item := range items {
		cloned[index] = Action{
			Type:    item.Type,
			Payload: append(json.RawMessage(nil), item.Payload...),
		}
	}
	return cloned
}
