package review

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
)

func TestXFYUNSpeechFeedbackAcousticProviderUsesConfirmedTextAndAudio(
	t *testing.T,
) {
	t.Parallel()
	accuracy, fluency, integrity := 81.5, 92.25, 100.0
	rejected := false
	audio := &speechFeedbackAudioReaderStub{audio: []byte{1, 2, 3}}
	evaluator := &speechFeedbackISEEvaluatorStub{
		result: xfyun.EvaluationResult{
			SessionID: "ise-session-1",
			RawXML:    "<xml_result/>",
			AvailableFields: []xfyun.ResultField{{
				Path:  "/read_word",
				Name:  "accuracy_score",
				Value: "81.5",
			}},
			Summary: xfyun.ScoreSummary{
				AccuracyScore:  &accuracy,
				FluencyScore:   &fluency,
				IntegrityScore: &integrity,
				Rejected:       &rejected,
			},
		},
	}
	provider, err := NewXFYUNSpeechFeedbackAcousticProvider(
		audio,
		evaluator,
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := provider.EvaluateSpeechFeedbackAcoustics(
		context.Background(),
		SpeechFeedbackAcousticInput{
			OwnerUserID:       "f475b521-a96f-44be-b447-8b85bed7e6e9",
			AudioAssetID:      "audio_asset_1",
			AudioAssetVersion: 2,
			AudioChecksum:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ConfirmedText:     "Hello",
		},
	)
	if err != nil {
		t.Fatalf("evaluate acoustics: %v", err)
	}
	if audio.calls != 1 ||
		evaluator.request.ReferenceText != "Hello" ||
		evaluator.request.Category != xfyun.CategoryReadWord ||
		string(evaluator.request.Audio) != string(audio.audio) ||
		evidence.Assessment.AccuracyScore == nil ||
		*evidence.Assessment.AccuracyScore != accuracy ||
		evidence.Assessment.Provider !=
			SpeechFeedbackAcousticProviderName {
		t.Fatalf(
			"unexpected evidence/request: %#v / %#v",
			evidence,
			evaluator.request,
		)
	}
}

func TestXFYUNSpeechFeedbackAcousticProviderRejectsChineseBeforeReadingAudio(
	t *testing.T,
) {
	t.Parallel()
	audio := &speechFeedbackAudioReaderStub{}
	provider, err := NewXFYUNSpeechFeedbackAcousticProvider(
		audio,
		&speechFeedbackISEEvaluatorStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.EvaluateSpeechFeedbackAcoustics(
		context.Background(),
		SpeechFeedbackAcousticInput{
			OwnerUserID:       "f475b521-a96f-44be-b447-8b85bed7e6e9",
			AudioAssetID:      "audio_asset_1",
			AudioAssetVersion: 1,
			AudioChecksum:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ConfirmedText:     "你好",
		},
	)
	if !errors.Is(err, ErrSpeechFeedbackAcousticUnavailable) ||
		audio.calls != 0 {
		t.Fatalf("error/calls = %v / %d", err, audio.calls)
	}
}

func TestSpeechFeedbackWorkerPersistsAcousticsBeforeShortTextResult(
	t *testing.T,
) {
	t.Parallel()
	claim := validSpeechFeedbackClaim()
	claim.Source = SpeechFeedbackSource{
		SourceKind:         SpeechFeedbackSourceConversationTurn,
		PracticeSessionID:  "practice_1",
		TurnID:             "turn_1",
		InputRevision:      1,
		EvidenceSnapshotID: "snapshot_1",
	}
	claim.EvidenceRefID = "evidence_1"
	claim.CanonicalText = "Hello"
	claim.AudioAssetID = "audio_asset_1"
	claim.AudioAssetVersion = 1
	claim.AudioChecksum =
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repository := &speechFeedbackRepositoryStub{claim: claim}
	acoustics := &speechFeedbackAcousticProviderStub{
		evidence: validSpeechFeedbackAcousticEvidence(),
	}
	worker, err := NewSpeechFeedbackWorkerWithAcoustics(
		repository,
		&speechFeedbackProviderStub{
			err: errors.New("text provider must not be called"),
		},
		repository,
		acoustics,
		validSpeechFeedbackWorkerConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process short word: %v", err)
	}
	if acoustics.calls != 1 ||
		repository.acousticEvidence == nil ||
		sweep.Completed != 1 ||
		sweep.Insufficient != 1 {
		t.Fatalf(
			"unexpected acoustic result: calls=%d evidence=%#v sweep=%#v",
			acoustics.calls,
			repository.acousticEvidence,
			sweep,
		)
	}
}

type speechFeedbackAudioReaderStub struct {
	audio []byte
	err   error
	calls int
}

func (reader *speechFeedbackAudioReaderStub) ReadSpeechFeedbackAudio(
	context.Context,
	string,
	string,
	string,
) ([]byte, error) {
	reader.calls++
	return reader.audio, reader.err
}

type speechFeedbackISEEvaluatorStub struct {
	request xfyun.EvaluationRequest
	result  xfyun.EvaluationResult
	err     error
}

func (evaluator *speechFeedbackISEEvaluatorStub) Evaluate(
	_ context.Context,
	request xfyun.EvaluationRequest,
) (xfyun.EvaluationResult, error) {
	evaluator.request = request
	return evaluator.result, evaluator.err
}

type speechFeedbackAcousticProviderStub struct {
	evidence SpeechFeedbackAcousticEvidence
	err      error
	calls    int
}

func (provider *speechFeedbackAcousticProviderStub) EvaluateSpeechFeedbackAcoustics(
	context.Context,
	SpeechFeedbackAcousticInput,
) (SpeechFeedbackAcousticEvidence, error) {
	provider.calls++
	return provider.evidence, provider.err
}

func validSpeechFeedbackAcousticEvidence() SpeechFeedbackAcousticEvidence {
	accuracy, fluency, integrity := 80.0, 90.0, 100.0
	return SpeechFeedbackAcousticEvidence{
		Assessment: SpeechFeedbackAcousticAssessment{
			Pronunciation:   SpeechFeedbackAssessed,
			AcousticFluency: SpeechFeedbackAssessed,
			Integrity:       SpeechFeedbackAssessed,
			AccuracyScore:   &accuracy,
			FluencyScore:    &fluency,
			IntegrityScore:  &integrity,
			Provider:        SpeechFeedbackAcousticProviderName,
			ProviderSession: "ise-session-1",
			Category:        "read_word",
			Notice:          SpeechFeedbackAcousticNotice,
		},
		RawResult:       "<xml_result/>",
		AvailableFields: []xfyun.ResultField{},
	}
}
