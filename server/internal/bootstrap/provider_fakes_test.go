package bootstrap

import (
	"context"
	"errors"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
)

func testAgentModelProviders(run agentrun.TextGenerator) AgentModelProviders {
	return AgentModelProviders{
		Run:     run,
		Memory:  testMemoryGenerator{run: run},
		Summary: testSummaryGenerator{run: run},
	}
}

func testJobTargetGenerator(
	run agentrun.TextGenerator,
) preparation.JobTargetGenerator {
	return testPreparationGenerator{run: run}
}

type testMemoryGenerator struct {
	run agentrun.TextGenerator
}

func (generator testMemoryGenerator) GenerateJSON(
	ctx context.Context,
	request memory.GenerationRequest,
) (memory.GenerationResult, error) {
	result, err := generateTestJSON(
		ctx,
		generator.run,
		request.SystemPrompt,
		request.UserPrompt,
	)
	return memory.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, err
}

type testSummaryGenerator struct {
	run agentrun.TextGenerator
}

func (generator testSummaryGenerator) GenerateJSON(
	ctx context.Context,
	request agentsummary.GenerationRequest,
) (agentsummary.GenerationResult, error) {
	result, err := generateTestJSON(
		ctx,
		generator.run,
		request.SystemPrompt,
		request.UserPrompt,
	)
	return agentsummary.GenerationResult{
		Provider: result.Provider,
		Model:    result.Model,
		Content:  result.Content,
	}, err
}

type testPreparationGenerator struct {
	run agentrun.TextGenerator
}

func (generator testPreparationGenerator) GenerateJobTarget(
	ctx context.Context,
	request preparation.JobTargetGenerationRequest,
) (preparation.JobTargetGenerationResult, error) {
	result, err := generateTestJSON(
		ctx,
		generator.run,
		request.SystemInstruction,
		request.UserMaterial,
	)
	return preparation.JobTargetGenerationResult{Content: result.Content}, err
}

func generateTestJSON(
	ctx context.Context,
	generator agentrun.TextGenerator,
	systemPrompt string,
	userPrompt string,
) (agentrun.TextResult, error) {
	if generator == nil {
		return agentrun.TextResult{}, errors.New("test text generator is required")
	}
	return generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{
			{Role: agentrun.TextRoleSystem, Content: systemPrompt},
			{Role: agentrun.TextRoleUser, Content: userPrompt},
		},
		ResponseFormat: agentrun.TextResponseFormatJSON,
	})
}

type testSpeechRecognizer struct {
	result agentvoice.TranscriptionResult
}

func newTestSpeechRecognizer(
	result agentvoice.TranscriptionResult,
) *testSpeechRecognizer {
	return &testSpeechRecognizer{result: result}
}

func (recognizer *testSpeechRecognizer) Transcribe(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
) (agentvoice.TranscriptionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := agentvoice.ValidateTranscriptionRequest(request); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	return recognizer.result, nil
}

func (recognizer *testSpeechRecognizer) TranscribeStream(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	result, err := recognizer.Transcribe(ctx, request)
	if err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{Transcript: result.Transcript},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{Transcript: result.Transcript, Final: true},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	return result, nil
}

type failingTestSpeechSynthesizer struct {
	err error
}

func newFailingTestSpeechSynthesizer(err error) *failingTestSpeechSynthesizer {
	return &failingTestSpeechSynthesizer{err: err}
}

func (synthesizer *failingTestSpeechSynthesizer) Synthesize(
	ctx context.Context,
	request agentvoice.SynthesisRequest,
) (agentvoice.SynthesisResult, error) {
	if err := ctx.Err(); err != nil {
		return agentvoice.SynthesisResult{}, err
	}
	if err := agentvoice.ValidateSynthesisRequest(request); err != nil {
		return agentvoice.SynthesisResult{}, err
	}
	return agentvoice.SynthesisResult{}, synthesizer.err
}

var (
	_ agentvoice.StreamingSpeechRecognizer = (*testSpeechRecognizer)(nil)
	_ agentvoice.SpeechSynthesizer         = (*failingTestSpeechSynthesizer)(nil)
)
