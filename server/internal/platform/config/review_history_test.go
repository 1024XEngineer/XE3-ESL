package config

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestLoadReviewHistory(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString(
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	t.Setenv("REVIEW_HISTORY_CURSOR_SIGNING_KEY", encoded)

	configuration, err := LoadReviewHistory()
	if err != nil {
		t.Fatalf("LoadReviewHistory() error = %v", err)
	}
	if configuration.CursorSigningKey.Reveal() != encoded ||
		strings.Contains(configuration.CursorSigningKey.String(), encoded) {
		t.Fatal("LoadReviewHistory() did not preserve a redacted key")
	}
}

func TestLoadReviewHistoryRejectsInvalidSigningKey(t *testing.T) {
	for _, value := range []string{
		"",
		"not-base64url!",
		base64.RawURLEncoding.EncodeToString([]byte("too-short")),
		base64.RawURLEncoding.EncodeToString(make([]byte, 33)),
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("REVIEW_HISTORY_CURSOR_SIGNING_KEY", value)
			_, err := LoadReviewHistory()
			if !errors.Is(err, ErrReviewHistoryCursorSigningKey) {
				t.Fatalf(
					"LoadReviewHistory() error = %v, want %v",
					err,
					ErrReviewHistoryCursorSigningKey,
				)
			}
		})
	}
}
