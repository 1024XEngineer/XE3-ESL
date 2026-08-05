package run

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestWithAssistantEnricherRejectsNilDependency(t *testing.T) {
	service := &Service{}
	if err := WithAssistantEnricher(nil)(service); err == nil {
		t.Fatal("WithAssistantEnricher(nil) error = nil")
	}
}

func TestCompleteAssistantPassesTrustedFinalReplyToEnricherAndRepository(
	t *testing.T,
) {
	t.Parallel()

	actor, run := enrichmentActorAndRun()
	result := finalLoopResult("You explained that clearly.")
	wantEnrichment := AssistantEnrichment{
		Memes: []AssistantMemeDraft{validAssistantMemeDraft()},
	}
	enricher := &recordingAssistantEnricher{result: wantEnrichment}
	repository := &recordingCompletionRepository{
		loopRepository: loopRepository{},
		result:         Run{ID: run.ID, Status: StatusCompleted},
	}
	service := &Service{
		repository:        repository,
		assistantEnricher: enricher,
	}

	completed, err := service.completeAssistant(
		context.Background(),
		actor,
		run,
		"I finally understand it.",
		result,
	)
	if err != nil {
		t.Fatalf("completeAssistant() error = %v", err)
	}
	if completed != repository.result {
		t.Fatalf("completed Run = %#v, want %#v", completed, repository.result)
	}
	wantRequest := AssistantEnrichmentRequest{
		Actor:            actor,
		RunID:            run.ID,
		ThreadID:         run.ThreadID,
		InputMessageID:   run.InputMessageID,
		UserContent:      "I finally understand it.",
		AssistantContent: result.Content,
	}
	if !reflect.DeepEqual(enricher.request, wantRequest) {
		t.Fatalf("Enricher request = %#v, want %#v", enricher.request, wantRequest)
	}
	if repository.ownerID != actor.UserID ||
		repository.runID != run.ID ||
		repository.workerLeaseToken != run.WorkerLeaseToken {
		t.Fatalf(
			"Complete identity = %q/%q/%q",
			repository.ownerID,
			repository.runID,
			repository.workerLeaseToken,
		)
	}
	wantCompletion := Completion{
		Content:    result.Content,
		Result:     result,
		Enrichment: wantEnrichment,
	}
	if !reflect.DeepEqual(repository.completion, wantCompletion) {
		t.Fatalf(
			"Repository completion = %#v, want %#v",
			repository.completion,
			wantCompletion,
		)
	}
}

func TestCompleteAssistantFallsBackToTextWhenEnricherFails(t *testing.T) {
	t.Parallel()

	actor, run := enrichmentActorAndRun()
	result := finalLoopResult("The original reply remains available.")
	repository := &recordingCompletionRepository{
		loopRepository: loopRepository{},
		result:         Run{ID: run.ID, Status: StatusCompleted},
	}
	service := &Service{
		repository: repository,
		assistantEnricher: &recordingAssistantEnricher{
			err: errors.New("classifier unavailable"),
		},
	}

	if _, err := service.completeAssistant(
		context.Background(),
		actor,
		run,
		"Please help me.",
		result,
	); err != nil {
		t.Fatalf("completeAssistant() error = %v", err)
	}
	if len(repository.completion.Enrichment.Memes) != 0 ||
		repository.completion.Content != result.Content {
		t.Fatalf("fallback completion = %#v", repository.completion)
	}
}

func TestEnrichAssistantRejectsInvalidAttachmentProjection(t *testing.T) {
	t.Parallel()

	actor, run := enrichmentActorAndRun()
	invalid := validAssistantMemeDraft()
	invalid.AssetKey = "../outside.webp"
	service := &Service{
		assistantEnricher: &recordingAssistantEnricher{
			result: AssistantEnrichment{
				Memes: []AssistantMemeDraft{invalid},
			},
		},
	}
	result := service.enrichAssistant(
		context.Background(),
		actor,
		run,
		"Hello.",
		"Hi there.",
	)
	if len(result.Memes) != 0 {
		t.Fatalf("invalid enrichment reached completion: %#v", result)
	}
}

func enrichmentActorAndRun() (requestcontext.Actor, Run) {
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	return actor, Run{
		ID:               "30000000-0000-4000-8000-000000000001",
		OwnerID:          actor.UserID,
		ThreadID:         "40000000-0000-4000-8000-000000000001",
		InputMessageID:   "50000000-0000-4000-8000-000000000001",
		WorkerLeaseToken: "lease-token",
	}
}

func validAssistantMemeDraft() AssistantMemeDraft {
	return AssistantMemeDraft{
		MemeID:                      "encouraging-001",
		PackID:                      "speakup-default",
		PackVersion:                 "1.0.0",
		Category:                    "encouraging",
		AssetKey:                    "speakup-default/1.0.0/encouraging/001.webp",
		ContentType:                 "image/webp",
		SizeBytes:                   1024,
		Width:                       320,
		Height:                      320,
		ChecksumSHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Position:                    0,
		ClassificationPolicyVersion: "meme-emotion-v1",
		SelectionPolicyVersion:      "meme-selection-v1",
		ClassifierProvider:          "qianwen",
		ClassifierModel:             "qwen-test",
	}
}

type recordingAssistantEnricher struct {
	request AssistantEnrichmentRequest
	result  AssistantEnrichment
	err     error
}

func (enricher *recordingAssistantEnricher) Enrich(
	_ context.Context,
	request AssistantEnrichmentRequest,
) (AssistantEnrichment, error) {
	enricher.request = request
	return enricher.result, enricher.err
}

type recordingCompletionRepository struct {
	loopRepository
	ownerID          string
	runID            string
	workerLeaseToken string
	completion       Completion
	result           Run
}

func (repository *recordingCompletionRepository) Complete(
	_ context.Context,
	ownerID string,
	runID string,
	workerLeaseToken string,
	completion Completion,
) (Run, error) {
	repository.ownerID = ownerID
	repository.runID = runID
	repository.workerLeaseToken = workerLeaseToken
	repository.completion = completion
	return repository.result, nil
}
