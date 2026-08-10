package run

import (
	"strings"
	"testing"
)

func TestValidModelIDSupportsQualifiedProviderModels(t *testing.T) {
	tests := map[string]bool{
		"qwen3.5-flash":          true,
		"moonshotai/kimi-k2.6":   true,
		"vendor/family/model-v1": true,
		"/leading-separator":     false,
		"trailing-separator/":    false,
		"repeated//separator":    false,
		"unsafe/../model":        false,
		"contains whitespace":    false,
		strings.Repeat("a", 129): false,
	}
	for model, want := range tests {
		t.Run(model, func(t *testing.T) {
			if got := ValidModelID(model); got != want {
				t.Fatalf("ValidModelID(%q) = %v, want %v", model, got, want)
			}
		})
	}
}
