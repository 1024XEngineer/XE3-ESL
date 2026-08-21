package context

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
)

type MessageSource struct {
	MessageID string                   `json:"message_id"`
	Sequence  int64                    `json:"sequence"`
	Role      conversation.MessageRole `json:"role"`
}

type SummarySource struct {
	CoveredThroughSequence int64 `json:"covered_through_sequence"`
}

type Manifest struct {
	RunID                               string
	OwnerID                             string
	ThreadID                            string
	InputMessageID                      string
	InstructionVersion                  string
	CoachingProfileContextPolicyVersion string
	CoachingProfileContextStatus        string
	CoachingProfileVersion              int64
	SummaryContextPolicyVersion         string
	SummaryContextStatus                string
	SelectedSummary                     *SummarySource
	SelectedMessages                    []MessageSource
	OmittedMessageCount                 int
	TrimReason                          string
	MaxInputCharacters                  int
	UsedInputCharacters                 int
	RequestedProvider                   string
	RequestedModel                      string
	MaxOutputTokens                     int
	ExposedTools                        []string
	ToolSchemaHashes                    map[string]string
	CreatedAt                           time.Time
}

func (manifest Manifest) Valid() bool {
	return uuidPattern.MatchString(manifest.RunID) &&
		uuidPattern.MatchString(manifest.OwnerID) &&
		uuidPattern.MatchString(manifest.ThreadID) &&
		uuidPattern.MatchString(manifest.InputMessageID) &&
		providerPattern.MatchString(manifest.RequestedProvider) &&
		validModelID(manifest.RequestedModel) &&
		manifest.MaxOutputTokens > 0 &&
		manifest.MaxOutputTokens <= maxBudget &&
		manifest.MaxInputCharacters >= 5000 &&
		manifest.MaxInputCharacters <= maxBudget &&
		manifest.UsedInputCharacters >= 0 &&
		manifest.UsedInputCharacters <= manifest.MaxInputCharacters &&
		manifest.CoachingProfileContextPolicyVersion ==
			coachingProfileContextPolicyV1 &&
		validCoachingProfileStatus(manifest.CoachingProfileContextStatus) &&
		manifest.CoachingProfileVersion >= 0
}

func validCoachingProfileStatus(status string) bool {
	switch status {
	case coachingProfileContextNotAvailable,
		CoachingProfileContextUnavailableError,
		coachingProfileContextDisabled,
		coachingProfileContextSelected,
		coachingProfileContextOmittedBudget:
		return true
	default:
		return false
	}
}
