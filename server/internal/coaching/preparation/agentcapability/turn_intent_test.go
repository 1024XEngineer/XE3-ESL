package agentcapability

import (
	"context"
	"strings"
	"testing"
)

func TestPracticeTurnIntentResolverKeepsBackgroundConversational(t *testing.T) {
	generator := &intentFixtureGenerator{content: `{"intent":"CONVERSE"}`}
	resolver, err := NewPracticeTurnIntentResolver(generator)
	if err != nil {
		t.Fatalf("NewPracticeTurnIntentResolver() error = %v", err)
	}
	intent, err := resolver.Resolve(
		context.Background(),
		"你好，我最近在准备雅思。",
		false,
	)
	if err != nil || intent != PracticeTurnIntentConverse {
		t.Fatalf("Resolve() = %q, %v", intent, err)
	}
	if !strings.Contains(generator.request.SystemInstruction, "context, not an action request") ||
		!strings.Contains(generator.request.UserMaterial, "我最近在准备雅思") {
		t.Fatalf("request = %#v", generator.request)
	}
}

func TestPracticeTurnIntentResolverDocumentsExplicitCreateBoundary(t *testing.T) {
	tests := []struct {
		message string
		intent  PracticeTurnIntent
	}{
		{
			message: "帮我创建一个雅思口语练习，但我还没想好练哪一部分。",
			intent:  PracticeTurnIntentRequestCreate,
		},
		{
			message: "我想创建一个职场英语练习，但不知道练什么。",
			intent:  PracticeTurnIntentRequestCreate,
		},
		{
			message: "我以后想练面试英语。",
			intent:  PracticeTurnIntentConverse,
		},
		{
			message: "我可能想练一下职场英语。",
			intent:  PracticeTurnIntentConverse,
		},
		{
			message: "你建议我先练哪种面试？",
			intent:  PracticeTurnIntentProposeCreate,
		},
	}
	for _, test := range tests {
		t.Run(string(test.intent)+"/"+test.message, func(t *testing.T) {
			generator := &intentFixtureGenerator{
				content: `{"intent":"` + string(test.intent) + `"}`,
			}
			resolver, err := NewPracticeTurnIntentResolver(generator)
			if err != nil {
				t.Fatalf("NewPracticeTurnIntentResolver() error = %v", err)
			}
			intent, err := resolver.Resolve(
				context.Background(),
				test.message,
				false,
			)
			if err != nil || intent != test.intent {
				t.Fatalf("Resolve() = %q, %v", intent, err)
			}
			if !strings.Contains(
				generator.request.SystemInstruction,
				`missing details never revoke action authorization`,
			) || !strings.Contains(
				generator.request.SystemInstruction,
				`但我还没想好练哪一部分`,
			) || !strings.Contains(
				generator.request.UserMaterial,
				test.message,
			) {
				t.Fatalf("request = %#v", generator.request)
			}
		})
	}
}

func TestPracticeTurnIntentResolverRejectsPendingIntentWithoutPendingState(t *testing.T) {
	resolver, err := NewPracticeTurnIntentResolver(
		&intentFixtureGenerator{content: `{"intent":"CONFIRM_PENDING"}`},
	)
	if err != nil {
		t.Fatalf("NewPracticeTurnIntentResolver() error = %v", err)
	}
	if _, err = resolver.Resolve(context.Background(), "对的", false); err == nil {
		t.Fatal("Resolve() accepted pending intent without pending state")
	}
}

type intentFixtureGenerator struct {
	content string
	request PracticeTurnIntentGenerationRequest
}

func (generator *intentFixtureGenerator) GeneratePracticeTurnIntent(
	_ context.Context,
	request PracticeTurnIntentGenerationRequest,
) (PracticeTurnIntentGenerationResult, error) {
	generator.request = request
	return PracticeTurnIntentGenerationResult{Content: generator.content}, nil
}
