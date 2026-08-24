package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestEvaluationRuntimeUsesIndependentSpeechWorkers(t *testing.T) {
	runtime, err := buildEvaluationRuntime(
		evaluationProcessorStub{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.workers) != 1+evaluationProfileConcurrency+
		evaluationSpeechConcurrency {
		t.Fatalf("workers = %d", len(runtime.workers))
	}
	speechWorkers := 0
	for _, worker := range runtime.workers {
		if worker.name == "speech" {
			speechWorkers++
		}
	}
	if speechWorkers != evaluationSpeechConcurrency {
		t.Fatalf("speech workers = %d", speechWorkers)
	}
}

func TestEvaluationFailureAttributesExposeSafeDiagnosticMetadata(t *testing.T) {
	attributes := evaluationErrorAttributes("session", evaluationFailureStub{})
	values := make(map[string]string, len(attributes))
	for _, attribute := range attributes {
		values[attribute.Key] = attribute.Value.String()
	}
	for key, want := range map[string]string{
		"lane":            "session",
		"evaluation_id":   "70000000-0000-4000-8000-000000000001",
		"evaluation_kind": "SESSION_REPORT",
		"attempt_count":   "2",
		"failure_code":    "PROVIDER_RESPONSE_INVALID",
		"retryable":       "true",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q; attributes=%#v", key, values[key], want, values)
		}
	}
	if _, leaked := values["error"]; leaked {
		t.Fatalf("diagnostics leaked raw error: %#v", values)
	}
}

type evaluationProcessorStub struct{}

func (evaluationProcessorStub) ProcessSession(context.Context) (bool, error) {
	return false, nil
}

func (evaluationProcessorStub) ProcessProfile(context.Context) (bool, error) {
	return false, nil
}

func (evaluationProcessorStub) ProcessSpeech(context.Context) (bool, error) {
	return false, nil
}

type evaluationFailureStub struct{}

func (evaluationFailureStub) Error() string { return "sensitive provider output" }
func (evaluationFailureStub) EvaluationID() string {
	return "70000000-0000-4000-8000-000000000001"
}
func (evaluationFailureStub) EvaluationKind() evaluation.Kind {
	return evaluation.KindSessionReport
}
func (evaluationFailureStub) EvaluationAttemptCount() int { return 2 }
func (evaluationFailureStub) EvaluationJobError() evaluation.JobError {
	return evaluation.JobError{
		Code: "PROVIDER_RESPONSE_INVALID", Retryable: true,
		Message: "Evaluation processing failed.",
	}
}
