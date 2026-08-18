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
