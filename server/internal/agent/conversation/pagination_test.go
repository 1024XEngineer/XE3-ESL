package conversation

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

const (
	cursorTestThreadA = "10000000-0000-4000-8000-000000000001"
	cursorTestThreadB = "10000000-0000-4000-8000-000000000002"
)

func TestThreadPageCursorIsCanonicalAndEndpointSpecific(t *testing.T) {
	t.Parallel()

	want := ThreadPageCursor{
		UpdatedAt: time.Date(2026, time.July, 26, 8, 9, 10, 123000, time.UTC),
		ThreadID:  cursorTestThreadA,
	}
	raw, err := EncodeThreadPageCursor(want)
	if err != nil {
		t.Fatalf("encode Thread cursor: %v", err)
	}
	got, err := DecodeThreadPageCursor(raw)
	if err != nil {
		t.Fatalf("decode Thread cursor: %v", err)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) || got.ThreadID != want.ThreadID {
		t.Fatalf("decoded Thread cursor = %#v, want %#v", got, want)
	}
	if _, err := DecodeMessagePageCursor(raw, cursorTestThreadA); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Thread cursor accepted by Message endpoint: %v", err)
	}

	unknownField := base64.RawURLEncoding.EncodeToString([]byte(
		`{"v":1,"kind":"agent_threads","thread_id":"` +
			cursorTestThreadA +
			`","updated_at":"2026-07-26T08:09:10.000123Z","extra":true}`,
	))
	if _, err := DecodeThreadPageCursor(unknownField); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cursor with unknown field error = %v, want invalid", err)
	}
	if _, err := DecodeThreadPageCursor(raw + "="); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("padded cursor error = %v, want invalid", err)
	}
}

func TestMessagePageCursorIsBoundToThread(t *testing.T) {
	t.Parallel()

	want := MessagePageCursor{ThreadID: cursorTestThreadA, BeforeSequence: 51}
	raw, err := EncodeMessagePageCursor(want)
	if err != nil {
		t.Fatalf("encode Message cursor: %v", err)
	}
	got, err := DecodeMessagePageCursor(raw, cursorTestThreadA)
	if err != nil {
		t.Fatalf("decode Message cursor: %v", err)
	}
	if got != want {
		t.Fatalf("decoded Message cursor = %#v, want %#v", got, want)
	}
	if _, err := DecodeMessagePageCursor(raw, cursorTestThreadB); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-Thread cursor error = %v, want invalid", err)
	}
	if _, err := DecodeThreadPageCursor(raw); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Message cursor accepted by Thread endpoint: %v", err)
	}
}
