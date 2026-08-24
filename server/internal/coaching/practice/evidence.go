package practice

import (
	"encoding/json"
	"time"
)

// SessionEvidence is the complete Practice-owned input frozen by Evaluation.
// It contains confirmed business facts only; ASR attempts and provider state
// are deliberately excluded.
type SessionEvidence struct {
	UserID              string
	SessionID           string
	Version             int
	CompletedAt         time.Time
	EvaluationPolicyRef string
	PracticeExperience  string
	SceneCategory       string
	PracticeMode        string
	PlanSnapshot        json.RawMessage
	Participants        json.RawMessage
	Questions           []EvidenceQuestion
	Turns               []EvidenceTurn
}

type EvidenceQuestion struct {
	ID                      string
	Position                int
	ParentQuestionID        string
	Text                    string
	SpeakerParticipantID    string
	AddresseeParticipantIDs []string
}

type EvidenceTurn struct {
	ID                      string
	Position                int
	QuestionID              string
	RespondentParticipantID string
	Transcript              string
	Effective               bool
	ConfirmedAt             time.Time
	AudioAssetID            string
}

type TurnFeedbackEvidence struct {
	UserID                  string
	SessionID               string
	TurnID                  string
	QuestionID              string
	QuestionText            string
	Transcript              string
	RespondentParticipantID string
	AudioAssetID            string
	ConfirmedAt             time.Time
	EvaluationPolicyRef     string
	PracticeExperience      string
	SceneCategory           string
	PracticeMode            string
}

type IELTSProfileStage string

const (
	IELTSProfileStagePart1 IELTSProfileStage = "PART_1"
	IELTSProfileStagePart2 IELTSProfileStage = "PART_2"
)

// IELTSPartProfileEvidence is cumulative confirmed FULL_MOCK evidence frozen
// at a Part boundary. Part 2 deliberately includes Part 1 so Evaluation can
// rebuild when the earlier profile is unavailable.
type IELTSPartProfileEvidence struct {
	UserID         string
	SessionID      string
	SessionVersion int
	Stage          IELTSProfileStage
	CompletedAt    time.Time
	Part1Boundary  int
	Part2Boundary  int
	Questions      []EvidenceQuestion
	Turns          []EvidenceTurn
}
