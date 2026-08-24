package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SessionInputSchemaVersion  = "session-evaluation-input/v1"
	SpeechInputSchemaVersion   = "speech-evaluation-input/v1"
	ConfigLineageSchemaVersion = "evaluation-config-lineage/v1"
)

type AcousticCapabilityStatus string

const (
	AcousticCapabilityEnabled       AcousticCapabilityStatus = "ENABLED"
	AcousticCapabilityNotConfigured AcousticCapabilityStatus = "NOT_CONFIGURED"
)

// Kind fixes the meaning of source_id and context_id. No second source-kind
// discriminator is persisted.
type Kind string

const (
	KindSessionReport        Kind = "SESSION_REPORT"
	KindPracticeTurnFeedback Kind = "PRACTICE_TURN_FEEDBACK"
	KindAgentMessageFeedback Kind = "AGENT_MESSAGE_FEEDBACK"
	KindIELTSPart1Profile    Kind = "IELTS_PART1_PROFILE"
	KindIELTSPart2Profile    Kind = "IELTS_PART2_PROFILE"
)

func (kind Kind) Valid() bool {
	switch kind {
	case KindSessionReport, KindPracticeTurnFeedback, KindAgentMessageFeedback,
		KindIELTSPart1Profile, KindIELTSPart2Profile:
		return true
	default:
		return false
	}
}

// JobStatus is the complete durable state machine. QUEUED is both the initial
// state and the retry-wait state; no parallel module or outbox state exists.
type JobStatus string

const (
	JobQueued  JobStatus = "QUEUED"
	JobRunning JobStatus = "RUNNING"
	JobReady   JobStatus = "READY"
	JobFailed  JobStatus = "FAILED"
)

func (status JobStatus) Valid() bool {
	switch status {
	case JobQueued, JobRunning, JobReady, JobFailed:
		return true
	default:
		return false
	}
}

type ConfigLineage struct {
	SchemaVersion   string `json:"schema_version"`
	StrategyRef     string `json:"strategy_ref"`
	PipelineVersion string `json:"pipeline_version"`
	PromptVersion   string `json:"prompt_version"`
	ResultSchema    string `json:"result_schema"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
}

func (lineage ConfigLineage) Valid() bool {
	return lineage.SchemaVersion == ConfigLineageSchemaVersion &&
		validVersion(lineage.StrategyRef) &&
		validVersion(lineage.PipelineVersion) &&
		validVersion(lineage.PromptVersion) &&
		validVersion(lineage.ResultSchema) &&
		validIdentifier(lineage.Provider) &&
		validIdentifier(lineage.Model)
}

type JobError struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
}

func (failure JobError) Valid() bool {
	message := strings.TrimSpace(failure.Message)
	return validIdentifier(failure.Code) && message != "" &&
		len(message) <= 2048
}

type SessionInputSnapshot struct {
	SchemaVersion       string                      `json:"schema_version"`
	SessionID           string                      `json:"session_id"`
	SessionVersion      int                         `json:"session_version"`
	EvaluationPolicyRef string                      `json:"evaluation_policy_ref"`
	PracticeExperience  string                      `json:"practice_experience"`
	SceneCategory       string                      `json:"scene_category"`
	PracticeMode        string                      `json:"practice_mode"`
	CompletedAt         time.Time                   `json:"completed_at"`
	AcousticCapability  AcousticCapabilityStatus    `json:"acoustic_capability"`
	PlanSnapshot        json.RawMessage             `json:"plan_snapshot"`
	Participants        json.RawMessage             `json:"participants"`
	Questions           []SessionEvidenceQuestion   `json:"questions"`
	Turns               []SessionEvidenceTurn       `json:"turns"`
	CumulativeProfile   *IELTSCumulativeProfile     `json:"cumulative_profile,omitempty"`
	ProfileResolution   IELTSFinalProfileResolution `json:"profile_resolution,omitempty"`
}

type SessionEvidenceQuestion struct {
	ID                      string   `json:"id"`
	Position                int      `json:"position"`
	ParentQuestionID        string   `json:"parent_question_id,omitempty"`
	Text                    string   `json:"text"`
	SpeakerParticipantID    string   `json:"speaker_participant_id"`
	AddresseeParticipantIDs []string `json:"addressee_participant_ids"`
}

type SessionEvidenceTurn struct {
	ID                      string              `json:"id"`
	Position                int                 `json:"position"`
	QuestionID              string              `json:"question_id"`
	RespondentParticipantID string              `json:"respondent_participant_id"`
	Transcript              string              `json:"transcript"`
	Effective               bool                `json:"effective"`
	ConfirmedAt             time.Time           `json:"confirmed_at"`
	AudioAssetID            string              `json:"audio_asset_id,omitempty"`
	Acoustic                *AcousticCheckpoint `json:"acoustic,omitempty"`
}

func (snapshot SessionInputSnapshot) Valid() bool {
	if snapshot.SchemaVersion != SessionInputSchemaVersion ||
		!validUUID(snapshot.SessionID) || snapshot.SessionVersion < 1 ||
		!validVersion(snapshot.EvaluationPolicyRef) ||
		!validIdentifier(snapshot.PracticeExperience) ||
		!validIdentifier(snapshot.SceneCategory) ||
		!validIdentifier(snapshot.PracticeMode) ||
		snapshot.CompletedAt.IsZero() ||
		(snapshot.AcousticCapability != AcousticCapabilityEnabled &&
			snapshot.AcousticCapability != AcousticCapabilityNotConfigured) ||
		!validJSONObject(snapshot.PlanSnapshot) ||
		!validJSONArray(snapshot.Participants) ||
		len(snapshot.Questions) == 0 || len(snapshot.Questions) > 128 ||
		len(snapshot.Turns) == 0 || len(snapshot.Turns) > 128 {
		return false
	}
	if snapshot.ProfileResolution != "" &&
		snapshot.ProfileResolution != IELTSFinalProfileResolved &&
		snapshot.ProfileResolution != IELTSFinalProfileFallback {
		return false
	}
	if (snapshot.ProfileResolution == IELTSFinalProfileResolved) !=
		(snapshot.CumulativeProfile != nil) ||
		(snapshot.CumulativeProfile != nil &&
			(!snapshot.CumulativeProfile.Valid() ||
				snapshot.CumulativeProfile.SessionID != snapshot.SessionID ||
				len(snapshot.CumulativeProfile.CompletedParts) != 2)) {
		return false
	}
	questionIDs := make(map[string]struct{}, len(snapshot.Questions))
	positions := make(map[int]struct{}, len(snapshot.Questions))
	for _, question := range snapshot.Questions {
		if !validSessionEvidenceQuestion(question) {
			return false
		}
		if _, exists := questionIDs[question.ID]; exists {
			return false
		}
		if _, exists := positions[question.Position]; exists {
			return false
		}
		questionIDs[question.ID] = struct{}{}
		positions[question.Position] = struct{}{}
	}
	for _, question := range snapshot.Questions {
		if question.ParentQuestionID != "" {
			if _, exists := questionIDs[question.ParentQuestionID]; !exists ||
				question.ParentQuestionID == question.ID {
				return false
			}
		}
	}
	turnIDs := make(map[string]struct{}, len(snapshot.Turns))
	turnPositions := make(map[int]struct{}, len(snapshot.Turns))
	for _, turn := range snapshot.Turns {
		if !validSessionEvidenceTurn(turn) {
			return false
		}
		if _, exists := questionIDs[turn.QuestionID]; !exists {
			return false
		}
		if _, exists := turnIDs[turn.ID]; exists {
			return false
		}
		if _, exists := turnPositions[turn.Position]; exists {
			return false
		}
		turnIDs[turn.ID] = struct{}{}
		turnPositions[turn.Position] = struct{}{}
	}
	return true
}

func validSessionEvidenceQuestion(question SessionEvidenceQuestion) bool {
	if !validUUID(question.ID) || question.Position < 1 ||
		(question.ParentQuestionID != "" && !validUUID(question.ParentQuestionID)) ||
		strings.TrimSpace(question.Text) == "" || len(question.Text) > 16*1024 ||
		!validIdentifier(question.SpeakerParticipantID) ||
		len(question.AddresseeParticipantIDs) == 0 ||
		len(question.AddresseeParticipantIDs) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(question.AddresseeParticipantIDs))
	for _, participantID := range question.AddresseeParticipantIDs {
		if !validIdentifier(participantID) {
			return false
		}
		if _, exists := seen[participantID]; exists {
			return false
		}
		seen[participantID] = struct{}{}
	}
	return true
}

func validSessionEvidenceTurn(turn SessionEvidenceTurn) bool {
	return validUUID(turn.ID) && turn.Position > 0 &&
		validUUID(turn.QuestionID) &&
		validIdentifier(turn.RespondentParticipantID) &&
		strings.TrimSpace(turn.Transcript) != "" &&
		len(turn.Transcript) <= 64*1024 && !turn.ConfirmedAt.IsZero() &&
		(turn.AudioAssetID == "" || validIdentifier(turn.AudioAssetID)) &&
		(turn.Acoustic == nil || turn.Acoustic.Valid()) &&
		(turn.Acoustic == nil || turn.Acoustic.Status != AcousticAssessed ||
			turn.AudioAssetID != "")
}

type AcousticAssessmentStatus string

const (
	AcousticAssessed    AcousticAssessmentStatus = "ASSESSED"
	AcousticNotAssessed AcousticAssessmentStatus = "NOT_ASSESSED"
)

type AcousticCheckpoint struct {
	Status           AcousticAssessmentStatus `json:"status"`
	Reason           string                   `json:"reason,omitempty"`
	Pronunciation    *float64                 `json:"pronunciation,omitempty"`
	Fluency          *float64                 `json:"fluency,omitempty"`
	Integrity        *float64                 `json:"integrity,omitempty"`
	SpeakingSpeedWPM *float64                 `json:"speaking_speed_wpm,omitempty"`
	Provider         string                   `json:"provider,omitempty"`
	ProviderSession  string                   `json:"provider_session,omitempty"`
}

func (checkpoint AcousticCheckpoint) Valid() bool {
	switch checkpoint.Status {
	case AcousticNotAssessed:
		return strings.TrimSpace(checkpoint.Reason) != "" &&
			checkpoint.Pronunciation == nil && checkpoint.Fluency == nil &&
			checkpoint.Integrity == nil && checkpoint.SpeakingSpeedWPM == nil &&
			checkpoint.Provider == "" && checkpoint.ProviderSession == ""
	case AcousticAssessed:
		return checkpoint.Reason == "" && checkpoint.Pronunciation != nil &&
			validScore(*checkpoint.Pronunciation) &&
			validOptionalScore(checkpoint.Fluency) &&
			validOptionalScore(checkpoint.Integrity) &&
			validOptionalNonNegative(checkpoint.SpeakingSpeedWPM) &&
			validIdentifier(checkpoint.Provider) &&
			validProviderSession(checkpoint.ProviderSession)
	default:
		return false
	}
}

func validProviderSession(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && len(value) <= 256 &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

type SpeechInputSnapshot struct {
	SchemaVersion string              `json:"schema_version"`
	Transcript    string              `json:"transcript"`
	EvidenceRefID string              `json:"evidence_ref_id"`
	QuestionID    string              `json:"question_id,omitempty"`
	PromptText    string              `json:"prompt_text,omitempty"`
	AudioAssetID  string              `json:"audio_asset_id,omitempty"`
	Acoustic      *AcousticCheckpoint `json:"acoustic,omitempty"`
}

type SpeechResult struct {
	SchemaVersion      string             `json:"schema_version"`
	ScoreabilityStatus string             `json:"scoreability_status"`
	Summary            string             `json:"summary"`
	ReasonCodes        []string           `json:"reason_codes"`
	Acoustic           AcousticCheckpoint `json:"acoustic"`
}

func (result SpeechResult) Valid() bool {
	if result.SchemaVersion != "speech-feedback/v1" ||
		(result.ScoreabilityStatus != "PROVISIONAL" &&
			result.ScoreabilityStatus != "INSUFFICIENT") ||
		strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 2048 ||
		result.ReasonCodes == nil || len(result.ReasonCodes) > 8 ||
		!result.Acoustic.Valid() {
		return false
	}
	seen := make(map[string]struct{}, len(result.ReasonCodes))
	for _, reason := range result.ReasonCodes {
		if !validIdentifier(reason) {
			return false
		}
		if _, exists := seen[reason]; exists {
			return false
		}
		seen[reason] = struct{}{}
	}
	return true
}

func (snapshot SpeechInputSnapshot) Valid(kind Kind) bool {
	if snapshot.SchemaVersion != SpeechInputSchemaVersion ||
		strings.TrimSpace(snapshot.Transcript) == "" ||
		len(snapshot.Transcript) > 16*1024 ||
		!validUUID(snapshot.EvidenceRefID) ||
		len(snapshot.PromptText) > 16*1024 ||
		(snapshot.AudioAssetID != "" && !validIdentifier(snapshot.AudioAssetID)) ||
		(snapshot.Acoustic != nil && !snapshot.Acoustic.Valid()) {
		return false
	}
	switch kind {
	case KindPracticeTurnFeedback:
		return validUUID(snapshot.QuestionID)
	case KindAgentMessageFeedback:
		return snapshot.QuestionID == "" && snapshot.AudioAssetID == "" &&
			snapshot.Acoustic != nil &&
			snapshot.Acoustic.Status == AcousticNotAssessed
	default:
		return false
	}
}

type Record struct {
	ID             string
	UserID         string
	Kind           Kind
	SourceID       string
	ContextID      string
	Status         JobStatus
	InputSnapshot  json.RawMessage
	InputHash      [sha256.Size]byte
	ConfigLineage  json.RawMessage
	ConfigHash     [sha256.Size]byte
	Result         json.RawMessage
	AttemptCount   int
	LeaseToken     string
	LeaseExpiresAt *time.Time
	AvailableAt    time.Time
	Error          *JobError
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

func (record Record) Valid() bool {
	if !validUUID(record.ID) || !validUUID(record.UserID) ||
		!record.Kind.Valid() || !validUUID(record.SourceID) ||
		!validUUID(record.ContextID) ||
		!record.Status.Valid() || len(record.InputSnapshot) == 0 ||
		record.InputHash == [sha256.Size]byte{} ||
		len(record.ConfigLineage) == 0 ||
		record.ConfigHash == [sha256.Size]byte{} || record.AttemptCount < 0 ||
		record.AvailableAt.IsZero() || record.CreatedAt.IsZero() ||
		record.UpdatedAt.Before(record.CreatedAt) {
		return false
	}
	if err := validateRecordJSON(record); err != nil {
		return false
	}
	switch record.Status {
	case JobQueued:
		return record.LeaseToken == "" && record.LeaseExpiresAt == nil &&
			record.Error == nil && record.FinishedAt == nil
	case JobRunning:
		return validUUID(record.LeaseToken) && record.LeaseExpiresAt != nil &&
			record.LeaseExpiresAt.After(record.UpdatedAt) &&
			record.AttemptCount > 0 && record.Error == nil &&
			record.StartedAt != nil && record.FinishedAt == nil
	case JobReady:
		return record.LeaseToken == "" && record.LeaseExpiresAt == nil &&
			len(record.Result) != 0 && record.Error == nil &&
			record.FinishedAt != nil
	case JobFailed:
		return record.LeaseToken == "" && record.LeaseExpiresAt == nil &&
			len(record.Result) == 0 && record.Error != nil &&
			record.Error.Valid() && record.FinishedAt != nil
	default:
		return false
	}
}

func validateRecordJSON(record Record) error {
	if sha256.Sum256(record.InputSnapshot) != record.InputHash ||
		sha256.Sum256(record.ConfigLineage) != record.ConfigHash {
		return ErrInvalidRequest
	}
	var lineage ConfigLineage
	if err := DecodeStrict(record.ConfigLineage, &lineage); err != nil ||
		!lineage.Valid() {
		return ErrInvalidRequest
	}
	switch record.Kind {
	case KindSessionReport:
		var snapshot SessionInputSnapshot
		if err := DecodeStrict(record.InputSnapshot, &snapshot); err != nil ||
			!snapshot.Valid() || snapshot.SessionID != record.SourceID ||
			snapshot.SessionID != record.ContextID {
			return ErrInvalidRequest
		}
	case KindPracticeTurnFeedback, KindAgentMessageFeedback:
		var snapshot SpeechInputSnapshot
		if err := DecodeStrict(record.InputSnapshot, &snapshot); err != nil ||
			!snapshot.Valid(record.Kind) ||
			snapshot.EvidenceRefID != record.SourceID {
			return ErrInvalidRequest
		}
	case KindIELTSPart1Profile, KindIELTSPart2Profile:
		var snapshot IELTSProfileInputSnapshot
		if err := DecodeStrict(record.InputSnapshot, &snapshot); err != nil ||
			!snapshot.Valid() || snapshot.SessionID != record.SourceID ||
			snapshot.SessionID != record.ContextID ||
			(record.Kind == KindIELTSPart1Profile && snapshot.Stage != IELTSProfileStagePart1) ||
			(record.Kind == KindIELTSPart2Profile && snapshot.Stage != IELTSProfileStagePart2) {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	if len(record.Result) != 0 && !json.Valid(record.Result) {
		return ErrInvalidRequest
	}
	return nil
}

type FeedbackEvidence struct {
	EvidenceRefID   string `json:"evidence_ref_id"`
	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

func (evidence FeedbackEvidence) Valid() bool {
	return validUUID(evidence.EvidenceRefID) &&
		utf8.ValidString(evidence.OriginalExcerpt) &&
		evidence.StartUTF8Byte >= 0 &&
		evidence.EndUTF8Byte > evidence.StartUTF8Byte &&
		evidence.EndUTF8Byte-evidence.StartUTF8Byte == len(evidence.OriginalExcerpt)
}

type FeedbackItem struct {
	ID             string           `json:"feedback_item_id"`
	EvaluationID   string           `json:"evaluation_id"`
	Position       int              `json:"position"`
	Category       string           `json:"category"`
	Severity       string           `json:"severity,omitempty"`
	Evidence       FeedbackEvidence `json:"evidence"`
	Recommendation string           `json:"recommendation"`
	Correction     string           `json:"correction,omitempty"`
	RepracticeMode string           `json:"repractice_mode"`
	CreatedAt      time.Time        `json:"created_at"`
}

func (item FeedbackItem) Valid() bool {
	return validUUID(item.ID) && validUUID(item.EvaluationID) &&
		item.Position > 0 && validIdentifier(item.Category) &&
		(item.Severity == "" || validIdentifier(item.Severity)) &&
		item.Evidence.Valid() &&
		strings.TrimSpace(item.Recommendation) != "" &&
		len(item.Recommendation) <= 4096 && len(item.Correction) <= 4096 &&
		validRepracticeMode(item.RepracticeMode) && !item.CreatedAt.IsZero()
}

type FeedbackItemDraft struct {
	Category       string
	Severity       string
	Evidence       FeedbackEvidence
	Recommendation string
	Correction     string
	RepracticeMode string
}

func (item FeedbackItemDraft) Valid() bool {
	return validIdentifier(item.Category) &&
		(item.Severity == "" || validIdentifier(item.Severity)) &&
		item.Evidence.Valid() && strings.TrimSpace(item.Recommendation) != "" &&
		len(item.Recommendation) <= 4096 && len(item.Correction) <= 4096 &&
		validRepracticeMode(item.RepracticeMode)
}

func validRepracticeMode(value string) bool {
	return value == "NONE" || value == "SAME_QUESTION"
}

func EncodeStrict[T any](value T) (json.RawMessage, [sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, [sha256.Size]byte{}, errors.Join(ErrInvalidRequest, err)
	}
	return encoded, sha256.Sum256(encoded), nil
}

func DecodeStrict(data json.RawMessage, target any) error {
	if len(data) == 0 || target == nil {
		return ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.Join(ErrInvalidRequest, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ErrInvalidRequest
	}
	return nil
}

func HashHex(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}

func validJSONObject(data json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var object json.RawMessage
	if err := decoder.Decode(&object); err != nil || len(object) == 0 || object[0] != '{' {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func validJSONArray(data json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var array []json.RawMessage
	if err := decoder.Decode(&array); err != nil || array == nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func validScore(value float64) bool {
	return value >= 0 && value <= 100
}

func validOptionalScore(value *float64) bool {
	return value == nil || validScore(*value)
}

func validOptionalNonNegative(value *float64) bool {
	return value == nil || *value >= 0
}
