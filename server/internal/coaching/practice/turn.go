package practice

import "time"

type Answer struct {
	InteractionMode string    `json:"interaction_mode,omitempty"`
	AnswerText      string    `json:"answer_text,omitempty"`
	AudioAssetID    string    `json:"audio_asset_id,omitempty"`
	CandidateID     string    `json:"-"`
	TranscriptID    string    `json:"-"`
	EvidenceVersion int64     `json:"-"`
	ConfirmedAt     time.Time `json:"-"`
}

type TurnKind string

const (
	TurnKindEffective TurnKind = "EFFECTIVE"
	TurnKindRetry     TurnKind = "RETRY"
)

// Turn is the only confirmed Question/Answer progression unit in Practice.
type Turn struct {
	ID                      string    `json:"turn_id"`
	SessionID               string    `json:"practice_session_id"`
	QuestionID              string    `json:"question_id"`
	SpeakerParticipantID    string    `json:"question_speaker_participant_id"`
	AddresseeParticipantIDs []string  `json:"addressee_participant_ids"`
	RespondentParticipantID string    `json:"respondent_participant_id"`
	Sequence                int       `json:"sequence"`
	InteractionMode         string    `json:"interaction_mode,omitempty"`
	AnswerText              string    `json:"answer_text,omitempty"`
	AudioAssetID            string    `json:"audio_asset_id,omitempty"`
	CandidateID             string    `json:"-"`
	TranscriptID            string    `json:"-"`
	EvidenceVersion         int64     `json:"-"`
	ConfirmedAt             time.Time `json:"-"`
	Kind                    TurnKind  `json:"turn_kind"`
	RetryRequestID          string    `json:"retry_request_id,omitempty"`
	OriginalTurnID          string    `json:"original_turn_id,omitempty"`
	CountsTowardTurnLimit   bool      `json:"counts_toward_turn_limit"`
	EffectiveTurns          int       `json:"effective_turns"`
	SessionCompleted        bool      `json:"session_completed"`
	Status                  string    `json:"turn_status,omitempty"`
	SpeechFeedbackStatusURL string    `json:"speech_feedback_status_url,omitempty"`
	SubmittedAt             time.Time `json:"submitted_at,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	CompletedAt             time.Time `json:"completed_at,omitempty"`
}

func (t Turn) ConfirmedAnswer() Answer {
	return Answer{
		InteractionMode: t.InteractionMode,
		AnswerText:      t.AnswerText,
		AudioAssetID:    t.AudioAssetID,
		CandidateID:     t.CandidateID,
		TranscriptID:    t.TranscriptID,
		EvidenceVersion: t.EvidenceVersion,
		ConfirmedAt:     t.ConfirmedAt,
	}
}
