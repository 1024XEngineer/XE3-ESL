package run

import (
	"regexp"
	"strings"
)

const MaxBudget = 1_000_000

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var modelPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`,
)

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidProviderID(value string) bool {
	return providerPattern.MatchString(value)
}

func ValidModelID(value string) bool {
	return modelPattern.MatchString(value) &&
		!strings.HasSuffix(value, "/") &&
		!strings.Contains(value, "//") &&
		!strings.Contains(value, "..")
}
