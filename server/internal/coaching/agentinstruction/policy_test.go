package agentinstruction

import (
	"strings"
	"testing"
)

func TestProviderIncludesStructuredProfileWriteAndFailurePolicy(t *testing.T) {
	instruction := (Provider{}).Render()
	if instruction.Version != VersionV6 || !instruction.Valid() {
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
		"generic greeting or opener",
		"普通问候不得主动列举或推荐雅思、面试或任何具体练习类别",
		"respond naturally to what the user shared",
		"current-turn application state is supplied",
		"For a NO_CHANGE conclusion",
		"For a NOT_APPLICABLE conclusion",
		"handle the user's request normally",
		"each alternative wording is optional",
		"Never invent or carry over a counterpart role",
	} {
		if !strings.Contains(instruction.Content, required) {
			t.Fatalf("instruction missing %q", required)
		}
	}
}

func TestProviderIncludesReplyLanguagePolicy(t *testing.T) {
	instruction := (Provider{}).Render()
	for _, required := range []string{
		"never changes behavior or tool use",
		"explicit language requests",
		"current message's clear primary language",
		"isolated loanwords do not switch it",
		"ambiguous input uses the conversation language",
		"explanation_language, then Simplified Chinese",
		"Clear English stays English",
	} {
		if !strings.Contains(instruction.Content, required) {
			t.Fatalf("instruction missing %q", required)
		}
	}
}
