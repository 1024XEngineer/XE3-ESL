package conversation

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxMessageContentRunes = 4096
	MaxMessageContentBytes = 16384
	MaxPageSize            = 100
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var clientMessageIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidClientMessageID(value string) bool {
	return clientMessageIDPattern.MatchString(value)
}

func ValidMessageContent(value string) bool {
	return utf8.ValidString(value) &&
		len(value) >= 1 &&
		utf8.RuneCountInString(value) <= MaxMessageContentRunes &&
		len(value) <= MaxMessageContentBytes &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) != ""
}
