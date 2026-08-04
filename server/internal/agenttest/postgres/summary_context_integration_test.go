package postgres_test

import (
	"context"
	"strings"
	"testing"
)

func TestPostgresContextAssemblerSelectsSummaryAndExcludesCoveredMessages(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	generator := &recordingTextGenerator{result: successfulTextResult()}
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		generator,
		testRunConfiguration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-context-first",
		"My product interview is next week.",
	); err != nil {
		t.Fatalf("submit first Run: %v", err)
	}
	checkpoint, err := repository.summary.CreateCheckpoint(
		context.Background(),
		summaryCommand(thread.ID, "", 1, 2, "context source"),
	)
	if err != nil {
		t.Fatalf("create summary checkpoint: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-context-current",
		"What should I practice next?",
	)
	if err != nil {
		t.Fatalf("submit current Run: %v", err)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actor,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("get ContextManifest: %v", err)
	}
	if manifest.SummaryContextPolicyVersion != "summary-context-v1" ||
		manifest.SummaryContextStatus != "selected" ||
		manifest.SelectedSummary == nil ||
		manifest.SelectedSummary.CheckpointID != checkpoint.ID ||
		manifest.SelectedSummary.SourceFromSequence != 1 ||
		manifest.SelectedSummary.CoveredThroughSequence != 2 ||
		manifest.OmittedMessageCount != 2 ||
		manifest.TrimReason != "summary_checkpoint" ||
		len(manifest.SelectedMessages) != 1 ||
		manifest.SelectedMessages[0].MessageID != submission.UserMessage.ID {
		t.Fatalf("unexpected summary ContextManifest: %#v", manifest)
	}
	requests := generator.Requests()
	if len(requests) != 2 || len(requests[1].Messages) != 2 {
		t.Fatalf("unexpected provider requests: %#v", requests)
	}
	system := requests[1].Messages[0].Content
	if !strings.Contains(system, "<thread_summary>") ||
		!strings.Contains(
			system,
			"Prepare for an English product interview",
		) ||
		strings.Contains(system, "My product interview is next week.") {
		t.Fatalf("unexpected summary system context: %q", system)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE agent_context_manifests
SET selected_summary_model = 'mismatched-model'
WHERE run_id = $1`,
		submission.Run.ID,
	); err == nil {
		t.Fatal("Manifest accepted Summary metadata that does not match checkpoint")
	}
}

func TestPostgresContextAssemblerAuditsSummaryOmittedByBudget(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	configuration := testRunConfiguration
	configuration.MaxInputCharacters = 5000
	generator := &recordingTextGenerator{result: successfulTextResult()}
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		generator,
		configuration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-budget-first",
		"Remember this earlier context.",
	); err != nil {
		t.Fatalf("submit first Run: %v", err)
	}
	command := summaryCommand(thread.ID, "", 1, 2, "large context source")
	command.Content.Background = make([]string, 8)
	for index := range command.Content.Background {
		command.Content.Background[index] = strings.Repeat("x", 500)
	}
	if _, err := repository.summary.CreateCheckpoint(
		context.Background(),
		command,
	); err != nil {
		t.Fatalf("create large summary checkpoint: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-budget-current",
		strings.Repeat("c", 1000),
	)
	if err != nil {
		t.Fatalf("submit current Run: %v", err)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actor,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("get ContextManifest: %v", err)
	}
	if manifest.SummaryContextStatus != "omitted_budget" ||
		manifest.SelectedSummary != nil ||
		manifest.OmittedMessageCount != 0 ||
		manifest.TrimReason != "none" ||
		len(manifest.SelectedMessages) != 3 {
		t.Fatalf("unexpected omitted Summary manifest: %#v", manifest)
	}
	requests := generator.Requests()
	if len(requests) != 2 ||
		strings.Contains(requests[1].Messages[0].Content, "<thread_summary>") {
		t.Fatalf("summary should not enter budgeted request: %#v", requests)
	}
}

func TestPostgresContextAssemblerCombinesSummaryAndBudgetTrim(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	configuration := testRunConfiguration
	configuration.MaxInputCharacters = 5000
	generator := &recordingTextGenerator{result: successfulTextResult()}
	_, dataService, runService, repository := newAgentRunServices(
		t,
		database.pool,
		generator,
		configuration,
	)
	actor := testActorA()
	thread, err := dataService.CreateThread(context.Background(), actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if _, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-combined-first",
		"First summarized message.",
	); err != nil {
		t.Fatalf("submit first Run: %v", err)
	}
	checkpoint, err := repository.summary.CreateCheckpoint(
		context.Background(),
		summaryCommand(thread.ID, "", 1, 2, "combined source"),
	)
	if err != nil {
		t.Fatalf("create summary checkpoint: %v", err)
	}
	if _, err := dataService.AppendUserMessage(
		context.Background(),
		actor,
		thread.ID,
		"summary-combined-large",
		strings.Repeat("o", 3500),
	); err != nil {
		t.Fatalf("append large recent message: %v", err)
	}
	submission, err := runService.SubmitText(
		context.Background(),
		actor,
		thread.ID,
		"summary-combined-current",
		strings.Repeat("c", 1000),
	)
	if err != nil {
		t.Fatalf("submit current Run: %v", err)
	}
	manifest, err := runService.GetContextManifest(
		context.Background(),
		actor,
		submission.Run.ID,
	)
	if err != nil {
		t.Fatalf("get ContextManifest: %v", err)
	}
	if manifest.SummaryContextStatus != "selected" ||
		manifest.SelectedSummary == nil ||
		manifest.SelectedSummary.CheckpointID != checkpoint.ID ||
		manifest.OmittedMessageCount != 3 ||
		manifest.TrimReason != "summary_checkpoint_and_budget" ||
		len(manifest.SelectedMessages) != 1 ||
		manifest.SelectedMessages[0].MessageID != submission.UserMessage.ID {
		t.Fatalf("unexpected combined trim manifest: %#v", manifest)
	}
}
