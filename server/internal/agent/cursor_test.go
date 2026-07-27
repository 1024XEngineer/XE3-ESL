package agent

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
	raw, err := encodeThreadPageCursor(want)
	if err != nil {
		t.Fatalf("encode Thread cursor: %v", err)
	}
	got, err := decodeThreadPageCursor(raw)
	if err != nil {
		t.Fatalf("decode Thread cursor: %v", err)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) || got.ThreadID != want.ThreadID {
		t.Fatalf("decoded Thread cursor = %#v, want %#v", got, want)
	}
	if _, err := decodeMessagePageCursor(
		raw,
		cursorTestThreadA,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Thread cursor accepted by Message endpoint: %v", err)
	}

	unknownField := base64.RawURLEncoding.EncodeToString([]byte(
		`{"v":1,"kind":"agent_threads","thread_id":"` +
			cursorTestThreadA +
			`","updated_at":"2026-07-26T08:09:10.000123Z","extra":true}`,
	))
	if _, err := decodeThreadPageCursor(
		unknownField,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cursor with unknown field error = %v, want invalid", err)
	}
	if _, err := decodeThreadPageCursor(raw + "="); !errors.Is(
		err,
		ErrInvalidRequest,
	) {
		t.Fatalf("padded cursor error = %v, want invalid", err)
	}
}

func TestMessagePageCursorIsBoundToThread(t *testing.T) {
	t.Parallel()

	want := MessagePageCursor{
		ThreadID:       cursorTestThreadA,
		BeforeSequence: 51,
	}
	raw, err := encodeMessagePageCursor(want)
	if err != nil {
		t.Fatalf("encode Message cursor: %v", err)
	}
	got, err := decodeMessagePageCursor(raw, cursorTestThreadA)
	if err != nil {
		t.Fatalf("decode Message cursor: %v", err)
	}
	if got != want {
		t.Fatalf("decoded Message cursor = %#v, want %#v", got, want)
	}
	if _, err := decodeMessagePageCursor(
		raw,
		cursorTestThreadB,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-Thread cursor error = %v, want invalid", err)
	}
	if _, err := decodeThreadPageCursor(raw); !errors.Is(
		err,
		ErrInvalidRequest,
	) {
		t.Fatalf("Message cursor accepted by Thread endpoint: %v", err)
	}
}

func TestDecodeAgentPageQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		defaultSize int
		wantSize    int
		wantCursor  string
		wantOK      bool
	}{
		{
			name:        "omitted",
			defaultSize: defaultThreadPageSize,
			wantSize:    defaultThreadPageSize,
			wantOK:      true,
		},
		{
			name:        "maximum and cursor",
			raw:         "page_size=100&cursor=opaque",
			defaultSize: defaultMessagePageSize,
			wantSize:    100,
			wantCursor:  "opaque",
			wantOK:      true,
		},
		{
			name:        "zero",
			raw:         "page_size=0",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "above maximum",
			raw:         "page_size=101",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "non decimal",
			raw:         "page_size=+20",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "duplicate size",
			raw:         "page_size=20&page_size=21",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "empty cursor",
			raw:         "cursor=",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "unknown query",
			raw:         "offset=20",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "invalid encoding",
			raw:         "cursor=%zz",
			defaultSize: defaultThreadPageSize,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			size, cursor, ok := decodeAgentPageQuery(
				testCase.raw,
				testCase.defaultSize,
			)
			if size != testCase.wantSize ||
				cursor != testCase.wantCursor ||
				ok != testCase.wantOK {
				t.Fatalf(
					"decodeAgentPageQuery(%q) = (%d, %q, %t), want (%d, %q, %t)",
					testCase.raw,
					size,
					cursor,
					ok,
					testCase.wantSize,
					testCase.wantCursor,
					testCase.wantOK,
				)
			}
		})
	}
}
