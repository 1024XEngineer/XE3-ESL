package assistant_test

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/assistant"
)

func TestModuleName(t *testing.T) {
	if got := assistant.New().Name(); got != "assistant" {
		t.Fatalf("expected assistant module name, got %q", got)
	}
}
