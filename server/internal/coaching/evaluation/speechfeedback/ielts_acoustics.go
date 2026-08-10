package speechfeedback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

type IELTSSpeakingFeedbackReader interface {
	FindSpeechFeedbackByConversationTurn(
		context.Context,
		string,
		string,
	) (SpeechFeedbackReference, bool, error)
	GetSpeechFeedback(
		context.Context,
		string,
		string,
	) (SpeechFeedback, error)
}

type ieltsSpeakingAcousticSource struct {
	feedback IELTSSpeakingFeedbackReader
}

func NewIELTSSpeakingAcousticSource(
	feedback IELTSSpeakingFeedbackReader,
) (scoring.IELTSSpeakingAcousticSource, error) {
	if feedback == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &ieltsSpeakingAcousticSource{feedback: feedback}, nil
}

func (source *ieltsSpeakingAcousticSource) GetIELTSSpeakingAcoustics(
	ctx context.Context,
	ownerUserID string,
	requests []scoring.IELTSSpeakingAcousticRequest,
) ([]scoring.IELTSSpeakingTurnAcoustics, error) {
	if source == nil || source.feedback == nil || ctx == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	result := make(
		[]scoring.IELTSSpeakingTurnAcoustics,
		0,
		len(requests),
	)
	pending := false
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
			// A confirmed recording normally creates SpeechFeedback in the same
			// turn flow. Treat a missing projection as a short-lived race; text-only
			// turns remain valid evidence and do not wait for an impossible row.
			if request.RecordingDurationMS > 0 {
				pending = true
			}
			continue
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
		case SpeechFeedbackQueued, SpeechFeedbackRunning:
			pending = true
			continue
		case SpeechFeedbackFailed:
			continue
		case SpeechFeedbackReady:
		default:
			return nil, errors.New(
				"evaluation speech feedback: unsupported feedback status",
			)
		}
		assessment := feedback.AcousticAssessment
		if assessment.Pronunciation != SpeechFeedbackAssessed ||
			assessment.AcousticFluency != SpeechFeedbackAssessed {
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
		result = append(result, scoring.IELTSSpeakingTurnAcoustics{
			TurnID:               request.TurnID,
			EvidenceRefID:        request.EvidenceRefID,
			PronunciationScore:   *pronunciation,
			AcousticFluencyScore: assessment.FluencyScore,
			SpeakingSpeedWPM:     assessment.SpeakingSpeedWPM,
			Provider:             assessment.Provider,
			ProviderRun:          acousticProviderRun(assessment.ProviderSession),
		})
	}
	if pending {
		return nil, scoring.ErrIELTSSpeakingAcousticsPending
	}
	return result, nil
}

func acousticProviderRun(providerSession string) string {
	digest := sha256.Sum256([]byte(providerSession))
	return "run_" + hex.EncodeToString(digest[:12])
}

var _ scoring.IELTSSpeakingAcousticSource = (*ieltsSpeakingAcousticSource)(nil)
