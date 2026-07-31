package review

import (
	"context"
	"errors"
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
	category := speechFeedbackISECategory(input.ConfirmedText)
	result, err := provider.evaluator.Evaluate(
		ctx,
		xfyun.EvaluationRequest{
			Audio:         audio,
			ReferenceText: input.ConfirmedText,
			Category:      category,
		},
	)
	if err != nil {
		return SpeechFeedbackAcousticEvidence{}, err
	}
	if result.Summary.Rejected == nil ||
		*result.Summary.Rejected ||
		!validSpeechFeedbackScore(result.Summary.AccuracyScore) ||
		!validSpeechFeedbackScore(result.Summary.FluencyScore) ||
		!validSpeechFeedbackScore(result.Summary.IntegrityScore) {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	evidence := SpeechFeedbackAcousticEvidence{
		Assessment: SpeechFeedbackAcousticAssessment{
			Pronunciation:   SpeechFeedbackAssessed,
			AcousticFluency: SpeechFeedbackAssessed,
			Integrity:       SpeechFeedbackAssessed,
			AccuracyScore:   result.Summary.AccuracyScore,
			FluencyScore:    result.Summary.FluencyScore,
			IntegrityScore:  result.Summary.IntegrityScore,
			Provider:        SpeechFeedbackAcousticProviderName,
			ProviderSession: strings.TrimSpace(result.SessionID),
			Category:        string(category),
			Notice:          SpeechFeedbackAcousticNotice,
		},
		RawResult:       result.RawXML,
		AvailableFields: result.AvailableFields,
	}
	if !evidence.valid() {
		return SpeechFeedbackAcousticEvidence{},
			ErrSpeechFeedbackAcousticUnavailable
	}
	return evidence, nil
}

type SpeechFeedbackAcousticRepository interface {
	SaveSpeechFeedbackAcousticEvidence(
		context.Context,
		SpeechFeedbackClaim,
		SpeechFeedbackAcousticEvidence,
	) error
}
