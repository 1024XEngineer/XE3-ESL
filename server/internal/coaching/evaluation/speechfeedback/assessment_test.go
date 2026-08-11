package speechfeedback

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

const testSpeechFeedbackAcousticProvider = "test-acoustic"

func TestSpeechFeedbackAcousticProviderUsesConfirmedTextAndAudio(
	t *testing.T,
) {
	t.Parallel()
	accuracy, fluency, integrity := 81.5, 92.25, 100.0
	rejected := false
	pcm := []byte{1, 2, 3, 4}
	audio := &speechFeedbackAudioReaderStub{
		audio: speechFeedbackTestWAV(pcm),
	}
	evaluator := &acousticEvaluatorStub{
		result: AcousticAssessmentResult{
			Provider:  testSpeechFeedbackAcousticProvider,
			SessionID: "wse00000001@ll36940e324c59000100",
			RawResult: "<xml_result/>",
			AvailableFields: []AcousticAssessmentField{{
				Path:  "/read_word",
				Name:  "accuracy_score",
				Value: "81.5",
			}},
			Summary: AcousticAssessmentSummary{
				AccuracyScore:  &accuracy,
				FluencyScore:   &fluency,
				IntegrityScore: &integrity,
				Rejected:       &rejected,
			},
		},
	}
	provider, err := NewSpeechFeedbackAcousticProvider(
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
		evaluator.request.Category != AcousticCategoryReadWord ||
		string(evaluator.request.Audio) != string(pcm) ||
		evidence.Assessment.AccuracyScore == nil ||
		*evidence.Assessment.AccuracyScore != accuracy ||
		evidence.Assessment.Provider !=
			testSpeechFeedbackAcousticProvider {
		t.Fatalf(
			"unexpected evidence/request: %#v / %#v",
			evidence,
			evaluator.request,
		)
	}
}

func TestSpeechFeedbackPCM16MonoRejectsMismatchedFormat(t *testing.T) {
	t.Parallel()
	wav := speechFeedbackTestWAV([]byte{1, 2, 3, 4})
	binary.LittleEndian.PutUint32(wav[24:28], 48_000)
	if _, err := speechFeedbackPCM16Mono(wav); !errors.Is(
		err,
		ErrSpeechFeedbackAcousticUnavailable,
	) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSpeechFeedbackAcousticSummaryExplainsUnavailableResult(
	t *testing.T,
) {
	t.Parallel()
	rejected := true
	err := validateSpeechFeedbackAcousticSummary(
		AcousticAssessmentSummary{
			Rejected:      &rejected,
			ExceptionInfo: "28676",
		},
		AcousticCategoryReadSentence,
	)
	if !errors.Is(err, ErrSpeechFeedbackAcousticUnavailable) ||
		!strings.Contains(err.Error(), "except_info=28676") {
		t.Fatalf("rejected error = %v", err)
	}

	rejected = false
	err = validateSpeechFeedbackAcousticSummary(
		AcousticAssessmentSummary{Rejected: &rejected},
		AcousticCategoryReadSentence,
	)
	if !errors.Is(err, ErrSpeechFeedbackAcousticUnavailable) ||
		!strings.Contains(err.Error(), "full-dimension") {
		t.Fatalf("missing fields error = %v", err)
	}
}

func TestSpeechFeedbackAcousticProviderUsesReadSentenceForPracticePrompt(
	t *testing.T,
) {
	t.Parallel()
	accuracy, fluency, integrity := 88.5, 92.0, 100.0
	rejected := false
	evaluator := &acousticEvaluatorStub{
		result: AcousticAssessmentResult{
			Provider:  testSpeechFeedbackAcousticProvider,
			SessionID: "wse00000001@ll36940e324c59000100",
			RawResult: "<xml_result/>",
			Summary: AcousticAssessmentSummary{
				AccuracyScore:  &accuracy,
				FluencyScore:   &fluency,
				IntegrityScore: &integrity,
				Rejected:       &rejected,
			},
		},
	}
	provider, err := NewSpeechFeedbackAcousticProvider(
		&speechFeedbackAudioReaderStub{
			audio: speechFeedbackTestWAV([]byte{1, 2, 3, 4}),
		},
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
			AudioAssetVersion: 1,
			AudioChecksum:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ConfirmedText:     "I use AI to summarize customer feedback.",
			PromptText:        "How do you use artificial intelligence at work?",
		},
	)
	if err != nil {
		t.Fatalf("evaluate practice acoustics: %v", err)
	}
	if evaluator.request.Category != AcousticCategoryReadSentence ||
		evaluator.request.TopicTitle != "" ||
		evaluator.request.ReferenceText !=
			"I use AI to summarize customer feedback." ||
		evidence.Assessment.Category != "read_sentence" ||
		evidence.Assessment.AccuracyScore == nil ||
		*evidence.Assessment.AccuracyScore != accuracy ||
		evidence.Assessment.FluencyScore == nil ||
		*evidence.Assessment.FluencyScore != fluency ||
		evidence.Assessment.IntegrityScore == nil ||
		*evidence.Assessment.IntegrityScore != integrity {
		t.Fatalf(
			"unexpected practice evidence/request: %#v / %#v",
			evidence,
			evaluator.request,
		)
	}
}

func TestSpeechFeedbackAcousticProviderRejectsChineseBeforeReadingAudio(
	t *testing.T,
) {
	t.Parallel()
	audio := &speechFeedbackAudioReaderStub{}
	provider, err := NewSpeechFeedbackAcousticProvider(
		audio,
		&acousticEvaluatorStub{},
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

func TestSpeechFeedbackAcousticProviderAssessesEnglishInMixedSpeech(
	t *testing.T,
) {
	t.Parallel()
	accuracy, fluency, integrity := 78.0, 75.0, 80.0
	rejected := false
	evaluator := &acousticEvaluatorStub{
		result: AcousticAssessmentResult{
			Provider:  testSpeechFeedbackAcousticProvider,
			SessionID: "mixed-session",
			RawResult: "<result/>",
			Summary: AcousticAssessmentSummary{
				AccuracyScore:  &accuracy,
				FluencyScore:   &fluency,
				IntegrityScore: &integrity,
				Rejected:       &rejected,
			},
		},
	}
	provider, err := NewSpeechFeedbackAcousticProvider(
		&speechFeedbackAudioReaderStub{
			audio: speechFeedbackTestWAV([]byte{1, 2, 3, 4}),
		},
		evaluator,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.EvaluateSpeechFeedbackAcoustics(
		context.Background(),
		SpeechFeedbackAcousticInput{
			OwnerUserID:       "f475b521-a96f-44be-b447-8b85bed7e6e9",
			AudioAssetID:      "audio_asset_mixed",
			AudioAssetVersion: 1,
			AudioChecksum:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ConfirmedText:     "这是补充。 I like AI, 因为 it helps me.",
		},
	)
	if err != nil {
		t.Fatalf("evaluate mixed acoustics: %v", err)
	}
	if evaluator.request.ReferenceText != "I like AI, it helps me" ||
		evaluator.request.Category != AcousticCategoryReadSentence {
		t.Fatalf("mixed ISE request = %#v", evaluator.request)
	}
}

func TestSpeechFeedbackWorkerPersistsAcousticsForShortEnglishText(
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
	claim.PromptText = "What would you like to practice today?"
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
			result: SpeechFeedbackProviderResult{
				Payload:   []byte(`{"items":[{"kind":"RECOMMENDED_EXPRESSION","explanation":"Use a complete answer.","suggested_text":"Hello, it is nice to meet you."}]}`),
				Provider:  "qianwen",
				Model:     "qwen-plus",
				RequestID: "request-short-english-acoustics",
			},
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
		sweep.Insufficient != 0 {
		t.Fatalf(
			"unexpected acoustic result: calls=%d evidence=%#v sweep=%#v",
			acoustics.calls,
			repository.acousticEvidence,
			sweep,
		)
	}
	assertSpeechFeedbackPersistenceContexts(
		t,
		repository.persistenceContexts,
		claim.LeaseExpiresAt,
		2,
	)
}

func TestAgentVoiceClaimUsesItsReadableMessageAudio(t *testing.T) {
	t.Parallel()
	claim := validSpeechFeedbackClaim()
	claim.AudioAssetID = "4a579544-0a28-4f8d-84ea-fac3202ce3a3"
	claim.AudioAssetVersion = claim.Source.CandidateVersion
	claim.AudioChecksum =
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	claim.AudioObjectKey =
		"audio/v1/agent/4a579544-0a28-4f8d-84ea-fac3202ce3a3.wav"

	if !claim.hasAcousticSource() {
		t.Fatal("Agent voice claim should expose readable acoustic evidence")
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
	string,
) ([]byte, error) {
	reader.calls++
	return reader.audio, reader.err
}

type acousticEvaluatorStub struct {
	request AcousticAssessmentRequest
	result  AcousticAssessmentResult
	err     error
}

func (evaluator *acousticEvaluatorStub) Evaluate(
	_ context.Context,
	request AcousticAssessmentRequest,
) (AcousticAssessmentResult, error) {
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
			Provider:        testSpeechFeedbackAcousticProvider,
			ProviderSession: "ise-session-1",
			Category:        "read_word",
			Notice:          SpeechFeedbackAcousticNotice,
		},
		RawResult:       "<xml_result/>",
		AvailableFields: []AcousticAssessmentField{},
	}
}

func speechFeedbackTestWAV(pcm []byte) []byte {
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 16_000)
	binary.LittleEndian.PutUint32(wav[28:32], 32_000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav
}
