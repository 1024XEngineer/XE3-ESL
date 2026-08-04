package xfyun

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

const SpeechFeedbackProviderName = "xfyun-ise"

// SpeechFeedbackEvaluator adapts the iFlytek ISE protocol to Evaluation's
// provider-neutral acoustic assessment port.
type SpeechFeedbackEvaluator struct {
	evaluator *Evaluator
}

func NewSpeechFeedbackEvaluator(
	configuration ISEConfig,
	appID string,
	apiKey string,
	apiSecret string,
) (*SpeechFeedbackEvaluator, error) {
	evaluator, err := NewEvaluator(
		configuration,
		appID,
		apiKey,
		apiSecret,
	)
	if err != nil {
		return nil, err
	}
	return &SpeechFeedbackEvaluator{evaluator: evaluator}, nil
}

func (provider *SpeechFeedbackEvaluator) Evaluate(
	ctx context.Context,
	request speechfeedback.AcousticAssessmentRequest,
) (speechfeedback.AcousticAssessmentResult, error) {
	if provider == nil || provider.evaluator == nil {
		return speechfeedback.AcousticAssessmentResult{}, errors.New(
			"iFlytek ISE evaluator is required",
		)
	}
	category, err := evaluationCategory(request.Category)
	if err != nil {
		return speechfeedback.AcousticAssessmentResult{}, err
	}
	result, err := provider.evaluator.Evaluate(ctx, EvaluationRequest{
		Audio:         request.Audio,
		ReferenceText: request.ReferenceText,
		TopicTitle:    request.TopicTitle,
		Category:      category,
	})
	if err != nil {
		return speechfeedback.AcousticAssessmentResult{}, err
	}
	fields := make(
		[]speechfeedback.AcousticAssessmentField,
		len(result.AvailableFields),
	)
	for index, field := range result.AvailableFields {
		fields[index] = speechfeedback.AcousticAssessmentField{
			Path:  field.Path,
			Name:  field.Name,
			Value: field.Value,
		}
	}
	return speechfeedback.AcousticAssessmentResult{
		Provider:        SpeechFeedbackProviderName,
		SessionID:       result.SessionID,
		RawResult:       result.RawXML,
		AvailableFields: fields,
		Summary: speechfeedback.AcousticAssessmentSummary{
			AccuracyScore:  result.Summary.AccuracyScore,
			FluencyScore:   result.Summary.FluencyScore,
			IntegrityScore: result.Summary.IntegrityScore,
			PhoneScore:     result.Summary.PhoneScore,
			SpeakingSpeed:  result.Summary.SpeakingSpeed,
			Rejected:       result.Summary.Rejected,
			ExceptionInfo:  result.Summary.ExceptionInfo,
		},
	}, nil
}

func evaluationCategory(
	category speechfeedback.AcousticAssessmentCategory,
) (EvaluationCategory, error) {
	switch category {
	case speechfeedback.AcousticCategoryReadWord:
		return CategoryReadWord, nil
	case speechfeedback.AcousticCategoryReadSentence:
		return CategoryReadSentence, nil
	case speechfeedback.AcousticCategoryTopic:
		return CategoryTopic, nil
	default:
		return "", errors.New("iFlytek ISE category is unsupported")
	}
}

var _ speechfeedback.AcousticEvaluator = (*SpeechFeedbackEvaluator)(nil)
