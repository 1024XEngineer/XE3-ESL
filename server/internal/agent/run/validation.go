package run

import (
	"regexp"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
)

const MaxBudget = 1_000_000

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var opaqueIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

func ValidUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func ValidProviderID(value string) bool {
	return providerPattern.MatchString(value)
}

// ValidOpaqueID validates provider completion and tool-call identifiers. These
// are opaque values, not provider-qualified model names.
func ValidOpaqueID(value string) bool {
	return opaqueIDPattern.MatchString(value)
}

func ValidModelID(value string) bool {
	return modelid.Valid(value)
}
