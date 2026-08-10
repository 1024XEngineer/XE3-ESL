package ielts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type answerRepositoryStub struct {
	value AnswerPreparation
	fail  bool
}

func (stub *answerRepositoryStub) Create(_ context.Context, _ requestcontext.Actor, command CreateAnswerPreparationCommand) (AnswerPreparation, bool, error) {
	stub.value = AnswerPreparation{ID: command.ID, Question: command.Question, PersonalPoints: command.Request.PersonalPoints, TargetBand: command.Request.TargetBand, Status: AnswerPreparationDraft, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return stub.value, false, nil
}
func (stub *answerRepositoryStub) Get(context.Context, requestcontext.Actor, string) (AnswerPreparation, error) {
	return stub.value, nil
}
func (stub *answerRepositoryStub) Update(_ context.Context, _ requestcontext.Actor, command UpdateAnswerPreparationCommand) (AnswerPreparation, bool, error) {
	if command.Request.ExpectedVersion != stub.value.Version {
		return AnswerPreparation{}, false, ErrAnswerPreparationConflict
	}
	stub.value.PersonalPoints, stub.value.TargetBand, stub.value.Status = command.Request.PersonalPoints, command.Request.TargetBand, AnswerPreparationDraft
	stub.value.Answer, stub.value.Outline, stub.value.UsefulExpressions, stub.value.SpeechText = "", nil, nil, ""
	stub.value.Version++
	return stub.value, false, nil
}
func (stub *answerRepositoryStub) BeginGeneration(_ context.Context, _ requestcontext.Actor, command BeginAnswerGenerationCommand) (AnswerPreparation, bool, error) {
	if command.Request.ExpectedVersion != stub.value.Version {
		return AnswerPreparation{}, false, ErrAnswerPreparationConflict
	}
	stub.value.Status, stub.value.Version, stub.value.GenerationRevision = AnswerPreparationGenerating, stub.value.Version+1, stub.value.GenerationRevision+1
	return stub.value, false, nil
}
func (stub *answerRepositoryStub) CompleteGeneration(_ context.Context, _ requestcontext.Actor, command CompleteAnswerGenerationCommand) (AnswerPreparation, error) {
	stub.value.Status, stub.value.Answer, stub.value.Outline = AnswerPreparationReady, command.Result.Answer, command.Result.Outline
	stub.value.UsefulExpressions, stub.value.SpeechText = command.Result.UsefulExpressions, command.Result.SpeechText
	stub.value.Version++
	return stub.value, nil
}
func (stub *answerRepositoryStub) FailGeneration(_ context.Context, _ requestcontext.Actor, command FailAnswerGenerationCommand) error {
	stub.fail = true
	stub.value.Status, stub.value.FailureCode, stub.value.Version = AnswerPreparationFailed, command.FailureCode, stub.value.Version+1
	return nil
}
func (stub *answerRepositoryStub) Delete(context.Context, requestcontext.Actor, DeleteAnswerPreparationCommand) (bool, error) {
	return false, nil
}

type answerQuestionStub struct{}

func (answerQuestionStub) ResolveAnswerQuestion(_ context.Context, reference QuestionReference) (ResolvedQuestion, error) {
	return ResolvedQuestion{Reference: reference, Prompt: "Do you enjoy music?"}, nil
}

type answerGeneratorStub struct {
	err     error
	calls   int
	request AnswerGenerationRequest
}

func (stub *answerGeneratorStub) GenerateAnswerPreparation(_ context.Context, request AnswerGenerationRequest) (AnswerGenerationResult, error) {
	stub.calls++
	stub.request = request
	if stub.err != nil {
		return AnswerGenerationResult{}, stub.err
	}
	return AnswerGenerationResult{Answer: "I enjoy music because it helps me relax.", Outline: []string{"preference", "reason"}, UsefulExpressions: []string{"helps me relax"}, SpeechText: "I enjoy music because it helps me relax."}, nil
}

type answerIDStub struct{}

func (answerIDStub) NewAnswerPreparationID() (string, error) {
	return "ielts_answer_0123456789abcdef0123456789abcdef", nil
}

func TestAnswerPreparationServiceCreatesStableQuestionAndGeneratesReadyResult(t *testing.T) {
	repository := &answerRepositoryStub{}
	generator := &answerGeneratorStub{}
	service, err := NewAnswerPreparationService(repository, answerQuestionStub{}, generator, answerIDStub{})
	if err != nil {
		t.Fatalf("NewAnswerPreparationService: %v", err)
	}
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	created, _, err := service.Create(context.Background(), actor, "create-key-1", CreateAnswerPreparationRequest{Question: QuestionReference{BankID: "bank-2026", Part: PracticeModePart1, SourceID: "p1-topic-001", QuestionPosition: 2}, PersonalPoints: []string{"  I play piano  "}, TargetBand: 6.5})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Question.Reference.QuestionPosition != 2 || created.Question.Prompt != "Do you enjoy music?" || created.PersonalPoints[0] != "I play piano" {
		t.Fatalf("created = %#v", created)
	}
	ready, _, err := service.Generate(context.Background(), actor, created.ID, "generate-key-1", GenerateAnswerPreparationRequest{ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if ready.Status != AnswerPreparationReady || ready.Answer == "" || len(ready.Outline) == 0 || len(ready.UsefulExpressions) == 0 || ready.SpeechText == "" || generator.calls != 1 {
		t.Fatalf("ready = %#v, calls=%d", ready, generator.calls)
	}
	if generator.request.Part != PracticeModePart1 {
		t.Fatalf("generation part = %q", generator.request.Part)
	}
}

func TestAnswerGenerationLengthCapsLikelySpeakingTime(t *testing.T) {
	short := AnswerGenerationResult{Answer: "short answer", SpeechText: "short answer"}
	if !validGenerationLength(PracticeModePart1, short) ||
		!validGenerationLength(PracticeModePart2, short) ||
		!validGenerationLength(PracticeModePart3, short) {
		t.Fatal("short answer should fit every part")
	}
	for _, test := range []struct {
		name  string
		part  PracticeMode
		words int
	}{
		{name: "Part 1", part: PracticeModePart1, words: 76},
		{name: "Part 2", part: PracticeModePart2, words: 241},
		{name: "Part 3", part: PracticeModePart3, words: 96},
	} {
		t.Run(test.name, func(t *testing.T) {
			words := make([]string, test.words)
			for index := range words {
				words[index] = "word"
			}
			tooLong := strings.Join(words, " ")
			if validGenerationLength(test.part, AnswerGenerationResult{Answer: tooLong, SpeechText: tooLong}) {
				t.Fatalf("%s answer with %d words should be rejected", test.name, test.words)
			}
		})
	}
}

func TestAnswerTimingKeepsPartSpecificSpokenTargets(t *testing.T) {
	part1 := answerTiming(PracticeModePart1)
	if part1.seconds != "20-30" || part1.words != "35-55" {
		t.Fatalf("Part 1 timing changed: %#v", part1)
	}
	part2 := answerTiming(PracticeModePart2)
	if part2.seconds != "80-110" || part2.words != "160-220" || !strings.Contains(part2.structure, "every cue-card point") {
		t.Fatalf("Part 2 timing = %#v", part2)
	}
	part3 := answerTiming(PracticeModePart3)
	if part3.seconds != "25-40" || part3.words != "50-80" || !strings.Contains(part3.structure, "avoid an essay-style") {
		t.Fatalf("Part 3 timing = %#v", part3)
	}
}

func TestAnswerPreparationServicePersistsExplicitFailedState(t *testing.T) {
	repository := &answerRepositoryStub{value: AnswerPreparation{ID: "ielts_answer_0123456789abcdef0123456789abcdef", Question: ResolvedQuestion{Prompt: "Question"}, Status: AnswerPreparationDraft, Version: 1, TargetBand: 6.5}}
	generator := &answerGeneratorStub{err: errors.New("provider unavailable")}
	service, _ := NewAnswerPreparationService(repository, answerQuestionStub{}, generator, answerIDStub{})
	_, _, err := service.Generate(context.Background(), requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}, repository.value.ID, "generate-key-2", GenerateAnswerPreparationRequest{ExpectedVersion: 1})
	if !errors.Is(err, ErrAnswerPreparationGeneration) || !repository.fail || repository.value.Status != AnswerPreparationFailed || repository.value.FailureCode != "provider_error" {
		t.Fatalf("err=%v value=%#v", err, repository.value)
	}
}

func TestAnswerPreparationServiceRejectsStaleEditWithoutOverwriting(t *testing.T) {
	repository := &answerRepositoryStub{value: AnswerPreparation{ID: "ielts_answer_0123456789abcdef0123456789abcdef", PersonalPoints: []string{"new"}, Status: AnswerPreparationDraft, Version: 3}}
	service, _ := NewAnswerPreparationService(repository, answerQuestionStub{}, &answerGeneratorStub{}, answerIDStub{})
	_, _, err := service.Update(context.Background(), requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}, repository.value.ID, "update-key-1", UpdateAnswerPreparationRequest{ExpectedVersion: 2, PersonalPoints: []string{"stale"}, TargetBand: 7})
	if !errors.Is(err, ErrAnswerPreparationConflict) || repository.value.PersonalPoints[0] != "new" {
		t.Fatalf("err=%v value=%#v", err, repository.value)
	}
}
