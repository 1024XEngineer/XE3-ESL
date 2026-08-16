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
