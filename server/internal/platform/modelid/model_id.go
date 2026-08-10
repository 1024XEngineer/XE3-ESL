// Package modelid validates provider model identifiers shared across domains.
package modelid

import "strings"

const MaximumBytes = 128

// Valid accepts plain model names and provider-qualified names such as
// "moonshotai/kimi-k2.6" without permitting path-like traversal sequences.
func Valid(value string) bool {
	if value == "" || len(value) > MaximumBytes || value != strings.TrimSpace(value) ||
		strings.Contains(value, "//") || strings.Contains(value, "..") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if !validSegment(segment) {
			return false
		}
	}
	return true
}

func validSegment(segment string) bool {
	if segment == "" || !asciiAlphaNumeric(segment[0]) {
		return false
	}
	for index := 1; index < len(segment); index++ {
		value := segment[index]
		if !asciiAlphaNumeric(value) && value != '.' && value != '_' &&
			value != ':' && value != '-' {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}
