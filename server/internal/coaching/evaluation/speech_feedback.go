package evaluation

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SpeechFeedbackSchemaVersion   = "speech-feedback/v1"
	SpeechFeedbackStrategyRef     = "qianwen-speech-feedback/v1"
	SpeechFeedbackPipelineVersion = "speech-feedback-pipeline/v1"
	SpeechFeedbackPromptVersion   = "speech-feedback-prompt/v3"

	SpeechFeedbackAcousticReasonUnavailable = "ACOUSTIC_EVIDENCE_UNAVAILABLE"
	SpeechFeedbackAcousticNotice            = "根据本次录音自动评估，仅供练习参考。"
)

var (
	ErrInvalidSpeechFeedback       = errors.New("evaluation: invalid SpeechFeedback")
	ErrSpeechFeedbackNotFound      = errors.New("evaluation: SpeechFeedback not found")
	ErrSpeechFeedbackNotApplicable = errors.New("evaluation: SpeechFeedback not applicable")
	ErrSpeechFeedbackConflict      = errors.New("evaluation: SpeechFeedback source conflict")
	ErrSpeechFeedbackClaimLost     = errors.New("evaluation: SpeechFeedback claim lost")
)

type SpeechFeedbackSourceKind string

const (
	SpeechFeedbackSourceConversationTurn  SpeechFeedbackSourceKind = "CONVERSATION_TURN"
	SpeechFeedbackSourceAgentVoiceMessage SpeechFeedbackSourceKind = "AGENT_VOICE_MESSAGE"
)

type SpeechFeedbackSource struct {
	SourceKind SpeechFeedbackSourceKind `json:"source_kind"`

	PracticeSessionID  string `json:"practice_session_id,omitempty"`
	TurnID             string `json:"turn_id,omitempty"`
	InputRevision      int64  `json:"input_revision,omitempty"`
	EvidenceSnapshotID string `json:"evidence_snapshot_id,omitempty"`

	ThreadID             string `json:"thread_id,omitempty"`
	MessageID            string `json:"message_id,omitempty"`
	TranscriptEvidenceID string `json:"transcript_evidence_id,omitempty"`
	CandidateVersion     int64  `json:"candidate_version,omitempty"`
}

func (source SpeechFeedbackSource) valid() bool {
	switch source.SourceKind {
	case SpeechFeedbackSourceConversationTurn:
		return validSpeechFeedbackIdentifier(source.PracticeSessionID) &&
			validSpeechFeedbackIdentifier(source.TurnID) &&
			source.InputRevision > 0 &&
			validSpeechFeedbackIdentifier(source.EvidenceSnapshotID) &&
			source.ThreadID == "" &&
			source.MessageID == "" &&
			source.TranscriptEvidenceID == "" &&
			source.CandidateVersion == 0
	case SpeechFeedbackSourceAgentVoiceMessage:
		return validUUID(source.ThreadID) &&
			validUUID(source.MessageID) &&
			validUUID(source.TranscriptEvidenceID) &&
			source.CandidateVersion > 0 &&
			source.PracticeSessionID == "" &&
			source.TurnID == "" &&
			source.InputRevision == 0 &&
			source.EvidenceSnapshotID == ""
	default:
		return false
	}
}

type SpeechFeedbackStatus string

const (
	SpeechFeedbackQueued  SpeechFeedbackStatus = "QUEUED"
	SpeechFeedbackRunning SpeechFeedbackStatus = "RUNNING"
	SpeechFeedbackReady   SpeechFeedbackStatus = "READY"
	SpeechFeedbackFailed  SpeechFeedbackStatus = "FAILED"
)

type SpeechFeedbackScoreabilityStatus string

const (
	SpeechFeedbackProvisional  SpeechFeedbackScoreabilityStatus = "PROVISIONAL"
	SpeechFeedbackInsufficient SpeechFeedbackScoreabilityStatus = "INSUFFICIENT"
)

type SpeechFeedbackGateStatus string

const (
	SpeechFeedbackFeedbackOnly SpeechFeedbackGateStatus = "FEEDBACK_ONLY"
	SpeechFeedbackBlocked      SpeechFeedbackGateStatus = "BLOCKED"
)

type SpeechFeedbackReasonCode string

const (
	SpeechFeedbackReasonTextTooShort                     SpeechFeedbackReasonCode = "TEXT_TOO_SHORT"
	SpeechFeedbackReasonTranscriptConfidenceInsufficient SpeechFeedbackReasonCode = "TRANSCRIPT_CONFIDENCE_INSUFFICIENT"
	SpeechFeedbackReasonEvidenceInconsistent             SpeechFeedbackReasonCode = "EVIDENCE_INCONSISTENT"
)

func (code SpeechFeedbackReasonCode) valid() bool {
	switch code {
	case SpeechFeedbackReasonTextTooShort,
		SpeechFeedbackReasonTranscriptConfidenceInsufficient,
		SpeechFeedbackReasonEvidenceInconsistent:
		return true
	default:
		return false
	}
}

type SpeechFeedbackFailureCode string

const (
	SpeechFeedbackFailureProviderUnavailable     SpeechFeedbackFailureCode = "PROVIDER_UNAVAILABLE"
	SpeechFeedbackFailureProviderResponseInvalid SpeechFeedbackFailureCode = "PROVIDER_RESPONSE_INVALID"
	SpeechFeedbackFailureProcessingTimeout       SpeechFeedbackFailureCode = "PROCESSING_TIMEOUT"
	SpeechFeedbackFailureInternalProcessing      SpeechFeedbackFailureCode = "INTERNAL_PROCESSING_ERROR"
)

func (code SpeechFeedbackFailureCode) valid() bool {
	switch code {
	case SpeechFeedbackFailureProviderUnavailable,
		SpeechFeedbackFailureProviderResponseInvalid,
		SpeechFeedbackFailureProcessingTimeout,
		SpeechFeedbackFailureInternalProcessing:
		return true
	default:
		return false
	}
}

type SpeechFeedbackStableFailure struct {
	ReasonCode SpeechFeedbackFailureCode `json:"reason_code"`
	Retryable  bool                      `json:"retryable"`
}

func (failure SpeechFeedbackStableFailure) valid() bool {
	return failure.ReasonCode.valid()
}

type SpeechFeedbackItemKind string

const (
	SpeechFeedbackItemCorrection            SpeechFeedbackItemKind = "CORRECTION"
	SpeechFeedbackItemStrength              SpeechFeedbackItemKind = "STRENGTH"
	SpeechFeedbackItemImprovement           SpeechFeedbackItemKind = "IMPROVEMENT"
	SpeechFeedbackItemRecommendedExpression SpeechFeedbackItemKind = "RECOMMENDED_EXPRESSION"
)

func (kind SpeechFeedbackItemKind) valid() bool {
	switch kind {
	case SpeechFeedbackItemCorrection,
		SpeechFeedbackItemStrength,
		SpeechFeedbackItemImprovement,
		SpeechFeedbackItemRecommendedExpression:
		return true
	default:
		return false
	}
}

type SpeechFeedbackAnchorKind string

const (
	SpeechFeedbackAnchorConversationTranscript SpeechFeedbackAnchorKind = "CONVERSATION_TRANSCRIPT"
	SpeechFeedbackAnchorAgentTranscript        SpeechFeedbackAnchorKind = "AGENT_TRANSCRIPT"
)

type SpeechFeedbackAnchor struct {
	AnchorKind SpeechFeedbackAnchorKind `json:"anchor_kind"`

	EvidenceRefID string `json:"evidence_ref_id,omitempty"`
	TurnID        string `json:"turn_id,omitempty"`

	TranscriptEvidenceID string `json:"transcript_evidence_id,omitempty"`
	MessageID            string `json:"message_id,omitempty"`

	StartUTF8Byte   int    `json:"start_utf8_byte"`
	EndUTF8Byte     int    `json:"end_utf8_byte"`
	OriginalExcerpt string `json:"original_excerpt"`
}

func (anchor SpeechFeedbackAnchor) validFor(
	source SpeechFeedbackSource,
	evidenceRefID string,
	canonicalText string,
) bool {
	if !utf8.ValidString(canonicalText) ||
		anchor.StartUTF8Byte < 0 ||
		anchor.EndUTF8Byte <= anchor.StartUTF8Byte ||
		anchor.EndUTF8Byte > len(canonicalText) ||
		(anchor.StartUTF8Byte > 0 &&
			!utf8.RuneStart(canonicalText[anchor.StartUTF8Byte])) ||
		(anchor.EndUTF8Byte < len(canonicalText) &&
			!utf8.RuneStart(canonicalText[anchor.EndUTF8Byte])) ||
		anchor.OriginalExcerpt !=
			canonicalText[anchor.StartUTF8Byte:anchor.EndUTF8Byte] {
		return false
	}
	switch source.SourceKind {
	case SpeechFeedbackSourceConversationTurn:
		return anchor.AnchorKind ==
			SpeechFeedbackAnchorConversationTranscript &&
			validSpeechFeedbackIdentifier(evidenceRefID) &&
			anchor.EvidenceRefID == evidenceRefID &&
			anchor.TurnID == source.TurnID &&
			anchor.TranscriptEvidenceID == "" &&
			anchor.MessageID == ""
	case SpeechFeedbackSourceAgentVoiceMessage:
		return anchor.AnchorKind ==
			SpeechFeedbackAnchorAgentTranscript &&
			anchor.TranscriptEvidenceID == source.TranscriptEvidenceID &&
			anchor.MessageID == source.MessageID &&
			evidenceRefID == "" &&
			anchor.EvidenceRefID == "" &&
			anchor.TurnID == ""
	default:
		return false
	}
}

type SpeechFeedbackRepracticeMode string

const (
	SpeechFeedbackRepracticeNone         SpeechFeedbackRepracticeMode = "NONE"
	SpeechFeedbackRepracticeSameQuestion SpeechFeedbackRepracticeMode = "SAME_QUESTION"
	SpeechFeedbackRepracticeSameThread   SpeechFeedbackRepracticeMode = "SAME_THREAD"
)

type SpeechFeedbackItem struct {
	FeedbackItemID   string                       `json:"feedback_item_id"`
	SpeechFeedbackID string                       `json:"speech_feedback_id"`
	Kind             SpeechFeedbackItemKind       `json:"kind"`
	Anchor           SpeechFeedbackAnchor         `json:"anchor"`
	Explanation      string                       `json:"explanation"`
	SuggestedText    *string                      `json:"suggested_text,omitempty"`
	RepracticeMode   SpeechFeedbackRepracticeMode `json:"repractice_mode"`
	CreatedAt        time.Time                    `json:"created_at"`
}

func (item SpeechFeedbackItem) validFor(
	source SpeechFeedbackSource,
	evidenceRefID string,
	canonicalText string,
) bool {
	if !validUUID(item.FeedbackItemID) ||
		!validUUID(item.SpeechFeedbackID) ||
		!item.Kind.valid() ||
		!item.Anchor.validFor(source, evidenceRefID, canonicalText) ||
		!validSpeechFeedbackAdviceText(item.Explanation) ||
		item.CreatedAt.IsZero() {
		return false
	}
	switch item.Kind {
	case SpeechFeedbackItemStrength:
		if item.SuggestedText != nil ||
			item.RepracticeMode != SpeechFeedbackRepracticeNone {
			return false
		}
	default:
		if item.SuggestedText == nil ||
			!validSpeechFeedbackAdviceText(*item.SuggestedText) {
			return false
		}
		switch source.SourceKind {
		case SpeechFeedbackSourceConversationTurn:
			if item.RepracticeMode !=
				SpeechFeedbackRepracticeSameQuestion {
				return false
			}
		case SpeechFeedbackSourceAgentVoiceMessage:
			if item.RepracticeMode != SpeechFeedbackRepracticeSameThread {
				return false
			}
		default:
			return false
		}
	}
	return true
}

type SpeechFeedbackAssessmentStatus string

const (
	SpeechFeedbackNotAssessed SpeechFeedbackAssessmentStatus = "NOT_ASSESSED"
	SpeechFeedbackAssessed    SpeechFeedbackAssessmentStatus = "ASSESSED"
)

type SpeechFeedbackAcousticAssessment struct {
	Pronunciation      SpeechFeedbackAssessmentStatus `json:"pronunciation"`
	AcousticFluency    SpeechFeedbackAssessmentStatus `json:"acoustic_fluency"`
	Integrity          SpeechFeedbackAssessmentStatus `json:"integrity,omitempty"`
	AccuracyScore      *float64                       `json:"accuracy_score,omitempty"`
	FluencyScore       *float64                       `json:"fluency_score,omitempty"`
	IntegrityScore     *float64                       `json:"integrity_score,omitempty"`
	PronunciationScore *float64                       `json:"pronunciation_score,omitempty"`
	SpeakingSpeedWPM   *float64                       `json:"speaking_speed_wpm,omitempty"`
	SemanticScore      *float64                       `json:"semantic_score,omitempty"`
	Provider           string                         `json:"provider,omitempty"`
	ProviderSession    string                         `json:"provider_session_id,omitempty"`
	Category           string                         `json:"category,omitempty"`
	ReasonCode         string                         `json:"reason_code,omitempty"`
	Notice             string                         `json:"notice,omitempty"`
}

func unavailableSpeechFeedbackAcoustics() SpeechFeedbackAcousticAssessment {
	return SpeechFeedbackAcousticAssessment{
		Pronunciation:   SpeechFeedbackNotAssessed,
		AcousticFluency: SpeechFeedbackNotAssessed,
		ReasonCode:      SpeechFeedbackAcousticReasonUnavailable,
	}
}

func (assessment SpeechFeedbackAcousticAssessment) valid() bool {
	if assessment.Pronunciation == SpeechFeedbackNotAssessed &&
		assessment.AcousticFluency == SpeechFeedbackNotAssessed &&
		assessment.ReasonCode ==
			SpeechFeedbackAcousticReasonUnavailable {
		return assessment.Integrity == "" &&
			assessment.AccuracyScore == nil &&
			assessment.FluencyScore == nil &&
			assessment.IntegrityScore == nil &&
			assessment.PronunciationScore == nil &&
			assessment.SpeakingSpeedWPM == nil &&
			assessment.SemanticScore == nil &&
			assessment.Provider == "" &&
			assessment.ProviderSession == "" &&
			assessment.Category == "" &&
			assessment.Notice == ""
	}
	if assessment.Pronunciation != SpeechFeedbackAssessed ||
		assessment.AcousticFluency != SpeechFeedbackAssessed ||
		!validSpeechFeedbackIdentifier(assessment.Provider) ||
		!validSpeechFeedbackProviderSession(assessment.ProviderSession) ||
		assessment.ReasonCode != "" ||
		assessment.Notice != SpeechFeedbackAcousticNotice {
		return false
	}
	switch assessment.Category {
	case "read_word", "read_sentence":
		return assessment.Integrity == SpeechFeedbackAssessed &&
			validSpeechFeedbackScore(assessment.AccuracyScore) &&
			validSpeechFeedbackScore(assessment.FluencyScore) &&
			validSpeechFeedbackScore(assessment.IntegrityScore) &&
			assessment.PronunciationScore == nil &&
			assessment.SpeakingSpeedWPM == nil &&
			assessment.SemanticScore == nil
	case "topic":
		return assessment.Integrity == "" &&
			assessment.AccuracyScore == nil &&
			assessment.FluencyScore == nil &&
			assessment.IntegrityScore == nil &&
			validSpeechFeedbackScore(assessment.PronunciationScore) &&
			validSpeechFeedbackSpeakingSpeed(
				assessment.SpeakingSpeedWPM,
			) &&
			validSpeechFeedbackScore(assessment.SemanticScore)
	default:
		return false
	}
}

func validSpeechFeedbackScore(score *float64) bool {
	return score != nil && *score >= 0 && *score <= 100
}

func validSpeechFeedbackSpeakingSpeed(speed *float64) bool {
	return speed != nil && *speed > 0 && *speed <= 1000
}

func validSpeechFeedbackProviderSession(value string) bool {
	return utf8.ValidString(value) &&
		strings.TrimSpace(value) == value &&
		value != "" &&
		len(value) <= 256 &&
		strings.IndexFunc(value, func(character rune) bool {
			return character < 0x20 || character == 0x7f
		}) == -1
}

type SpeechFeedback struct {
	SpeechFeedbackID   string                            `json:"speech_feedback_id"`
	Source             SpeechFeedbackSource              `json:"source"`
	FeedbackStatus     SpeechFeedbackStatus              `json:"feedback_status"`
	ScoreabilityStatus *SpeechFeedbackScoreabilityStatus `json:"scoreability_status,omitempty"`
	GateStatus         *SpeechFeedbackGateStatus         `json:"gate_status,omitempty"`
	ReasonCodes        []SpeechFeedbackReasonCode        `json:"reason_codes,omitempty"`
	SchemaVersion      string                            `json:"schema_version"`
	StrategyRef        string                            `json:"strategy_ref"`
	PipelineVersion    string                            `json:"pipeline_version"`
	IsFinal            bool                              `json:"is_final"`
	Items              []SpeechFeedbackItem              `json:"items"`
	AcousticAssessment SpeechFeedbackAcousticAssessment  `json:"acoustic_assessment"`
	StableFailure      *SpeechFeedbackStableFailure      `json:"stable_failure,omitempty"`
	StatusURL          string                            `json:"status_url"`
	CreatedAt          time.Time                         `json:"created_at"`
	UpdatedAt          time.Time                         `json:"updated_at"`
	CompletedAt        *time.Time                        `json:"completed_at,omitempty"`
}

func (feedback SpeechFeedback) valid(
	evidenceRefID string,
	canonicalText string,
) bool {
	if !validUUID(feedback.SpeechFeedbackID) ||
		!feedback.Source.valid() ||
		feedback.SchemaVersion != SpeechFeedbackSchemaVersion ||
		!validSpeechFeedbackVersion(feedback.StrategyRef) ||
		!validSpeechFeedbackVersion(feedback.PipelineVersion) ||
		feedback.IsFinal ||
		!feedback.AcousticAssessment.valid() ||
		feedback.StatusURL !=
			SpeechFeedbackStatusURL(feedback.SpeechFeedbackID) ||
		feedback.CreatedAt.IsZero() ||
		feedback.UpdatedAt.Before(feedback.CreatedAt) {
		return false
	}
	if len(feedback.ReasonCodes) > 3 ||
		len(feedback.Items) > maxSpeechFeedbackProviderItems {
		return false
	}
	seenReasons := make(
		map[SpeechFeedbackReasonCode]struct{},
		len(feedback.ReasonCodes),
	)
	for _, code := range feedback.ReasonCodes {
		if !code.valid() {
			return false
		}
		if _, duplicate := seenReasons[code]; duplicate {
			return false
		}
		seenReasons[code] = struct{}{}
	}
	switch feedback.FeedbackStatus {
	case SpeechFeedbackQueued, SpeechFeedbackRunning:
		return feedback.ScoreabilityStatus == nil &&
			feedback.GateStatus == nil &&
			len(feedback.ReasonCodes) == 0 &&
			len(feedback.Items) == 0 &&
			feedback.StableFailure == nil &&
			feedback.CompletedAt == nil
	case SpeechFeedbackReady:
		if feedback.CompletedAt == nil ||
			feedback.CompletedAt.Before(feedback.CreatedAt) ||
			feedback.ScoreabilityStatus == nil ||
			feedback.GateStatus == nil ||
			feedback.StableFailure != nil {
			return false
		}
		switch {
		case *feedback.ScoreabilityStatus == SpeechFeedbackProvisional &&
			*feedback.GateStatus == SpeechFeedbackFeedbackOnly &&
			len(feedback.ReasonCodes) == 0 &&
			len(feedback.Items) > 0:
			for _, item := range feedback.Items {
				if item.SpeechFeedbackID != feedback.SpeechFeedbackID ||
					!item.validFor(
						feedback.Source,
						evidenceRefID,
						canonicalText,
					) {
					return false
				}
			}
			return true
		case *feedback.ScoreabilityStatus == SpeechFeedbackInsufficient &&
			*feedback.GateStatus == SpeechFeedbackBlocked &&
			len(feedback.ReasonCodes) > 0 &&
			len(feedback.Items) == 0:
			return true
		default:
			return false
		}
	case SpeechFeedbackFailed:
		return feedback.ScoreabilityStatus == nil &&
			feedback.GateStatus == nil &&
			len(feedback.ReasonCodes) == 0 &&
			len(feedback.Items) == 0 &&
			feedback.StableFailure != nil &&
			feedback.StableFailure.valid() &&
			feedback.CompletedAt != nil &&
			!feedback.CompletedAt.Before(feedback.CreatedAt)
	default:
		return false
	}
}

type SpeechFeedbackReference struct {
	SpeechFeedbackID string `json:"speech_feedback_id"`
	StatusURL        string `json:"speech_feedback_status_url"`
}

func (reference SpeechFeedbackReference) valid() bool {
	return validUUID(reference.SpeechFeedbackID) &&
		reference.StatusURL ==
			SpeechFeedbackStatusURL(reference.SpeechFeedbackID)
}

func SpeechFeedbackStatusURL(speechFeedbackID string) string {
	if !validUUID(speechFeedbackID) {
		return ""
	}
	return "/v1/speech-feedback/" + speechFeedbackID
}

func validSpeechFeedbackIdentifier(value string) bool {
	if value == "" || len(value) > 128 ||
		value != strings.TrimSpace(value) {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '.', '_', ':', '-':
		default:
			return false
		}
	}
	return true
}

func validSpeechFeedbackVersion(value string) bool {
	return validSpeechFeedbackIdentifier(
		strings.NewReplacer("/", "-", "@", "-").Replace(value),
	) && len(value) <= 128
}

func validSpeechFeedbackText(value string, maximumBytes int) bool {
	if !utf8.ValidString(value) ||
		strings.TrimSpace(value) == "" ||
		len(value) > maximumBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 &&
			character != '\t' &&
			character != '\n' &&
			character != '\r' {
			return false
		}
		if character >= 0x7f && character <= 0x9f {
			return false
		}
	}
	return true
}

func validSpeechFeedbackAdviceText(value string) bool {
	return validSpeechFeedbackText(value, 2048) &&
		utf8.RuneCountInString(value) <= 1000
}

func invalidSpeechFeedbackField(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSpeechFeedback, field)
}
