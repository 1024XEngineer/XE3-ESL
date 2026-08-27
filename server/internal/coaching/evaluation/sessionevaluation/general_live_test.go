//go:build live

package sessionevaluation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/sessionevaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

func TestQiniuGeneralEvaluatorProducesStableValidReports(t *testing.T) {
	snapshot := generalLiveSnapshot()
	if !snapshot.Valid() {
		t.Fatal("live GeneralEvaluator snapshot is invalid")
	}
	if os.Getenv("QIANWEN_LIVE_TEST") != "1" {
		t.Skip("set QIANWEN_LIVE_TEST=1 to run billable provider calls")
	}
	configuration, err := config.LoadTextGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Provider != config.TextProviderQiniu {
		t.Fatalf("TEXT_GENERATION_PROVIDER = %q, want qiniu", configuration.Provider)
	}
	realGenerator, err := qianwen.NewEvaluationScoringGenerator(qianwen.TextConfig{
		Provider: configuration.Provider, BaseURL: configuration.BaseURL,
		Model: configuration.EvaluationModel, Timeout: configuration.Timeout,
		MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	generator := &countingGenerator{delegate: realGenerator}
	evaluators, err := sessionevaluation.New(generator)
	if err != nil {
		t.Fatal(err)
	}
	lineages, err := sessionevaluation.Lineages(
		configuration.Provider, configuration.EvaluationModel,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"TASK_ACHIEVEMENT", "CLARITY_COHERENCE", "LANGUAGE_CONTROL", "INTERACTION",
	}
	for attempt := 1; attempt <= 3; attempt++ {
		before := generator.calls
		ctx, cancel := context.WithTimeout(context.Background(), configuration.Timeout)
		encoded, evaluateErr := evaluators.EvaluateGeneral(
			ctx, evaluation.Record{}, snapshot, lineages.General,
		)
		cancel()
		if evaluateErr != nil {
			t.Fatalf("attempt %d EvaluateGeneral() error = %v", attempt, evaluateErr)
		}
		if generator.calls-before != 1 {
			t.Fatalf("attempt %d generation calls = %d, want 1", attempt, generator.calls-before)
		}
		var formal report.FormalReport
		if err := evaluation.DecodeStrict(encoded, &formal); err != nil {
			t.Fatalf("attempt %d decode report: %v", attempt, err)
		}
		if !formal.Valid() {
			t.Fatalf("attempt %d produced an invalid FormalReport", attempt)
		}
		if formal.ScoreabilityStatus != report.ReportScoreabilityProvisional {
			t.Fatalf(
				"attempt %d scoreability = %q, want PROVISIONAL",
				attempt,
				formal.ScoreabilityStatus,
			)
		}
		if len(formal.Questions) != 3 {
			t.Fatalf("attempt %d questions = %d, want 3", attempt, len(formal.Questions))
		}
		for index, question := range formal.Questions {
			if question.Answer == nil || question.Answer.Transcript == "" {
				t.Fatalf("attempt %d question %d has no answer", attempt, index+1)
			}
		}
		gotKeys := make([]string, len(formal.Dimensions))
		seen := make(map[string]struct{}, len(formal.Dimensions))
		for index, dimension := range formal.Dimensions {
			gotKeys[index] = dimension.Key
			if dimension.Score == nil {
				t.Fatalf(
					"attempt %d dimension %q has no score",
					attempt,
					dimension.Key,
				)
			}
			if _, duplicate := seen[dimension.Key]; duplicate {
				t.Fatalf("attempt %d duplicated dimension key %q", attempt, dimension.Key)
			}
			seen[dimension.Key] = struct{}{}
		}
		if !slices.Equal(gotKeys, wantKeys) {
			t.Fatalf("attempt %d dimension keys = %#v, want %#v", attempt, gotKeys, wantKeys)
		}
	}
}

type countingGenerator struct {
	delegate textgeneration.Generator
	calls    int
}

func (generator *countingGenerator) Generate(
	ctx context.Context,
	request textgeneration.Request,
) (textgeneration.Result, error) {
	generator.calls++
	return generator.delegate.Generate(ctx, request)
}

func generalLiveSnapshot() evaluation.SessionInputSnapshot {
	now := time.Now().UTC()
	questions := []string{
		"Explain the project delay to your manager.",
		"What caused the delay and what did you learn?",
		"What concrete next step do you recommend?",
	}
	transcripts := []string{
		"The release is currently two days late because the payment integration took longer than our estimate. I should have raised that risk in Monday's stand-up, so I take responsibility for the late warning. The core user flow is stable, but two edge cases still fail in regression testing. I want to explain the impact clearly and agree on a realistic recovery plan with you today.",
		"The main cause was an external dependency whose ownership and response time were not confirmed before implementation started. I initially treated it as a small task, but it affected authentication, error handling, and our test environment. I learned that I need to identify cross-team dependencies during planning, assign an owner, and escalate a blocked item within one working day instead of waiting for the next status meeting.",
		"I recommend that we finish the critical authentication fix today, run the complete regression suite tomorrow morning, and publish the verified build by Friday afternoon. I will send you a written update at noon with passed and failed cases. If the remaining edge case is still unresolved, we can disable only that optional payment method and release the rest safely. Does that plan match the priority you want for this week?",
	}
	snapshot := evaluation.SessionInputSnapshot{
		SchemaVersion:       evaluation.SessionInputSchemaVersion,
		SessionID:           "20000000-0000-4000-8000-000000000002",
		SessionVersion:      1,
		EvaluationPolicyRef: evaluation.WorkplaceEvaluationPolicyRef,
		PracticeExperience:  "WORKPLACE",
		SceneCategory:       "WORKPLACE_GENERAL",
		PracticeMode:        "GUIDED",
		CompletedAt:         now,
		AcousticCapability:  evaluation.AcousticCapabilityNotConfigured,
		PlanSnapshot:        json.RawMessage(`{}`),
		Participants:        json.RawMessage(`[]`),
		Questions:           make([]evaluation.SessionEvidenceQuestion, len(questions)),
		Turns:               make([]evaluation.SessionEvidenceTurn, len(transcripts)),
	}
	for index := range questions {
		position := index + 1
		questionID := fmt.Sprintf("40000000-0000-4000-8000-%012d", position)
		turnID := fmt.Sprintf("30000000-0000-4000-8000-%012d", position)
		snapshot.Questions[index] = evaluation.SessionEvidenceQuestion{
			ID: questionID, Position: position, Text: questions[index],
			SpeakerParticipantID:    "assistant-1",
			AddresseeParticipantIDs: []string{"user-1"},
		}
		snapshot.Turns[index] = evaluation.SessionEvidenceTurn{
			ID: turnID, Position: position, QuestionID: questionID,
			RespondentParticipantID: "user-1", Transcript: transcripts[index],
			Effective: true, ConfirmedAt: now,
		}
	}
	return snapshot
}

var _ textgeneration.Generator = (*countingGenerator)(nil)
