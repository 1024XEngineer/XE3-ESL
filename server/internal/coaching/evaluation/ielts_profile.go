package evaluation

import (
	"strings"
	"time"
)

const (
	IELTSProfileInputSchemaVersion      = "ielts-cumulative-profile-input/v1"
	IELTSCumulativeProfileSchemaVersion = "ielts-cumulative-profile/v1"
)

type IELTSProfileStage string

const (
	IELTSProfileStagePart1 IELTSProfileStage = "PART_1"
	IELTSProfileStagePart2 IELTSProfileStage = "PART_2"
)

type IELTSProfileDependencyResolution string

const (
	IELTSProfileDependencyPending  IELTSProfileDependencyResolution = "PENDING"
	IELTSProfileDependencyResolved IELTSProfileDependencyResolution = "RESOLVED"
	IELTSProfileDependencyFallback IELTSProfileDependencyResolution = "FALLBACK"
)

type IELTSFinalProfileResolution string

const (
	IELTSFinalProfileResolved IELTSFinalProfileResolution = "RESOLVED"
	IELTSFinalProfileFallback IELTSFinalProfileResolution = "FALLBACK"
)

type IELTSProfileInputSnapshot struct {
	SchemaVersion        string                           `json:"schema_version"`
	SessionID            string                           `json:"session_id"`
	SessionVersion       int                              `json:"session_version"`
	Stage                IELTSProfileStage                `json:"stage"`
	CompletedAt          time.Time                        `json:"completed_at"`
	Part1Boundary        int                              `json:"part_1_boundary"`
	Part2Boundary        int                              `json:"part_2_boundary"`
	AcousticCapability   AcousticCapabilityStatus         `json:"acoustic_capability"`
	Questions            []SessionEvidenceQuestion        `json:"questions"`
	Turns                []SessionEvidenceTurn            `json:"turns"`
	PreviousProfile      *IELTSCumulativeProfile          `json:"previous_profile,omitempty"`
	DependencyResolution IELTSProfileDependencyResolution `json:"dependency_resolution"`
}

func (snapshot IELTSProfileInputSnapshot) Valid() bool {
	if snapshot.SchemaVersion != IELTSProfileInputSchemaVersion ||
		!validUUID(snapshot.SessionID) || snapshot.SessionVersion < 1 ||
		snapshot.CompletedAt.IsZero() || snapshot.Part1Boundary < 1 ||
		snapshot.Part2Boundary <= snapshot.Part1Boundary ||
		(snapshot.AcousticCapability != AcousticCapabilityEnabled &&
			snapshot.AcousticCapability != AcousticCapabilityNotConfigured) ||
		(snapshot.Stage != IELTSProfileStagePart1 && snapshot.Stage != IELTSProfileStagePart2) ||
		(snapshot.DependencyResolution != IELTSProfileDependencyPending &&
			snapshot.DependencyResolution != IELTSProfileDependencyResolved &&
			snapshot.DependencyResolution != IELTSProfileDependencyFallback) ||
		len(snapshot.Questions) == 0 || len(snapshot.Turns) == 0 {
		return false
	}
	expectedTurns := snapshot.Part1Boundary
	if snapshot.Stage == IELTSProfileStagePart2 {
		expectedTurns = snapshot.Part2Boundary
	}
	if len(snapshot.Turns) != expectedTurns ||
		(snapshot.Stage == IELTSProfileStagePart1 && snapshot.PreviousProfile != nil) ||
		(snapshot.DependencyResolution == IELTSProfileDependencyResolved && snapshot.PreviousProfile == nil) ||
		(snapshot.PreviousProfile != nil && (!snapshot.PreviousProfile.Valid() ||
			snapshot.PreviousProfile.SessionID != snapshot.SessionID)) {
		return false
	}
	questionIDs := make(map[string]struct{}, len(snapshot.Questions))
	for _, question := range snapshot.Questions {
		if !validSessionEvidenceQuestion(question) {
			return false
		}
		questionIDs[question.ID] = struct{}{}
	}
	seenTurns := make(map[string]struct{}, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if !turn.Effective || !validSessionEvidenceTurn(turn) {
			return false
		}
		if _, exists := questionIDs[turn.QuestionID]; !exists {
			return false
		}
		if _, duplicate := seenTurns[turn.ID]; duplicate {
			return false
		}
		seenTurns[turn.ID] = struct{}{}
	}
	return true
}

type IELTSCumulativeProfile struct {
	SchemaVersion  string                  `json:"schema_version"`
	SessionID      string                  `json:"session_id"`
	CompletedParts []int                   `json:"completed_parts"`
	Dimensions     []IELTSProfileDimension `json:"dimensions"`
	Provider       string                  `json:"provider"`
	Model          string                  `json:"model"`
}

func (profile IELTSCumulativeProfile) Valid() bool {
	if profile.SchemaVersion != IELTSCumulativeProfileSchemaVersion ||
		!validUUID(profile.SessionID) ||
		(len(profile.CompletedParts) != 1 && len(profile.CompletedParts) != 2) ||
		profile.CompletedParts[0] != 1 ||
		(len(profile.CompletedParts) == 2 && profile.CompletedParts[1] != 2) ||
		len(profile.Dimensions) != 4 || !validIdentifier(profile.Provider) ||
		!validIdentifier(profile.Model) {
		return false
	}
	expected := []string{
		"FLUENCY_COHERENCE", "LEXICAL_RESOURCE",
		"GRAMMATICAL_RANGE_ACCURACY", "PRONUNCIATION",
	}
	for index, dimension := range profile.Dimensions {
		if dimension.Key != expected[index] || !dimension.Valid() {
			return false
		}
	}
	return true
}

type IELTSProfileDimension struct {
	Key                 string                    `json:"key"`
	ProvisionalBandLow  float64                   `json:"provisional_band_low"`
	ProvisionalBandHigh float64                   `json:"provisional_band_high"`
	Coverage            float64                   `json:"coverage"`
	Confidence          float64                   `json:"confidence"`
	Observations        []IELTSProfileObservation `json:"observations"`
}

func (dimension IELTSProfileDimension) Valid() bool {
	return validIdentifier(dimension.Key) && validIELTSBand(dimension.ProvisionalBandLow) &&
		validIELTSBand(dimension.ProvisionalBandHigh) &&
		dimension.ProvisionalBandLow <= dimension.ProvisionalBandHigh &&
		dimension.Coverage >= 0 && dimension.Coverage <= 1 &&
		dimension.Confidence >= 0 && dimension.Confidence <= 1 &&
		dimension.Observations != nil && len(dimension.Observations) <= 3 &&
		allValidProfileObservations(dimension.Observations)
}

type IELTSProfileObservation struct {
	Kind       string                 `json:"kind"`
	ReasonCode string                 `json:"reason_code"`
	Evidence   []IELTSProfileEvidence `json:"evidence"`
}

func (observation IELTSProfileObservation) Valid() bool {
	if (observation.Kind != "STRENGTH" && observation.Kind != "IMPROVEMENT") ||
		!validIdentifier(observation.ReasonCode) || len(observation.Evidence) == 0 ||
		len(observation.Evidence) > 2 {
		return false
	}
	for _, evidence := range observation.Evidence {
		if !evidence.Valid() {
			return false
		}
	}
	return true
}

type IELTSProfileEvidence struct {
	TurnID     string `json:"turn_id"`
	Quote      string `json:"quote"`
	Occurrence int    `json:"occurrence"`
	Part       int    `json:"part"`
}

func (evidence IELTSProfileEvidence) Valid() bool {
	return validUUID(evidence.TurnID) && strings.TrimSpace(evidence.Quote) != "" &&
		len(evidence.Quote) <= 4096 && evidence.Occurrence > 0 &&
		(evidence.Part == 1 || evidence.Part == 2)
}

func validIELTSBand(value float64) bool {
	return value >= 0 && value <= 9 && value*2 == float64(int(value*2))
}

func allValidProfileObservations(values []IELTSProfileObservation) bool {
	for _, value := range values {
		if !value.Valid() {
			return false
		}
	}
	return true
}
