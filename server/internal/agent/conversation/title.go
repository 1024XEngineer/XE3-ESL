package conversation

import (
	"strings"
	"unicode"
)

const MaxThreadTitleRunes = 32

// DeriveThreadTitle is a deterministic projection of the first user message.
// It avoids a second model call and does not create another durable job.
func DeriveThreadTitle(content string) string {
	words := strings.FieldsFunc(content, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	})
	if len(words) == 0 {
		return ""
	}
	runes := []rune(strings.Join(words, " "))
	if len(runes) > MaxThreadTitleRunes {
		runes = runes[:MaxThreadTitleRunes]
	}
	return string(runes)
}
