package core

import (
	"strings"
	"unicode/utf8"
)

const ThreadTitleContentLimit = 24

func DeriveThreadTitle(firstUserMessage string) string {
	normalized := strings.Join(strings.Fields(firstUserMessage), " ")
	if normalized == "" {
		return ""
	}
	if utf8.RuneCountInString(normalized) <= ThreadTitleContentLimit {
		return normalized
	}
	runes := []rune(normalized)
	return string(runes[:ThreadTitleContentLimit]) + "…"
}
