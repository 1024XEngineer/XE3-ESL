package identity

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxDisplayNameCodePoints = 40
	maxDisplayNameBytes      = 120
)

func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalidRequest
	}
	displayName := norm.NFC.String(strings.TrimSpace(raw))
	if displayName == "" ||
		utf8.RuneCountInString(displayName) > maxDisplayNameCodePoints ||
		len(displayName) > maxDisplayNameBytes {
		return "", ErrInvalidRequest
	}
	for _, character := range displayName {
		if forbiddenDisplayNameCharacter(character) {
			return "", ErrInvalidRequest
		}
	}
	return displayName, nil
}

func forbiddenDisplayNameCharacter(character rune) bool {
	if unicode.IsControl(character) ||
		unicode.Is(unicode.Zl, character) ||
		unicode.Is(unicode.Zp, character) ||
		unicode.Is(unicode.Cf, character) && character != '\u200d' {
		return true
	}
	switch character {
	case '\u00ad', '\u034f', '\u061c', '\u180e', '\u200b', '\u200e',
		'\u200f', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2060', '\u2061', '\u2062', '\u2063', '\u2064', '\u2066',
		'\u2067', '\u2068', '\u2069', '\ufeff':
		return true
	default:
		return false
	}
}
