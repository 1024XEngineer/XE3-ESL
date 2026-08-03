package review

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSpeechFeedbackWorkerGeneratesFeedbackForShortEnglishText(
	t *testing.T,
) {
	t.Parallel()
	claim := validSpeechFeedbackClaim()
	claim.CanonicalText = "Hello."
	repository := &speechFeedbackRepositoryStub{
		claim: claim,
	}
	payload, err := json.Marshal(map[string]any{
		"items": []any{map[string]any{
			"kind":           "RECOMMENDED_EXPRESSION",
			"explanation":    "Use a complete answer for speaking practice.",
			"suggested_text": "Hello, it is nice to meet you.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &speechFeedbackProviderStub{
		result: SpeechFeedbackProviderResult{
			Payload:   payload,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-short-english",
		},
	}
	worker, err := NewSpeechFeedbackWorker(
		repository,
		provider,
		validSpeechFeedbackWorkerConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process short English text: %v", err)
	}
	if provider.calls != 1 ||
		sweep.Claimed != 1 ||
		sweep.Completed != 1 ||
		sweep.Insufficient != 0 ||
		len(repository.completedItems) != 1 {
		t.Fatalf("unexpected sweep/provider: %#v calls=%d", sweep, provider.calls)
	}
	if len(repository.insufficientReasons) != 0 {
		t.Fatalf("insufficient reasons = %#v", repository.insufficientReasons)
	}
}

func TestSpeechFeedbackWorkerKeepsChineseTextInsufficient(t *testing.T) {
	t.Parallel()
	claim := validSpeechFeedbackClaim()
	claim.CanonicalText = "你好。"
	repository := &speechFeedbackRepositoryStub{claim: claim}
	provider := &speechFeedbackProviderStub{
		err: errors.New("provider must not be called"),
	}
	worker, err := NewSpeechFeedbackWorker(
		repository,
		provider,
		validSpeechFeedbackWorkerConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process Chinese text: %v", err)
	}
	if provider.calls != 0 || sweep.Insufficient != 1 ||
		len(repository.insufficientReasons) != 1 {
		t.Fatalf(
			"unexpected Chinese result: %#v calls=%d reasons=%#v",
			sweep,
			provider.calls,
			repository.insufficientReasons,
		)
	}
}

func TestSpeechFeedbackWorkerPersistsProviderFailureNotInsufficient(
	t *testing.T,
) {
	t.Parallel()
	repository := &speechFeedbackRepositoryStub{
		claim: validSpeechFeedbackClaim(),
	}
	provider := &speechFeedbackProviderStub{
		err: context.DeadlineExceeded,
	}
	worker, err := NewSpeechFeedbackWorker(
		repository,
		provider,
		validSpeechFeedbackWorkerConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process provider timeout: %v", err)
	}
	if len(repository.insufficientReasons) != 0 ||
		repository.failure == nil ||
		repository.failure.ReasonCode !=
			SpeechFeedbackFailureProcessingTimeout ||
		!repository.failure.Retryable ||
		sweep.Retried != 1 {
		t.Fatalf(
			"failure/sweep = %#v / %#v",
			repository.failure,
			sweep,
		)
	}
}

func TestSpeechFeedbackWorkerCompletesOnlyValidatedProviderItems(
	t *testing.T,
) {
	t.Parallel()
	claim := validSpeechFeedbackClaim()
	suggestion := "I work on this project."
	payload, err := json.Marshal(map[string]any{
		"items": []any{map[string]any{
			"kind":           "CORRECTION",
			"explanation":    "Use a preposition after work.",
			"suggested_text": suggestion,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := &speechFeedbackRepositoryStub{claim: claim}
	provider := &speechFeedbackProviderStub{
		result: SpeechFeedbackProviderResult{
			Payload:   payload,
			Provider:  "qianwen",
			Model:     "qwen-plus",
			RequestID: "request-1",
		},
	}
	worker, err := NewSpeechFeedbackWorker(
		repository,
		provider,
		validSpeechFeedbackWorkerConfiguration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := worker.ProcessPending(context.Background(), 1)
	if err != nil {
		t.Fatalf("process valid provider result: %v", err)
	}
	if sweep.Completed != 1 ||
		len(repository.completedItems) != 1 ||
		repository.completedItems[0].SuggestedText == nil ||
		*repository.completedItems[0].SuggestedText != suggestion ||
		repository.completedItems[0].Anchor.OriginalExcerpt !=
			claim.CanonicalText ||
		repository.completedItems[0].RepracticeMode !=
			SpeechFeedbackRepracticeSameThread {
		t.Fatalf(
			"completion/sweep = %#v / %#v",
			repository.completedItems,
			sweep,
		)
	}
}

type speechFeedbackProviderStub struct {
	result SpeechFeedbackProviderResult
	err    error
	calls  int
}

func (provider *speechFeedbackProviderStub) GenerateSpeechFeedback(
	context.Context,
	SpeechFeedbackProviderInput,
) (SpeechFeedbackProviderResult, error) {
	provider.calls++
	return provider.result, provider.err
}

type speechFeedbackRepositoryStub struct {
	claim               SpeechFeedbackClaim
	claimed             bool
	completedItems      []SpeechFeedbackDraftItem
	insufficientReasons []SpeechFeedbackReasonCode
	failure             *SpeechFeedbackStableFailure
	acousticEvidence    *SpeechFeedbackAcousticEvidence
}

func (repository *speechFeedbackRepositoryStub) SaveSpeechFeedbackAcousticEvidence(
	_ context.Context,
	_ SpeechFeedbackClaim,
	evidence SpeechFeedbackAcousticEvidence,
) error {
	repository.acousticEvidence = &evidence
	return nil
}

func (repository *speechFeedbackRepositoryStub) ClaimSpeechFeedback(
	context.Context,
	SpeechFeedbackWorkerConfiguration,
) (SpeechFeedbackClaim, bool, error) {
	if repository.claimed {
		return SpeechFeedbackClaim{}, false, nil
	}
	repository.claimed = true
	return repository.claim, true, nil
}

func (repository *speechFeedbackRepositoryStub) CompleteSpeechFeedback(
	_ context.Context,
	_ SpeechFeedbackClaim,
	items []SpeechFeedbackDraftItem,
) (SpeechFeedback, error) {
	repository.completedItems = append(
		[]SpeechFeedbackDraftItem(nil),
		items...,
	)
	return SpeechFeedback{}, nil
}

func (repository *speechFeedbackRepositoryStub) CompleteSpeechFeedbackInsufficient(
	_ context.Context,
	_ SpeechFeedbackClaim,
	reasons []SpeechFeedbackReasonCode,
) (SpeechFeedback, error) {
	repository.insufficientReasons = append(
		[]SpeechFeedbackReasonCode(nil),
		reasons...,
	)
	return SpeechFeedback{}, nil
}

func (repository *speechFeedbackRepositoryStub) FailSpeechFeedback(
	_ context.Context,
	_ SpeechFeedbackClaim,
	failure SpeechFeedbackStableFailure,
	_ SpeechFeedbackWorkerConfiguration,
) (SpeechFeedbackStatus, error) {
	repository.failure = &failure
	if failure.Retryable {
		return SpeechFeedbackQueued, nil
	}
	return SpeechFeedbackFailed, nil
}

func (*speechFeedbackRepositoryStub) EnsureConfirmedConversationTurn(
	context.Context,
	string,
	string,
	string,
) (SpeechFeedbackReference, error) {
	panic("not used")
}

func (*speechFeedbackRepositoryStub) EnsureConfirmedAgentVoiceMessage(
	context.Context,
	string,
	string,
	string,
) (SpeechFeedbackReference, error) {
	panic("not used")
}

func (*speechFeedbackRepositoryStub) GetSpeechFeedback(
	context.Context,
	string,
	string,
) (SpeechFeedback, error) {
	panic("not used")
}

func (*speechFeedbackRepositoryStub) FindSpeechFeedbackByConversationTurn(
	context.Context,
	string,
	string,
) (SpeechFeedbackReference, bool, error) {
	panic("not used")
}

func (*speechFeedbackRepositoryStub) FindSpeechFeedbackByAgentMessage(
	context.Context,
	string,
	string,
) (SpeechFeedbackReference, bool, error) {
	panic("not used")
}

func validSpeechFeedbackClaim() SpeechFeedbackClaim {
	digest := sha256.Sum256([]byte("source"))
	return SpeechFeedbackClaim{
		SpeechFeedbackID: "729cdce7-4d33-418c-8497-d2932c651003",
		OwnerUserID:      "f475b521-a96f-44be-b447-8b85bed7e6e9",
		Source: SpeechFeedbackSource{
			SourceKind:           SpeechFeedbackSourceAgentVoiceMessage,
			ThreadID:             "b8075bee-00bc-47ec-b28b-fccf5b57bd87",
			MessageID:            "47d04075-2a5f-45b6-a580-6327717ce16a",
			TranscriptEvidenceID: "acfd7c7e-11c7-42d5-a21a-54633cab2517",
			CandidateVersion:     1,
		},
		CanonicalText:    "I work this project.",
		SourceDigest:     digest,
		AttemptCount:     1,
		FencingToken:     1,
		LeaseExpiresAt:   time.Now().UTC().Add(time.Minute),
		StrategyRef:      SpeechFeedbackStrategyRef,
		PipelineVersion:  SpeechFeedbackPipelineVersion,
		SourceConsistent: true,
	}
}

func validSpeechFeedbackWorkerConfiguration() SpeechFeedbackWorkerConfiguration {
	return SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   30 * time.Second,
		RetryDelay:      time.Second,
		StrategyRef:     SpeechFeedbackStrategyRef,
		PipelineVersion: SpeechFeedbackPipelineVersion,
		PromptVersion:   SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
}
