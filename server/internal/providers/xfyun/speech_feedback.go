package xfyun

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
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
	request evaluation.AcousticAssessmentRequest,
) (evaluation.AcousticAssessmentResult, error) {
	if provider == nil || provider.evaluator == nil {
		return evaluation.AcousticAssessmentResult{}, errors.New(
			"iFlytek ISE evaluator is required",
		)
	}
	category, err := evaluationCategory(request.Category)
	if err != nil {
		return evaluation.AcousticAssessmentResult{}, err
	}
	result, err := provider.evaluator.Evaluate(ctx, EvaluationRequest{
		Audio:         request.Audio,
		ReferenceText: request.ReferenceText,
		TopicTitle:    request.TopicTitle,
		Category:      category,
	})
	if err != nil {
		return evaluation.AcousticAssessmentResult{}, err
	}
	fields := make(
		[]evaluation.AcousticAssessmentField,
		len(result.AvailableFields),
	)
	for index, field := range result.AvailableFields {
		fields[index] = evaluation.AcousticAssessmentField{
			Path:  field.Path,
			Name:  field.Name,
			Value: field.Value,
		}
	}
	return evaluation.AcousticAssessmentResult{
		Provider:        SpeechFeedbackProviderName,
		SessionID:       result.SessionID,
		RawResult:       result.RawXML,
		AvailableFields: fields,
		Summary: evaluation.AcousticAssessmentSummary{
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
	category evaluation.AcousticAssessmentCategory,
) (EvaluationCategory, error) {
	switch category {
	case evaluation.AcousticCategoryReadWord:
		return CategoryReadWord, nil
	case evaluation.AcousticCategoryReadSentence:
		return CategoryReadSentence, nil
	case evaluation.AcousticCategoryTopic:
		return CategoryTopic, nil
	default:
		return "", errors.New("iFlytek ISE category is unsupported")
	}
}

var _ evaluation.AcousticEvaluator = (*SpeechFeedbackEvaluator)(nil)
