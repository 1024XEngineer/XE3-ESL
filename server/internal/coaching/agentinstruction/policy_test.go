package agentinstruction

import (
	"strings"
	"testing"
)

func TestProviderIncludesStructuredProfileWriteAndFailurePolicy(t *testing.T) {
	instruction := (Provider{}).Render()
	if instruction.Version != VersionV3 || !instruction.Valid() {
		t.Fatalf("instruction = %#v", instruction)
	}
	for _, required := range []string{
		"explicit stable current fact",
		"durable preference",
		"exact supporting excerpt",
		"Never save role-play",
		"forget selected fields",
		"never claim that a profile change succeeded",
	} {
		if !strings.Contains(instruction.Content, required) {
			t.Fatalf("instruction missing %q", required)
		}
	}
}
