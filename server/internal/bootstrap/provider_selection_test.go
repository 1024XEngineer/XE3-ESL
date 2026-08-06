package bootstrap

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestProviderSelectionRejectsUnregisteredImplementations(t *testing.T) {
	t.Parallel()

	if provider, err := NewMemoryEmbedder(config.EmbeddingConfig{
		Provider: "fake",
	}); err == nil || provider != nil {
		t.Fatalf("unregistered Memory embedder = %T, %v", provider, err)
	}
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

func TestDisabledAvatarProviderIsExplicitlyAbsent(t *testing.T) {
	t.Parallel()

	provider, err := NewAvatarTokenProvider(config.SpatiusConfig{})
	if err != nil || provider != nil {
		t.Fatalf("disabled Avatar provider = %T, %v", provider, err)
	}
}
