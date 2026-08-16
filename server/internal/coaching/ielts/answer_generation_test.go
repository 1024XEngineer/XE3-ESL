package ielts

import (
	"context"
	"testing"

	ieltsdata "github.com/1024XEngineer/XE3-ESL/server/data/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type answerGeneratorStub struct{ input AnswerGenerationInput }

func (stub *answerGeneratorStub) GenerateIELTSAnswer(_ context.Context, input AnswerGenerationInput) (AnswerGenerationResult, error) {
	stub.input = input
	return AnswerGenerationResult{
		RequestID: "answer-request-1", Provider: "qianwen", Model: "qwen3.5-flash",
		Answer:            "I enjoy commuting by train because I can read on the way.",
		Outline:           []string{"direct answer", "reason"},
		UsefulExpressions: []string{"on the way"},
		SpeechText:        "I enjoy commuting by train because I can read on the way.",
	}, nil
}

func TestAnswerGenerationUsesPublishedQuestionAndDoesNotPersistResource(t *testing.T) {
	catalog := answerTestCatalog(t)
	generator := &answerGeneratorStub{}
	service, err := NewAnswerGenerationService(catalog, generator)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	answer, err := service.Generate(context.Background(), requestcontext.Actor{
		UserID:    "00000000-0000-4000-8000-000000000001",
		SessionID: "00000000-0000-4000-8000-000000000002",
	}, AnswerGenerationRequest{
		Question:       QuestionReference{BankID: "ielts-speaking-2026-05-08-mainland", Part: PracticeModePart1, SourceID: "p1-topic-001", QuestionPosition: 1},
		PersonalPoints: []string{"  I read on the train.  "}, TargetBand: 7,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if generator.input.Question != "Do you prefer sad or happy music?" ||
		len(generator.input.PersonalPoints) != 1 ||
		generator.input.PersonalPoints[0] != "I read on the train." ||
		answer.Question.Prompt != "Do you prefer sad or happy music?" {
		t.Fatalf("resolved generation = %#v, answer = %#v", generator.input, answer)
	}
}

func answerTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	file, err := ieltsdata.Files.Open(ieltsdata.CurrentFile)
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer file.Close()
	catalog, err := LoadCatalog(file)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return catalog
}
