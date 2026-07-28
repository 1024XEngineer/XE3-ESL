package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"
)

const (
	cursorVersion       = 1
	threadCursorKind    = "agent_threads"
	messageCursorKind   = "agent_messages"
	maxEncodedCursorLen = 1024
)

type cursorEnvelope struct {
	Version        int    `json:"v"`
	Kind           string `json:"kind"`
	ThreadID       string `json:"thread_id"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	BeforeSequence int64  `json:"before_sequence,omitempty"`
}

func EncodeThreadPageCursor(cursor ThreadPageCursor) (string, error) {
	if cursor.UpdatedAt.IsZero() || !ValidUUID(cursor.ThreadID) {
		return "", ErrInvalidRequest
	}
	return encodeCursor(cursorEnvelope{
		Version:   cursorVersion,
		Kind:      threadCursorKind,
		ThreadID:  cursor.ThreadID,
		UpdatedAt: cursor.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func DecodeThreadPageCursor(raw string) (ThreadPageCursor, error) {
	envelope, err := decodeCursor(raw)
	if err != nil ||
		envelope.Version != cursorVersion ||
		envelope.Kind != threadCursorKind ||
		!ValidUUID(envelope.ThreadID) ||
		envelope.UpdatedAt == "" ||
		envelope.BeforeSequence != 0 {
		return ThreadPageCursor{}, ErrInvalidRequest
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, envelope.UpdatedAt)
	if err != nil {
		return ThreadPageCursor{}, ErrInvalidRequest
	}
	cursor := ThreadPageCursor{
		UpdatedAt: updatedAt.UTC(),
		ThreadID:  envelope.ThreadID,
	}
	canonical, err := EncodeThreadPageCursor(cursor)
	if err != nil || canonical != raw {
		return ThreadPageCursor{}, ErrInvalidRequest
	}
	return cursor, nil
}

func EncodeMessagePageCursor(cursor MessagePageCursor) (string, error) {
	if !ValidUUID(cursor.ThreadID) || cursor.BeforeSequence < 1 {
		return "", ErrInvalidRequest
	}
	return encodeCursor(cursorEnvelope{
		Version:        cursorVersion,
		Kind:           messageCursorKind,
		ThreadID:       cursor.ThreadID,
		BeforeSequence: cursor.BeforeSequence,
	})
}

func DecodeMessagePageCursor(
	raw string,
	threadID string,
) (MessagePageCursor, error) {
	envelope, err := decodeCursor(raw)
	if err != nil ||
		envelope.Version != cursorVersion ||
		envelope.Kind != messageCursorKind ||
		envelope.ThreadID != threadID ||
		!ValidUUID(envelope.ThreadID) ||
		envelope.UpdatedAt != "" ||
		envelope.BeforeSequence < 1 {
		return MessagePageCursor{}, ErrInvalidRequest
	}
	cursor := MessagePageCursor{
		ThreadID:       envelope.ThreadID,
		BeforeSequence: envelope.BeforeSequence,
	}
	canonical, err := EncodeMessagePageCursor(cursor)
	if err != nil || canonical != raw {
		return MessagePageCursor{}, ErrInvalidRequest
	}
	return cursor, nil
}

func encodeCursor(envelope cursorEnvelope) (string, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", ErrRepository
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(raw string) (cursorEnvelope, error) {
	if raw == "" || len(raw) > maxEncodedCursorLen {
		return cursorEnvelope{}, ErrInvalidRequest
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return cursorEnvelope{}, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var envelope cursorEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return cursorEnvelope{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return cursorEnvelope{}, ErrInvalidRequest
	}
	return envelope, nil
}
