package review

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
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
	AvailableFields []xfyun.ResultField
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

type SpeechFeedbackISEEvaluator interface {
	Evaluate(
		context.Context,
		xfyun.EvaluationRequest,
	) (xfyun.EvaluationResult, error)
}

type SpeechFeedbackAcousticProvider interface {
	EvaluateSpeechFeedbackAcoustics(
		context.Context,
		SpeechFeedbackAcousticInput,
	) (SpeechFeedbackAcousticEvidence, error)
}

type XFYUNSpeechFeedbackAcousticProvider struct {
	audio     SpeechFeedbackAudioReader
	evaluator SpeechFeedbackISEEvaluator
}

func NewXFYUNSpeechFeedbackAcousticProvider(
	audio SpeechFeedbackAudioReader,
	evaluator SpeechFeedbackISEEvaluator,
) (*XFYUNSpeechFeedbackAcousticProvider, error) {
	if audio == nil || evaluator == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &XFYUNSpeechFeedbackAcousticProvider{
		audio:     audio,
		evaluator: evaluator,
	}, nil
}

func (provider *XFYUNSpeechFeedbackAcousticProvider) EvaluateSpeechFeedbackAcoustics(
	ctx context.Context,
	input SpeechFeedbackAcousticInput,
) (SpeechFeedbackAcousticEvidence, error) {
	if provider == nil || provider.audio == nil ||
		provider.evaluator == nil || ctx == nil ||
		!validUUID(input.OwnerUserID) ||
		!validSpeechFeedbackIdentifier(input.AudioAssetID) ||
		input.AudioAssetVersion < 1 ||
		len(input.AudioChecksum) != 64 ||
		classifySpeechFeedbackLanguage(input.ConfirmedText) !=
			speechFeedbackLanguageEnglish {
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
	category := speechFeedbackISECategory(input.ConfirmedText)
	referenceText := input.ConfirmedText
	result, err := provider.evaluator.Evaluate(
		ctx,
		xfyun.EvaluationRequest{
			Audio:         pcm,
			ReferenceText: referenceText,
			// The scene prompt is used by the text evaluator for relevance and
			// task completion. ISE should compare the audio with the learner's
			// confirmed answer, not require the learner to repeat the question.
			Category: category,
		},
	)
	if err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	if err := validateSpeechFeedbackISESummary(
		result.Summary,
		category,
	); err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	assessment := SpeechFeedbackAcousticAssessment{
		Pronunciation:   SpeechFeedbackAssessed,
		AcousticFluency: SpeechFeedbackAssessed,
		Provider:        SpeechFeedbackAcousticProviderName,
		ProviderSession: strings.TrimSpace(result.SessionID),
		Category:        string(category),
		Notice:          SpeechFeedbackAcousticNotice,
	}
	if category == xfyun.CategoryTopic {
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
		RawResult:       result.RawXML,
		AvailableFields: result.AvailableFields,
	}
	if !evidence.valid() {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	return evidence, nil
}

func validateSpeechFeedbackISESummary(
	summary xfyun.ScoreSummary,
	category xfyun.EvaluationCategory,
) error {
	if summary.Rejected == nil && category != xfyun.CategoryTopic {
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
	if category == xfyun.CategoryTopic {
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
