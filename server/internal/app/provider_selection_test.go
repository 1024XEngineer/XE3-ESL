package app

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestProviderSelectionRejectsUnregisteredImplementations(t *testing.T) {
	t.Parallel()

	if provider, err := NewAgentSpeechRecognizer(
		config.SpeechRecognitionConfig{Provider: "fake"},
	); err == nil || provider != nil {
		t.Fatalf("unregistered Agent recognizer = %T, %v", provider, err)
	}
	if provider, err := NewAgentSpeechSynthesizer(
		config.SpeechSynthesisConfig{Provider: "fake"},
	); err == nil || provider != nil {
		t.Fatalf("unregistered Agent synthesizer = %T, %v", provider, err)
	}
}
