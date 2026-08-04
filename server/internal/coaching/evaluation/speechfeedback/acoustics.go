package speechfeedback

import "context"

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
