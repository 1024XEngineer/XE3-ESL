package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestEvaluationRuntimeUsesIndependentSpeechWorkers(t *testing.T) {
	runtime, err := buildEvaluationRuntime(
		evaluationProcessorStub{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.workers) != 1+evaluationSpeechConcurrency {
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

type evaluationProcessorStub struct{}

func (evaluationProcessorStub) ProcessSession(context.Context) (bool, error) {
	return false, nil
}

func (evaluationProcessorStub) ProcessSpeech(context.Context) (bool, error) {
	return false, nil
}
