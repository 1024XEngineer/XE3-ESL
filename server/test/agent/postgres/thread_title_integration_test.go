package postgres_test

import (
	"context"
	"testing"
	"time"

	agenttitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
)

func TestCompletedRunGeneratesOnePersistedThreadTitle(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, service, runService, repositories := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := service.CreateThread(ctx, actor, "")
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if thread.Title != "" {
		t.Fatalf("new Thread title = %q", thread.Title)
	}

	if _, err := runService.SubmitText(
		ctx,
		actor,
		thread.ID,
		"thread-title-first-run",
		"你好，我想准备产品经理模拟面试。",
	); err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	var persistedTitle *string
	if err := database.pool.QueryRow(
		ctx,
		"SELECT title FROM agent_threads WHERE id = $1",
		thread.ID,
	).Scan(&persistedTitle); err != nil {
		t.Fatalf("read raw Thread title: %v", err)
	}
	if persistedTitle != nil {
		t.Fatalf("raw pre-generation title = %q", *persistedTitle)
	}
	beforeGeneration, err := service.GetThread(ctx, actor, thread.ID)
	if err != nil {
		t.Fatalf("get untitled Thread: %v", err)
	}
	if beforeGeneration.Title != "" {
		t.Fatalf("pre-generation title = %q", beforeGeneration.Title)
	}

	generator := &fixedTitleGenerator{result: agenttitle.GenerationResult{
		Provider: "fake",
		Model:    "configured-model",
		Content:  `{"title":"产品经理面试准备"}`,
	}}
	serviceGenerator, err := agenttitle.NewService(
		generator,
		agenttitle.Configuration{
			PromptVersion: "thread-title-prompt-v1",
			Provider:      "fake",
			Model:         "configured-model",
		},
	)
	if err != nil {
		t.Fatalf("new Title service: %v", err)
	}
	worker, err := agenttitle.NewWorker(
		repositories.title,
		serviceGenerator,
		titleWorkerConfiguration(),
	)
	if err != nil {
		t.Fatalf("new Title worker: %v", err)
	}
	result, err := worker.ProcessPending(ctx, 1)
	if err != nil {
		t.Fatalf("process Title job: %v", err)
	}
	if result.Completed != 1 || generator.CallCount() != 1 {
		t.Fatalf("result = %#v calls = %d", result, generator.CallCount())
	}

	found, err := service.GetThread(ctx, actor, thread.ID)
	if err != nil {
		t.Fatalf("get titled Thread: %v", err)
	}
	if found.Title != "产品经理面试准备" {
		t.Fatalf("GetThread title = %q", found.Title)
	}
	page, err := service.PageThreads(ctx, actor, 20, "")
	if err != nil {
		t.Fatalf("page Threads: %v", err)
	}
	if len(page.Threads) != 1 || page.Threads[0].Title != found.Title {
		t.Fatalf("PageThreads = %#v", page.Threads)
	}
	focused, err := service.SetFocusedThread(ctx, actor, thread.ID)
	if err != nil {
		t.Fatalf("focus Thread: %v", err)
	}
	if focused.Title != found.Title {
		t.Fatalf("focused title = %q", focused.Title)
	}

	if _, err := runService.SubmitText(
		ctx,
		actor,
		thread.ID,
		"thread-title-second-run",
		"继续练习。",
	); err != nil {
		t.Fatalf("complete second Run: %v", err)
	}
	result, err = worker.ProcessPending(ctx, 1)
	if err != nil {
		t.Fatalf("process after second Run: %v", err)
	}
	if result.Claimed != 0 || generator.CallCount() != 1 {
		t.Fatalf("second result = %#v calls = %d", result, generator.CallCount())
	}
	var jobCount int
	if err := database.pool.QueryRow(
		ctx,
		"SELECT count(*) FROM agent_thread_title_jobs WHERE source_thread_id = $1",
		thread.ID,
	).Scan(&jobCount); err != nil {
		t.Fatalf("count Title jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("Title job count = %d", jobCount)
	}
}

func titleWorkerConfiguration() agenttitle.WorkerConfiguration {
	return agenttitle.WorkerConfiguration{
		LeaseDuration: time.Minute,
		MaxAttempts:   agenttitle.DefaultWorkerMaxAttempts,
		Generation: agenttitle.Configuration{
			PromptVersion: "thread-title-prompt-v1",
			Provider:      "fake",
			Model:         "configured-model",
		},
	}
}
