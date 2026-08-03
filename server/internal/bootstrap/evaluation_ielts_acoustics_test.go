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

type ieltsSpeakingFeedbackReaderStub struct {
	feedback review.SpeechFeedback
}

func (stub *ieltsSpeakingFeedbackReaderStub) FindSpeechFeedbackByConversationTurn(
	context.Context,
	string,
	string,
) (review.SpeechFeedbackReference, bool, error) {
	return review.SpeechFeedbackReference{
		SpeechFeedbackID: "20000000-0000-4000-8000-000000000001",
	}, true, nil
}

func (stub *ieltsSpeakingFeedbackReaderStub) GetSpeechFeedback(
	context.Context,
	string,
	string,
) (review.SpeechFeedback, error) {
	return stub.feedback, nil
}
