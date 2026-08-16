package preparation

import "strings"

type ResourceIDGenerator interface {
	NewID() (string, error)
}

func validResourceIdentifier(value string) bool {
	return value != "" && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func ValidResourceIdentifier(value string) bool { return validResourceIdentifier(value) }

// ValidAggregateID validates the canonical UUID wire shape used by mutable
// user-owned Preparation aggregates. Static Scene/Catalog identifiers remain
// deliberately separate opaque text values.
func ValidAggregateID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' ||
		value[18] != '-' || value[23] != '-' {
		return false
	}
	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func validCanonicalPath(value string) bool {
	return strings.HasPrefix(value, "/") && len(value) <= 1024 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func ValidCanonicalPath(value string) bool { return validCanonicalPath(value) }

func validIdempotencyKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func ValidIdempotencyKey(value string) bool { return validIdempotencyKey(value) }
