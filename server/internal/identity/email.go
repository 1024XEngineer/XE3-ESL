package identity

import (
	"regexp"
	"strings"
)

const maxEmailBytes = 254

var canonicalEmailPattern = regexp.MustCompile(
	`^[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+)*@` +
		`[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
)

// NormalizeEmail implements the v1 wire contract: trim only ASCII whitespace
// U+0009 through U+000D and U+0020, require an ASCII/Punycode address, lowercase
// the whole address, and never apply provider-specific alias rules.
func NormalizeEmail(value string) (string, error) {
	trimmed := strings.Trim(value, "\t\n\v\f\r ")
	if len(trimmed) < 3 || len(trimmed) > maxEmailBytes {
		return "", ErrInvalidRequest
	}
	for i := range len(trimmed) {
		if trimmed[i] > 0x7f {
			return "", ErrInvalidRequest
		}
	}

	canonical := strings.ToLower(trimmed)
	if !canonicalEmailPattern.MatchString(canonical) {
		return "", ErrInvalidRequest
	}
	return canonical, nil
}
