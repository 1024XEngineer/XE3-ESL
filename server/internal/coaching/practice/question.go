package practice

import (
	"time"
)

// Question is the immutable prompt presented inside one Practice Session.
type Question struct {
	ID                      string    `json:"question_id"`
	SessionID               string    `json:"practice_session_id"`
	SpeakerParticipantID    string    `json:"speaker_participant_id"`
	AddresseeParticipantIDs []string  `json:"addressee_participant_ids"`
	ObjectiveID             string    `json:"objective_id"`
	Type                    string    `json:"question_type"`
	DialogueAct             string    `json:"-"`
	ParentQuestionID        string    `json:"parent_question_id,omitempty"`
	Content                 string    `json:"content"`
	Sequence                int       `json:"sequence"`
	CreatedAt               time.Time `json:"created_at"`
}

type QuestionDraft struct {
	ObjectiveID      string
	Type             string
	ParentQuestionID string
	Content          string
}

type QuestionProvider interface {
	BuildQuestion(int) (QuestionDraft, error)
}
