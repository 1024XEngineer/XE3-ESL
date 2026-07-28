package identity

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxDisplayNameCodePoints = 40
	maxDisplayNameBytes      = 120
	minIdempotencyKeyBytes   = 8
	maxIdempotencyKeyBytes   = 128
	sha256DigestBytes        = sha256.Size
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

func validIdempotencyKey(value string) bool {
	if value != strings.TrimSpace(value) ||
		len(value) < minIdempotencyKeyBytes ||
		len(value) > maxIdempotencyKeyBytes {
		return false
	}
	for _, character := range value {
		if !isIdempotencyKeyCharacter(character) {
			return false
		}
	}
	return true
}

func isIdempotencyKeyCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("._~+/-", character)
}

func profileRequestDigest(
	displayName string,
	expectedVersion *int64,
) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(displayName))
	_, _ = digest.Write([]byte{0})
	if expectedVersion == nil {
		_, _ = digest.Write([]byte("create"))
	} else {
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], uint64(*expectedVersion))
		_, _ = digest.Write([]byte("update"))
		_, _ = digest.Write(encoded[:])
	}
	return digest.Sum(nil)
}
