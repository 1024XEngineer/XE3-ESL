package config

import (
	"testing"
	"time"
)

func TestLoadSpeechRecognition(t *testing.T) {
	setSpeechEnvironment(t)
	t.Setenv("QIANWEN_ASR_TIMEOUT", "75s")

	cfg, err := LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load speech recognition: %v", err)
	}
	if cfg.Provider != SpeechProviderQianwen ||
		cfg.BaseURL != "https://dashscope.aliyuncs.com/api/v1" ||
		cfg.Model != "fun-asr-flash-2026-06-15" ||
		cfg.Timeout != 75*time.Second ||
		cfg.APIKey.Reveal() != "test-secret-value" {
		t.Fatalf("unexpected speech recognition config: %#v", cfg)
	}
}

func TestLoadSpeechSynthesis(t *testing.T) {
	setSpeechEnvironment(t)
	t.Setenv("QIANWEN_TTS_TIMEOUT", "45s")
	t.Setenv("QIANWEN_TTS_TEMP_DIRECTORY", " /private/tmp/xe3-voice ")

	cfg, err := LoadSpeechSynthesis()
	if err != nil {
		t.Fatalf("load speech synthesis: %v", err)
	}
	if cfg.Provider != SpeechProviderQianwen ||
		cfg.BaseURL != "https://dashscope.aliyuncs.com/api/v1" ||
		cfg.Model != "qwen-audio-3.0-tts-flash" ||
		cfg.Voice != "loongeva_v3.6" ||
		cfg.LanguageHint != "en" ||
		cfg.Timeout != 45*time.Second ||
		cfg.TempDirectory != "/private/tmp/xe3-voice" ||
		cfg.APIKey.Reveal() != "test-secret-value" {
		t.Fatalf("unexpected speech synthesis config: %#v", cfg)
	}
}

func TestSpeechConfigurationUsesIndependentTimeoutDefaults(t *testing.T) {
	setSpeechEnvironment(t)
	t.Setenv("QIANWEN_ASR_TIMEOUT", "")
	t.Setenv("QIANWEN_TTS_TIMEOUT", "")

	asr, err := LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load speech recognition: %v", err)
	}
	tts, err := LoadSpeechSynthesis()
	if err != nil {
		t.Fatalf("load speech synthesis: %v", err)
	}
	if asr.Timeout != defaultASRTimeout {
		t.Fatalf("ASR timeout = %s, want %s", asr.Timeout, defaultASRTimeout)
	}
	if tts.Timeout != defaultTTSTimeout {
		t.Fatalf("TTS timeout = %s, want %s", tts.Timeout, defaultTTSTimeout)
	}
}

func TestLoadSpeechRecognitionRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing provider", key: "SPEECH_RECOGNITION_PROVIDER", value: ""},
		{name: "unsupported provider", key: "SPEECH_RECOGNITION_PROVIDER", value: "fake"},
		{name: "missing base URL", key: "QIANWEN_ASR_BASE_URL", value: ""},
		{name: "missing model", key: "QIANWEN_ASR_MODEL", value: ""},
		{name: "invalid timeout", key: "QIANWEN_ASR_TIMEOUT", value: "soon"},
		{name: "excessive timeout", key: "QIANWEN_ASR_TIMEOUT", value: "301s"},
		{name: "missing API key", key: "DASHSCOPE_API_KEY", value: ""},
		{name: "API key whitespace", key: "DASHSCOPE_API_KEY", value: "secret value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setSpeechEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadSpeechRecognition(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestLoadSpeechSynthesisRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing provider", key: "SPEECH_SYNTHESIS_PROVIDER", value: ""},
		{name: "unsupported provider", key: "SPEECH_SYNTHESIS_PROVIDER", value: "fake"},
		{name: "missing base URL", key: "QIANWEN_TTS_BASE_URL", value: ""},
		{name: "missing model", key: "QIANWEN_TTS_MODEL", value: ""},
		{name: "missing voice", key: "QIANWEN_TTS_VOICE", value: ""},
		{name: "missing language", key: "QIANWEN_TTS_LANGUAGE", value: ""},
		{name: "invalid timeout", key: "QIANWEN_TTS_TIMEOUT", value: "soon"},
		{name: "zero timeout", key: "QIANWEN_TTS_TIMEOUT", value: "0s"},
		{name: "missing API key", key: "DASHSCOPE_API_KEY", value: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setSpeechEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadSpeechSynthesis(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func setSpeechEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("SPEECH_RECOGNITION_PROVIDER", SpeechProviderQianwen)
	t.Setenv("QIANWEN_ASR_BASE_URL", "https://dashscope.aliyuncs.com/api/v1")
	t.Setenv("QIANWEN_ASR_MODEL", "fun-asr-flash-2026-06-15")
	t.Setenv("QIANWEN_ASR_TIMEOUT", "")
	t.Setenv("SPEECH_SYNTHESIS_PROVIDER", SpeechProviderQianwen)
	t.Setenv("QIANWEN_TTS_BASE_URL", "https://dashscope.aliyuncs.com/api/v1")
	t.Setenv("QIANWEN_TTS_MODEL", "qwen-audio-3.0-tts-flash")
	t.Setenv("QIANWEN_TTS_VOICE", "loongeva_v3.6")
	t.Setenv("QIANWEN_TTS_LANGUAGE", "en")
	t.Setenv("QIANWEN_TTS_TIMEOUT", "")
	t.Setenv("QIANWEN_TTS_TEMP_DIRECTORY", "")
	t.Setenv("DASHSCOPE_API_KEY", "test-secret-value")
}
