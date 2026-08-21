package evaluation

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidRequest      = errors.New("evaluation: invalid request")
	ErrNotFound            = errors.New("evaluation: not found")
	ErrIdempotencyConflict = errors.New("evaluation: idempotency conflict")
	ErrAccountUnavailable  = errors.New("evaluation: account unavailable")
	ErrRetryNotAllowed     = errors.New("evaluation: retry not allowed")
)

type SceneType string

const (
	SceneIELTSSpeaking     SceneType = "IELTS_SPEAKING"
	SceneInterview         SceneType = "INTERVIEW"
	SceneOverseasDaily     SceneType = "OVERSEAS_DAILY_LIFE"
	SceneOverseasWorkplace SceneType = "OVERSEAS_WORKPLACE"
)

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
	identifierPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	versionPattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9._:/-]{0,127}$`,
	)
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func validIdentifier(value string) bool {
	return identifierPattern.MatchString(strings.TrimSpace(value))
}

func validVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}
