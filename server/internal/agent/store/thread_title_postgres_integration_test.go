package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPostgresThreadTitlesAreDerivedFromFirstOwnedUserMessage(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, service := newAgentDataServices(t, database.pool)
	ctx := context.Background()
	actorA := requestcontext.Actor{
		UserID:    agentTestUserA,
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	actorB := requestcontext.Actor{
		UserID:    agentTestUserB,
		SessionID: "20000000-0000-4000-8000-000000000002",
	}

	empty, err := service.CreateThread(ctx, actorA, "")
	if err != nil {
		t.Fatalf("create empty Thread: %v", err)
	}
	if empty.Title != "" {
		t.Fatalf("new Thread title = %q", empty.Title)
	}

	titled, err := service.CreateThread(ctx, actorA, "")
	if err != nil {
		t.Fatalf("create titled Thread: %v", err)
	}
	if _, err := service.AppendUserMessage(
		ctx,
		actorA,
		titled.ID,
		"title-message-1",
		"  我想\n练习\t英文自我介绍  ",
	); err != nil {
		t.Fatalf("append first user Message: %v", err)
	}
	if _, err := service.AppendUserMessage(
		ctx,
		actorA,
		titled.ID,
		"title-message-2",
		"第二条消息不能覆盖标题",
	); err != nil {
		t.Fatalf("append second user Message: %v", err)
	}

	foreign, err := service.CreateThread(ctx, actorB, "")
	if err != nil {
		t.Fatalf("create foreign Thread: %v", err)
	}
	if _, err := service.AppendUserMessage(
		ctx,
		actorB,
		foreign.ID,
		"foreign-title-message",
		"另一个用户的私密标题",
	); err != nil {
		t.Fatalf("append foreign user Message: %v", err)
	}

	long, err := service.CreateThread(ctx, actorA, "")
	if err != nil {
		t.Fatalf("create long-title Thread: %v", err)
	}
	if _, err := service.AppendUserMessage(
		ctx,
		actorA,
		long.ID,
		"long-title-message",
		strings.Repeat("面", conversation.ThreadTitleContentLimit+1),
	); err != nil {
		t.Fatalf("append long title Message: %v", err)
	}

	found, err := service.GetThread(ctx, actorA, titled.ID)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if found.Title != "我想 练习 英文自我介绍" {
		t.Fatalf("GetThread title = %q", found.Title)
	}

	page, err := service.PageThreads(ctx, actorA, 20, "")
	if err != nil {
		t.Fatalf("PageThreads: %v", err)
	}
	titles := make(map[string]string, len(page.Threads))
	for _, thread := range page.Threads {
		titles[thread.ID] = thread.Title
	}
	if len(titles) != 3 {
		t.Fatalf("owner A Thread count = %d, want 3", len(titles))
	}
	if _, visible := titles[foreign.ID]; visible {
		t.Fatal("foreign Thread was visible in owner A page")
	}
	if titles[empty.ID] != "" ||
		titles[titled.ID] != "我想 练习 英文自我介绍" ||
		titles[long.ID] !=
			strings.Repeat("面", conversation.ThreadTitleContentLimit)+"…" {
		t.Fatalf("PageThreads titles = %#v", titles)
	}

	focused, err := service.SetFocusedThread(ctx, actorA, titled.ID)
	if err != nil {
		t.Fatalf("SetFocusedThread: %v", err)
	}
	if focused.Title != "我想 练习 英文自我介绍" {
		t.Fatalf("focused title = %q", focused.Title)
	}
	restored, present, err := service.GetFocusedThread(ctx, actorA)
	if err != nil {
		t.Fatalf("GetFocusedThread: %v", err)
	}
	if !present || restored.Title != focused.Title {
		t.Fatalf("restored focused Thread = %#v, present = %t", restored, present)
	}
}
