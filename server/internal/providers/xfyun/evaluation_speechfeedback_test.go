package xfyun

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

func TestSpeechFeedbackEvaluatorMapsEvaluationPortToISEProtocol(
	t *testing.T,
) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      0,
			Message:   "success",
			SessionID: "ise-session",
			Data: &responseData{
				Status: 2,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(testResultXML),
				),
			},
		}},
	}
	client := newTestEvaluator(t, connection)
	client.frameBytes = 2
	client.frameInterval = 0
	provider := &SpeechFeedbackEvaluator{evaluator: client}

	result, err := provider.Evaluate(
		context.Background(),
		speechfeedback.AcousticAssessmentRequest{
			Audio:         []byte{1, 2, 3},
			ReferenceText: "Hello.",
			Category:      speechfeedback.AcousticCategoryReadSentence,
		},
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Provider != SpeechFeedbackProviderName ||
		result.SessionID != "ise-session" ||
		result.RawResult != testResultXML ||
		result.Summary.AccuracyScore == nil ||
		*result.Summary.AccuracyScore != 86 ||
		len(result.AvailableFields) == 0 {
		t.Fatalf("Evaluation result = %#v", result)
	}
	initial := decodeObject(t, connection.writes[0])
	business := objectValue(t, initial, "business")
	if business["category"] != "read_sentence" {
		t.Fatalf("ISE business payload = %#v", business)
	}
}

func TestSpeechFeedbackEvaluatorRejectsUnsupportedEvaluationCategory(
	t *testing.T,
) {
	provider := &SpeechFeedbackEvaluator{evaluator: &Evaluator{}}
	if _, err := provider.Evaluate(
		context.Background(),
		speechfeedback.AcousticAssessmentRequest{
			Category: "unsupported",
		},
	); err == nil {
		t.Fatal("unsupported category was accepted")
	}
}
