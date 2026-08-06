package postgres

import (
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
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

func validSceneType(sceneType evaluation.SceneType) bool {
	switch sceneType {
	case evaluation.SceneIELTSSpeaking,
		evaluation.SceneInterview,
		evaluation.SceneOverseasDaily,
		evaluation.SceneOverseasWorkplace:
		return true
	default:
		return false
	}
}

func generalSceneTypeSupported(sceneType evaluation.SceneType) bool {
	return sceneType == evaluation.SceneIELTSSpeaking ||
		sceneType == evaluation.SceneOverseasDaily ||
		sceneType == evaluation.SceneOverseasWorkplace
}
