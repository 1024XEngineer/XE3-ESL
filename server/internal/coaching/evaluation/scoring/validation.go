package scoring

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/modelid"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
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

func validActor(actor requestcontext.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
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

func validVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}

func validRuntimeLineage(value string) bool {
	return strings.TrimSpace(value) == value &&
		value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00')
}

func validModelIdentifier(value string) bool {
	return modelid.Valid(value)
}

func nonZeroDigest(digest [sha256.Size]byte) bool {
	return digest != [sha256.Size]byte{}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return evaluation.ErrInvalidRequest
}
