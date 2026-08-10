package speechfeedback

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestNewIELTSSpeakingAcousticSourceRejectsNilReader(t *testing.T) {
	if _, err := NewIELTSSpeakingAcousticSource(nil); !errors.Is(
		err,
		evaluation.ErrInvalidRequest,
	) {
		t.Fatalf("NewIELTSSpeakingAcousticSource error = %v", err)
	}
}

func TestIELTSSpeakingAcousticSourceAcceptsSentenceAssessment(t *testing.T) {
	accuracy := 74.0
	fluency := 68.0
	reader := &ieltsSpeakingFeedbackReaderStub{
		feedback: SpeechFeedback{
			FeedbackStatus: SpeechFeedbackReady,
			AcousticAssessment: SpeechFeedbackAcousticAssessment{
				Pronunciation:   SpeechFeedbackAssessed,
				AcousticFluency: SpeechFeedbackAssessed,
				AccuracyScore:   &accuracy,
				FluencyScore:    &fluency,
				Provider:        "xfyun_ise",
				ProviderSession: "session-1",
			},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.GetIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{{
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
		feedbackByID: map[string]SpeechFeedback{
			"feedback_ready": {
				FeedbackStatus: SpeechFeedbackReady,
				AcousticAssessment: SpeechFeedbackAcousticAssessment{
					Pronunciation:   SpeechFeedbackAssessed,
					AcousticFluency: SpeechFeedbackAssessed,
					AccuracyScore:   &accuracy,
					FluencyScore:    &fluency,
					Provider:        "xfyun_ise",
					ProviderSession: "session-ready",
				},
			},
			"feedback_failed": {FeedbackStatus: SpeechFeedbackFailed},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.GetIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{
			{TurnID: "turn_ready", EvidenceRefID: "evidence_ready"},
			{TurnID: "turn_failed", EvidenceRefID: "evidence_failed"},
		},
	)
	if err != nil || len(values) != 1 || values[0].TurnID != "turn_ready" {
		t.Fatalf("acoustics = %#v; err = %v", values, err)
	}
}

func TestIELTSSpeakingAcousticSourceWaitsForPendingEvidence(
	t *testing.T,
) {
	reader := &ieltsSpeakingFeedbackReaderStub{
		referenceByTurn: map[string]string{
			"turn_pending": "feedback_pending",
		},
		feedbackByID: map[string]SpeechFeedback{
			"feedback_pending": {
				FeedbackStatus: SpeechFeedbackRunning,
			},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.GetIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{
			{TurnID: "turn_text", EvidenceRefID: "evidence_text"},
			{
				TurnID:              "turn_pending",
				EvidenceRefID:       "evidence_pending",
				RecordingDurationMS: 2000,
			},
		},
	)
	if !errors.Is(err, scoring.ErrIELTSSpeakingAcousticsPending) ||
		len(values) != 0 {
		t.Fatalf("acoustics = %#v; err = %v", values, err)
	}
}

func TestIELTSSpeakingAcousticSourceUsesSufficientReadyEvidenceWhilePending(
	t *testing.T,
) {
	accuracy := 82.0
	fluency := 79.0
	reader := &ieltsSpeakingFeedbackReaderStub{
		referenceByTurn: map[string]string{
			"turn_ready":   "feedback_ready",
			"turn_pending": "feedback_pending",
		},
		feedbackByID: map[string]SpeechFeedback{
			"feedback_ready": {
				FeedbackStatus: SpeechFeedbackReady,
				AcousticAssessment: SpeechFeedbackAcousticAssessment{
					Pronunciation:   SpeechFeedbackAssessed,
					AcousticFluency: SpeechFeedbackAssessed,
					AccuracyScore:   &accuracy,
					FluencyScore:    &fluency,
					Provider:        "xfyun_ise",
					ProviderSession: "session-ready",
				},
			},
			"feedback_pending": {FeedbackStatus: SpeechFeedbackRunning},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.GetIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{
			{
				TurnID:              "turn_ready",
				EvidenceRefID:       "evidence_ready",
				RecordingDurationMS: 3000,
			},
			{
				TurnID:              "turn_pending",
				EvidenceRefID:       "evidence_pending",
				RecordingDurationMS: 2000,
			},
		},
	)
	if err != nil || len(values) != 1 || values[0].TurnID != "turn_ready" {
		t.Fatalf("acoustics = %#v; err = %v", values, err)
	}
}

func TestIELTSSpeakingAcousticSourceWaitsForMissingRecordedTurn(
	t *testing.T,
) {
	source, err := NewIELTSSpeakingAcousticSource(
		&ieltsSpeakingFeedbackReaderStub{
			referenceByTurn: map[string]string{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	values, err := source.GetIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{{
			TurnID:              "turn_recorded",
			EvidenceRefID:       "evidence_recorded",
			RecordingDurationMS: 2000,
		}},
	)
	if !errors.Is(err, scoring.ErrIELTSSpeakingAcousticsPending) ||
		len(values) != 0 {
		t.Fatalf("acoustics = %#v; err = %v", values, err)
	}
}

type ieltsSpeakingFeedbackReaderStub struct {
	feedback        SpeechFeedback
	referenceByTurn map[string]string
	feedbackByID    map[string]SpeechFeedback
}

func (stub *ieltsSpeakingFeedbackReaderStub) FindSpeechFeedbackByConversationTurn(
	_ context.Context,
	_ string,
	turnID string,
) (SpeechFeedbackReference, bool, error) {
	if id, ok := stub.referenceByTurn[turnID]; ok {
		return SpeechFeedbackReference{SpeechFeedbackID: id}, true, nil
	}
	if stub.referenceByTurn != nil {
		return SpeechFeedbackReference{}, false, nil
	}
	return SpeechFeedbackReference{
		SpeechFeedbackID: "20000000-0000-4000-8000-000000000001",
	}, true, nil
}

func (stub *ieltsSpeakingFeedbackReaderStub) GetSpeechFeedback(
	_ context.Context,
	_ string,
	feedbackID string,
) (SpeechFeedback, error) {
	if feedback, ok := stub.feedbackByID[feedbackID]; ok {
		return feedback, nil
	}
	return stub.feedback, nil
}
