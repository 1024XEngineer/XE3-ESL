package modelid

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	tests := map[string]bool{
		"":                       false,
		"qwen3.5-flash":          true,
		"moonshotai/kimi-k2.6":   true,
		"vendor/family/model-v1": true,
		strings.Repeat("a", 128): true,
		"/leading-separator":     false,
		"trailing-separator/":    false,
		"repeated//separator":    false,
		"unsafe/../model":        false,
		"vendor/.hidden":         false,
		"contains whitespace":    false,
		" leading-space":         false,
		"trailing-space ":        false,
		"vendor/model\t":         false,
		"vendor/model\n":         false,
		strings.Repeat("a", 129): false,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			if got := Valid(value); got != want {
				t.Fatalf("Valid(%q) = %v, want %v", value, got, want)
			}
		})
	}
}
