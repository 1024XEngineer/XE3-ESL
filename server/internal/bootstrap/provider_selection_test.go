package bootstrap

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestProviderSelectionRegistersQiniuASR(t *testing.T) {
	t.Setenv("SPEECH_RECOGNITION_PROVIDER", config.SpeechProviderQiniu)
	t.Setenv("QINIU_ASR_BASE_URL", "wss://api.qnaigc.com/v1/voice/asr")
	t.Setenv("QINIU_ASR_MODEL", "asr")
	t.Setenv("QINIU_ASR_TIMEOUT", "30s")
	t.Setenv("QINIU_AI_API_KEY", "qiniu-test-key")

	configuration, err := config.LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load Qiniu ASR configuration: %v", err)
	}
	agent, err := NewAgentSpeechRecognizer(configuration)
	if err != nil || agent == nil {
		t.Fatalf("Qiniu Agent recognizer = %T, %v", agent, err)
	}
	practice, err := NewPracticeSpeechRecognizer(configuration)
	if err != nil || practice == nil {
		t.Fatalf("Qiniu Practice recognizer = %T, %v", practice, err)
	}
}

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
