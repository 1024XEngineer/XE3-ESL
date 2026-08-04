package evaluation

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

var ErrSpeechFeedbackAcousticUnavailable = errors.New(
	"review: SpeechFeedback acoustic assessment unavailable",
)

type SpeechFeedbackAcousticInput struct {
	OwnerUserID       string
	AudioAssetID      string
	AudioAssetVersion int64
	AudioChecksum     string
	AudioObjectKey    string
	ConfirmedText     string
	PromptText        string
}

type SpeechFeedbackAcousticEvidence struct {
	Assessment      SpeechFeedbackAcousticAssessment
	RawResult       string
	AvailableFields []AcousticAssessmentField
}

func (evidence SpeechFeedbackAcousticEvidence) valid() bool {
	return evidence.Assessment.valid() &&
		evidence.Assessment.Pronunciation == SpeechFeedbackAssessed &&
		strings.TrimSpace(evidence.RawResult) != "" &&
		len(evidence.RawResult) <= 1024*1024
}

type SpeechFeedbackAudioReader interface {
	ReadSpeechFeedbackAudio(
		context.Context,
		string,
		string,
		string,
		string,
	) ([]byte, error)
}

type AcousticAssessmentCategory string

const (
	AcousticCategoryReadWord     AcousticAssessmentCategory = "read_word"
	AcousticCategoryReadSentence AcousticAssessmentCategory = "read_sentence"
	AcousticCategoryTopic        AcousticAssessmentCategory = "topic"
)

type AcousticAssessmentRequest struct {
	Audio         []byte
	ReferenceText string
	TopicTitle    string
	Category      AcousticAssessmentCategory
}

type AcousticAssessmentResult struct {
	Provider        string
	SessionID       string
	RawResult       string
	AvailableFields []AcousticAssessmentField
	Summary         AcousticAssessmentSummary
}

type AcousticAssessmentField struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AcousticAssessmentSummary struct {
	AccuracyScore  *float64
	FluencyScore   *float64
	IntegrityScore *float64
	PhoneScore     *float64
	SpeakingSpeed  *float64
	Rejected       *bool
	ExceptionInfo  string
}

type AcousticEvaluator interface {
	Evaluate(
		context.Context,
		AcousticAssessmentRequest,
	) (AcousticAssessmentResult, error)
}

type SpeechFeedbackAcousticProvider interface {
	EvaluateSpeechFeedbackAcoustics(
		context.Context,
		SpeechFeedbackAcousticInput,
	) (SpeechFeedbackAcousticEvidence, error)
}

type speechFeedbackAcousticProvider struct {
	audio     SpeechFeedbackAudioReader
	evaluator AcousticEvaluator
}

func NewSpeechFeedbackAcousticProvider(
	audio SpeechFeedbackAudioReader,
	evaluator AcousticEvaluator,
) (SpeechFeedbackAcousticProvider, error) {
	if audio == nil || evaluator == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &speechFeedbackAcousticProvider{
		audio:     audio,
		evaluator: evaluator,
	}, nil
}

func (provider *speechFeedbackAcousticProvider) EvaluateSpeechFeedbackAcoustics(
	ctx context.Context,
	input SpeechFeedbackAcousticInput,
) (SpeechFeedbackAcousticEvidence, error) {
	if provider == nil || provider.audio == nil ||
		provider.evaluator == nil || ctx == nil ||
		!validUUID(input.OwnerUserID) ||
		!validSpeechFeedbackIdentifier(input.AudioAssetID) ||
		input.AudioAssetVersion < 1 ||
		len(input.AudioChecksum) != 64 ||
		!speechFeedbackHasAssessableEnglish(input.ConfirmedText) {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	language := classifySpeechFeedbackLanguage(input.ConfirmedText)
	referenceText := strings.TrimSpace(input.ConfirmedText)
	if language == speechFeedbackLanguageMixed {
		referenceText = speechFeedbackEnglishReferenceText(input.ConfirmedText)
	}
	if referenceText == "" {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	audio, err := provider.audio.ReadSpeechFeedbackAudio(
		ctx,
		input.OwnerUserID,
		input.AudioAssetID,
		input.AudioObjectKey,
		input.AudioChecksum,
	)
	if err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	pcm, err := speechFeedbackPCM16Mono(audio)
	if err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	category := speechFeedbackAcousticCategory(referenceText)
	result, err := provider.evaluator.Evaluate(
		ctx,
		AcousticAssessmentRequest{
			Audio:         pcm,
			ReferenceText: referenceText,
			// The scene prompt is used by the text evaluator for relevance and
			// task completion. Acoustic assessment compares the audio with the
			// confirmed answer, not with the question.
			Category: category,
		},
	)
	if err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	if err := validateSpeechFeedbackAcousticSummary(
		result.Summary,
		category,
	); err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	assessment := SpeechFeedbackAcousticAssessment{
		Pronunciation:   SpeechFeedbackAssessed,
		AcousticFluency: SpeechFeedbackAssessed,
		Provider:        strings.TrimSpace(result.Provider),
		ProviderSession: strings.TrimSpace(result.SessionID),
		Category:        string(category),
		Notice:          SpeechFeedbackAcousticNotice,
	}
	if category == AcousticCategoryTopic {
		assessment.PronunciationScore = result.Summary.PhoneScore
		assessment.SpeakingSpeedWPM = result.Summary.SpeakingSpeed
		assessment.SemanticScore = result.Summary.AccuracyScore
	} else {
		assessment.Integrity = SpeechFeedbackAssessed
		assessment.AccuracyScore = result.Summary.AccuracyScore
		assessment.FluencyScore = result.Summary.FluencyScore
		assessment.IntegrityScore = result.Summary.IntegrityScore
	}
	evidence := SpeechFeedbackAcousticEvidence{
		Assessment:      assessment,
		RawResult:       result.RawResult,
		AvailableFields: result.AvailableFields,
	}
	if !evidence.valid() {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	return evidence, nil
}

func validateSpeechFeedbackAcousticSummary(
	summary AcousticAssessmentSummary,
	category AcousticAssessmentCategory,
) error {
	if summary.Rejected == nil && category != AcousticCategoryTopic {
		return fmt.Errorf(
			"%w: result is missing is_rejected",
			ErrSpeechFeedbackAcousticUnavailable,
		)
	}
	if summary.Rejected != nil && *summary.Rejected {
		return fmt.Errorf(
			"%w: speech was rejected (except_info=%s)",
			ErrSpeechFeedbackAcousticUnavailable,
			strings.TrimSpace(summary.ExceptionInfo),
		)
	}
	if category == AcousticCategoryTopic {
		missing := make([]string, 0, 3)
		if !validSpeechFeedbackScore(summary.PhoneScore) {
			missing = append(missing, "phone_score")
		}
		if !validSpeechFeedbackSpeakingSpeed(summary.SpeakingSpeed) {
			missing = append(missing, "speeking_speed")
		}
		if !validSpeechFeedbackScore(summary.AccuracyScore) {
			missing = append(missing, "accuracy_score")
		}
		if len(missing) != 0 {
			return fmt.Errorf(
				"%w: topic result is missing fields %s",
				ErrSpeechFeedbackAcousticUnavailable,
				strings.Join(missing, ","),
			)
		}
		return nil
	}
	missing := make([]string, 0, 3)
	if !validSpeechFeedbackScore(summary.AccuracyScore) {
		missing = append(missing, "accuracy_score")
	}
	if !validSpeechFeedbackScore(summary.FluencyScore) {
		missing = append(missing, "fluency_score")
	}
	if !validSpeechFeedbackScore(summary.IntegrityScore) {
		missing = append(missing, "integrity_score")
	}
	if len(missing) != 0 {
		return fmt.Errorf(
			"%w: result is missing full-dimension fields %s",
			ErrSpeechFeedbackAcousticUnavailable,
			strings.Join(missing, ","),
		)
	}
	return nil
}

func speechFeedbackPCM16Mono(wav []byte) ([]byte, error) {
	if len(wav) < 12 ||
		string(wav[:4]) != "RIFF" ||
		string(wav[8:12]) != "WAVE" {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	var (
		foundFormat bool
		pcm         []byte
	)
	for offset := 12; offset+8 <= len(wav); {
		chunkSize := int(binary.LittleEndian.Uint32(
			wav[offset+4 : offset+8],
		))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wav) {
			return nil, ErrSpeechFeedbackAcousticUnavailable
		}
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if foundFormat || chunkSize < 16 ||
				binary.LittleEndian.Uint16(
					wav[chunkStart:chunkStart+2],
				) != 1 ||
				binary.LittleEndian.Uint16(
					wav[chunkStart+2:chunkStart+4],
				) != 1 ||
				binary.LittleEndian.Uint32(
					wav[chunkStart+4:chunkStart+8],
				) != 16_000 ||
				binary.LittleEndian.Uint16(
					wav[chunkStart+14:chunkStart+16],
				) != 16 {
				return nil, ErrSpeechFeedbackAcousticUnavailable
			}
			foundFormat = true
		case "data":
			if pcm != nil || chunkSize == 0 {
				return nil, ErrSpeechFeedbackAcousticUnavailable
			}
			pcm = wav[chunkStart:chunkEnd]
		}
		offset = chunkEnd + chunkSize%2
	}
	if !foundFormat || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	return pcm, nil
}

type SpeechFeedbackAcousticRepository interface {
	SaveSpeechFeedbackAcousticEvidence(
		context.Context,
		SpeechFeedbackClaim,
		SpeechFeedbackAcousticEvidence,
	) error
}
