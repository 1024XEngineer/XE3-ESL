package postgres_test

import (
	"context"
	"testing"
)

func TestFirstUserMessageDerivesOneStableThreadTitle(t *testing.T) {
	database := newAgentTestDatabase(t)
	service, runService, _ := newAgentRunServices(
		t,
		database.pool,
		newFixedTextGenerator(successfulTextResult()),
		testRunConfiguration,
	)
	ctx := context.Background()
	actor := testActorA()
	thread, err := service.CreateThread(ctx, actor)
	if err != nil {
		t.Fatalf("create Thread: %v", err)
	}
	if thread.Title != "" {
		t.Fatalf("new Thread title = %q", thread.Title)
	}

	const firstMessage = "你好， 我想准备产品经理模拟面试。"
	if _, err := runService.SubmitText(
		ctx,
		actor,
		thread.ID,
		"thread-title-first-run",
		firstMessage,
	); err != nil {
		t.Fatalf("complete first Run: %v", err)
	}
	found, err := service.GetThread(ctx, actor, thread.ID)
	if err != nil {
		t.Fatalf("get titled Thread: %v", err)
	}
	if found.Title != firstMessage {
		t.Fatalf("first-message title = %q", found.Title)
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
	afterSecondRun, err := service.GetThread(ctx, actor, thread.ID)
	if err != nil {
		t.Fatalf("get Thread after second Run: %v", err)
	}
	if afterSecondRun.Title != found.Title {
		t.Fatalf(
			"second message changed title from %q to %q",
			found.Title,
			afterSecondRun.Title,
		)
	}
}
