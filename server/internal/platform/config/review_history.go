package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

const reviewHistoryCursorKeyBytes = 32

var ErrReviewHistoryCursorSigningKey = errors.New(
	"REVIEW_HISTORY_CURSOR_SIGNING_KEY must be an unpadded base64url value encoding exactly 32 bytes",
)

type ReviewHistoryConfig struct {
	CursorSigningKey Secret
}

func LoadReviewHistory() (ReviewHistoryConfig, error) {
	raw := strings.TrimSpace(os.Getenv("REVIEW_HISTORY_CURSOR_SIGNING_KEY"))
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != reviewHistoryCursorKeyBytes {
		return ReviewHistoryConfig{}, ErrReviewHistoryCursorSigningKey
	}
	return ReviewHistoryConfig{
		CursorSigningKey: Secret{value: raw},
	}, nil
}
