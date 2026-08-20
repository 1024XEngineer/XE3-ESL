package agentinstruction

import (
	"strings"
	"testing"
)

func TestProviderIncludesStructuredProfileWriteAndFailurePolicy(t *testing.T) {
	instruction := (Provider{}).Render()
	if instruction.Version != VersionV5 || !instruction.Valid() {
		t.Fatalf("instruction = %#v", instruction)
	}
	for _, required := range []string{
		"explicit stable current fact",
		"durable preference",
		"exact supporting excerpt",
		"Never save role-play",
		"forget selected fields",
		"never claim that a profile change succeeded",
		"server exposes the practice preview capability only after authorizing",
		"does not authorize creating a practice",
		"Never inherit creation authorization",
		"respond naturally to what the user shared",
		"current-turn application state is supplied",
		"For a NO_CHANGE conclusion",
		"each alternative wording is optional",
		"Never invent or carry over a counterpart role",
	} {
		if !strings.Contains(instruction.Content, required) {
			t.Fatalf("instruction missing %q", required)
		}
	}
}
