package agent

import "regexp"

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
	)
	clientMessageIDPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	modelPattern    = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
