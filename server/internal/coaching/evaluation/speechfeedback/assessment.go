package speechfeedback

import (
	"context"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

type CompactAcousticEvaluator struct {
	audio     AudioReader
	evaluator AcousticEvaluator
}

func NewCompactAcousticEvaluator(
	audio AudioReader,
	evaluator AcousticEvaluator,
) (*CompactAcousticEvaluator, error) {
	if audio == nil || evaluator == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &CompactAcousticEvaluator{audio: audio, evaluator: evaluator}, nil
}

func (evaluator *CompactAcousticEvaluator) EvaluateAcoustic(
	ctx context.Context,
	record evaluation.Record,
	snapshot evaluation.SpeechInputSnapshot,
) (evaluation.AcousticCheckpoint, error) {
	if evaluator == nil || evaluator.audio == nil || evaluator.evaluator == nil ||
		ctx == nil || record.Kind != evaluation.KindPracticeTurnFeedback ||
		snapshot.AudioAssetID == "" || snapshot.Acoustic != nil ||
		!speechFeedbackHasAssessableEnglish(snapshot.Transcript) {
		return evaluation.AcousticCheckpoint{}, ErrAcousticUnavailable
	}
	audio, err := evaluator.audio.ReadOwnedAudio(
		ctx, record.UserID, snapshot.AudioAssetID,
	)
	if err != nil {
		return evaluation.AcousticCheckpoint{}, err
	}
	pcm, err := pcm16Mono(audio)
	if err != nil {
		return evaluation.AcousticCheckpoint{}, err
	}
	reference := speechFeedbackEnglishReferenceText(snapshot.Transcript)
	category := speechFeedbackAcousticCategory(reference)
	result, err := evaluator.evaluator.Evaluate(ctx, AcousticAssessmentRequest{
		Audio:         pcm,
		ReferenceText: reference,
		Category:      category,
	})
	if err != nil {
		return evaluation.AcousticCheckpoint{}, err
	}
	if result.Summary.Rejected == nil || *result.Summary.Rejected ||
		!validScore(result.Summary.AccuracyScore) ||
		!validSpeechFeedbackIdentifier(strings.TrimSpace(result.Provider)) {
		return evaluation.AcousticCheckpoint{}, ErrAcousticUnavailable
	}
	if category == AcousticCategoryReadSentence &&
		(!validScore(result.Summary.FluencyScore) ||
			!validScore(result.Summary.IntegrityScore)) {
		return evaluation.AcousticCheckpoint{}, ErrAcousticUnavailable
	}
	checkpoint := evaluation.AcousticCheckpoint{
		Status:           evaluation.AcousticAssessed,
		Pronunciation:    result.Summary.AccuracyScore,
		Fluency:          result.Summary.FluencyScore,
		Integrity:        result.Summary.IntegrityScore,
		SpeakingSpeedWPM: result.Summary.SpeakingSpeed,
		Provider:         strings.TrimSpace(result.Provider),
		ProviderSession:  strings.TrimSpace(result.SessionID),
	}
	if !checkpoint.Valid() {
		return evaluation.AcousticCheckpoint{}, ErrAcousticUnavailable
	}
	return checkpoint, nil
}

func validScore(value *float64) bool {
	return value != nil && *value >= 0 && *value <= 100
}

var _ evaluation.AcousticEvaluator = (*CompactAcousticEvaluator)(nil)
