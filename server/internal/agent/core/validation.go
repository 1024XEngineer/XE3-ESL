package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxMessageContentRunes = 4096
	MaxMessageContentBytes = 16384
	MaxAgentPageSize       = 100
	MaxRunBudget           = 1_000_000
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var clientMessageIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var modelPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidClientMessageID(value string) bool {
	return clientMessageIDPattern.MatchString(value)
}

func ValidProviderID(value string) bool {
	return providerPattern.MatchString(value)
}

func ValidModelID(value string) bool {
	return modelPattern.MatchString(value)
}

func ValidMessageContent(value string) bool {
	return utf8.ValidString(value) &&
		len(value) >= 1 &&
		utf8.RuneCountInString(value) <= MaxMessageContentRunes &&
		len(value) <= MaxMessageContentBytes &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) != ""
}

func ValidRunConfiguration(configuration RunConfiguration) bool {
	return ValidProviderID(configuration.Provider) &&
		ValidModelID(configuration.Model) &&
		configuration.MaxOutputTokens > 0 &&
		configuration.MaxOutputTokens <= MaxRunBudget &&
		configuration.MaxInputCharacters >= 5000 &&
		configuration.MaxInputCharacters <= MaxRunBudget
}
