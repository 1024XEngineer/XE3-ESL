package bootstrap

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
)

func TestIELTSSpeakingAcousticSourceAcceptsSentenceAssessment(t *testing.T) {
	accuracy := 74.0
	fluency := 68.0
	reader := &ieltsSpeakingFeedbackReaderStub{
		feedback: review.SpeechFeedback{
			FeedbackStatus: review.SpeechFeedbackReady,
			AcousticAssessment: review.SpeechFeedbackAcousticAssessment{
				Pronunciation:   review.SpeechFeedbackAssessed,
				AcousticFluency: review.SpeechFeedbackAssessed,
				AccuracyScore:   &accuracy,
				FluencyScore:    &fluency,
				Provider:        "xfyun_ise",
				ProviderSession: "session-1",
			},
		},
	}
	values, err := (&ieltsSpeakingAcousticSource{feedback: reader}).
		GetIELTSSpeakingAcoustics(
			context.Background(),
			"10000000-0000-4000-8000-000000000001",
			[]evaluation.IELTSSpeakingAcousticRequest{{
				TurnID:        "turn_1",
				EvidenceRefID: "evidence_1",
			}},
		)
	if err != nil {
		t.Fatalf("GetIELTSSpeakingAcoustics: %v", err)
	}
	if len(values) != 1 || values[0].PronunciationScore != accuracy ||
		values[0].AcousticFluencyScore == nil ||
		*values[0].AcousticFluencyScore != fluency ||
		values[0].SpeakingSpeedWPM != nil {
		t.Fatalf("acoustics = %#v", values)
	}
}

func TestIELTSSpeakingAcousticSourceKeepsValidPartialEvidence(t *testing.T) {
	accuracy := 82.0
	fluency := 79.0
	reader := &ieltsSpeakingFeedbackReaderStub{
		referenceByTurn: map[string]string{
			"turn_ready":  "feedback_ready",
			"turn_failed": "feedback_failed",
		},
		feedbackByID: map[string]review.SpeechFeedback{
			"feedback_ready": {
				FeedbackStatus: review.SpeechFeedbackReady,
				AcousticAssessment: review.SpeechFeedbackAcousticAssessment{
					Pronunciation:   review.SpeechFeedbackAssessed,
					AcousticFluency: review.SpeechFeedbackAssessed,
					AccuracyScore:   &accuracy,
					FluencyScore:    &fluency,
					Provider:        "xfyun_ise",
					ProviderSession: "session-ready",
				},
			},
			"feedback_failed": {FeedbackStatus: review.SpeechFeedbackFailed},
		},
	}
	values, err := (&ieltsSpeakingAcousticSource{feedback: reader}).
		GetIELTSSpeakingAcoustics(
			context.Background(),
			"10000000-0000-4000-8000-000000000001",
			[]evaluation.IELTSSpeakingAcousticRequest{
				{TurnID: "turn_ready", EvidenceRefID: "evidence_ready"},
				{TurnID: "turn_failed", EvidenceRefID: "evidence_failed"},
			},
		)
	if err != nil || len(values) != 1 || values[0].TurnID != "turn_ready" {
		t.Fatalf("acoustics = %#v; err = %v", values, err)
	}
}

type ieltsSpeakingFeedbackReaderStub struct {
	feedback        review.SpeechFeedback
	referenceByTurn map[string]string
	feedbackByID    map[string]review.SpeechFeedback
}

func (stub *ieltsSpeakingFeedbackReaderStub) FindSpeechFeedbackByConversationTurn(
	_ context.Context,
	_ string,
	turnID string,
) (review.SpeechFeedbackReference, bool, error) {
	if id, ok := stub.referenceByTurn[turnID]; ok {
		return review.SpeechFeedbackReference{SpeechFeedbackID: id}, true, nil
	}
	return review.SpeechFeedbackReference{
		SpeechFeedbackID: "20000000-0000-4000-8000-000000000001",
	}, true, nil
}

func (stub *ieltsSpeakingFeedbackReaderStub) GetSpeechFeedback(
	_ context.Context,
	_ string,
	feedbackID string,
) (review.SpeechFeedback, error) {
	if feedback, ok := stub.feedbackByID[feedbackID]; ok {
		return feedback, nil
	}
	return stub.feedback, nil
}
