package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

type ieltsSpeakingFeedbackReader interface {
	FindSpeechFeedbackByConversationTurn(
		context.Context,
		string,
		string,
	) (review.SpeechFeedbackReference, bool, error)
	GetSpeechFeedback(
		context.Context,
		string,
		string,
	) (review.SpeechFeedback, error)
}

type ieltsSpeakingAcousticSource struct {
	feedback ieltsSpeakingFeedbackReader
}

func (source *ieltsSpeakingAcousticSource) GetIELTSSpeakingAcoustics(
	ctx context.Context,
	ownerUserID string,
	requests []evaluation.IELTSSpeakingAcousticRequest,
) ([]evaluation.IELTSSpeakingTurnAcoustics, error) {
	if source == nil || source.feedback == nil || ctx == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	result := make(
		[]evaluation.IELTSSpeakingTurnAcoustics,
		0,
		len(requests),
	)
	for _, request := range requests {
		reference, found, err :=
			source.feedback.FindSpeechFeedbackByConversationTurn(
				ctx,
				ownerUserID,
				request.TurnID,
			)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, evaluation.ErrIELTSSpeakingAcousticsPending
		}
		feedback, err := source.feedback.GetSpeechFeedback(
			ctx,
			ownerUserID,
			reference.SpeechFeedbackID,
		)
		if err != nil {
			return nil, err
		}
		switch feedback.FeedbackStatus {
		case review.SpeechFeedbackQueued, review.SpeechFeedbackRunning:
			return nil, evaluation.ErrIELTSSpeakingAcousticsPending
		case review.SpeechFeedbackFailed:
			continue
		case review.SpeechFeedbackReady:
		default:
			return nil, errors.New("bootstrap: unsupported speech feedback status")
		}
		assessment := feedback.AcousticAssessment
		if assessment.Pronunciation != review.SpeechFeedbackAssessed ||
			assessment.AcousticFluency != review.SpeechFeedbackAssessed {
			continue
		}
		pronunciation := assessment.PronunciationScore
		if pronunciation == nil {
			pronunciation = assessment.AccuracyScore
		}
		if pronunciation == nil ||
			(assessment.FluencyScore == nil &&
				assessment.SpeakingSpeedWPM == nil) {
			continue
		}
		result = append(result, evaluation.IELTSSpeakingTurnAcoustics{
			TurnID:               request.TurnID,
			EvidenceRefID:        request.EvidenceRefID,
			PronunciationScore:   *pronunciation,
			AcousticFluencyScore: assessment.FluencyScore,
			SpeakingSpeedWPM:     assessment.SpeakingSpeedWPM,
			Provider:             assessment.Provider,
			ProviderRun:          acousticProviderRun(assessment.ProviderSession),
		})
	}
	return result, nil
}

func acousticProviderRun(providerSession string) string {
	digest := sha256.Sum256([]byte(providerSession))
	return "run_" + hex.EncodeToString(digest[:12])
}

var _ evaluation.IELTSSpeakingAcousticSource = (*ieltsSpeakingAcousticSource)(nil)
