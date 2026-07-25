package bootstrap

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestMapRecordingConfirmationError(t *testing.T) {
	terminalConflicts := []error{
		conversation.ErrAudioAssetNotFound,
		conversation.ErrAudioAssetForbidden,
		conversation.ErrAudioAssetAlreadyBound,
		conversation.ErrAudioAssetInvalidTransition,
		conversation.ErrAudioAssetUploadTerminated,
	}
	for _, input := range terminalConflicts {
		if mapped := mapRecordingConfirmationError(input); !errors.Is(
			mapped,
			agent.ErrConflict,
		) {
			t.Errorf("map terminal recording error %v = %v", input, mapped)
		}
	}

	if mapped := mapRecordingConfirmationError(
		conversation.ErrAudioAssetConcurrentUpdate,
	); !errors.Is(mapped, conversation.ErrVoiceRoundProcessing) {
		t.Errorf("map concurrent recording update = %v", mapped)
	}

	fallback := errors.New("recording database failed")
	if mapped := mapRecordingConfirmationError(fallback); !errors.Is(
		mapped,
		fallback,
	) {
		t.Errorf("map recording fallback = %v", mapped)
	}
}

func TestSpeechProviderRegistryUsesOnlyConfiguredQianwen(t *testing.T) {
	setSpeechRegistryEnvironment(t)
	asr, err := config.LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load ASR config: %v", err)
	}
	tts, err := config.LoadSpeechSynthesis()
	if err != nil {
		t.Fatalf("load TTS config: %v", err)
	}
	if _, err := NewSpeechRecognizer(asr); err != nil {
		t.Fatalf("register ASR: %v", err)
	}
	if _, err := NewSpeechSynthesizer(tts); err != nil {
		t.Fatalf("register TTS: %v", err)
	}

	asr.Provider = "fake"
	if _, err := NewSpeechRecognizer(asr); err == nil {
		t.Fatal("unregistered ASR provider was accepted")
	}
	tts.Provider = "fake"
	if _, err := NewSpeechSynthesizer(tts); err == nil {
		t.Fatal("unregistered TTS provider was accepted")
	}
}

func TestBuildVoiceApplicationRequiresOwningModulePorts(t *testing.T) {
	valid := VoiceConfiguration{
		Recognizer:     &voiceTestRecognizer{},
		Synthesizer:    &voiceTestSynthesizer{},
		TemporaryAudio: &voiceTestVault{},
		Ports: VoicePorts{
			ConversationStore: &voiceTestStore{},
			Practice:          &voiceTestPractice{},
			Sessions:          &voiceTestSessions{},
			Questions:         &voiceTestQuestions{},
			Checkpoints:       &voiceTestCheckpoints{},
			Reviews:           &voiceTestReviews{},
		},
	}
	if _, err := buildVoiceApplication(&voiceTestMatters{}, valid); err != nil {
		t.Fatalf("build valid voice application: %v", err)
	}

	missingStore := valid
	missingStore.Ports.ConversationStore = nil
	if _, err := buildVoiceApplication(
		&voiceTestMatters{},
		missingStore,
	); err == nil {
		t.Fatal("missing Conversation Port was accepted")
	}
	if _, err := buildVoiceApplication(nil, valid); err == nil {
		t.Fatal("missing Matter Port was accepted")
	}
}

func setSpeechRegistryEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("SPEECH_RECOGNITION_PROVIDER", "qianwen")
	t.Setenv("QIANWEN_ASR_BASE_URL", "https://dashscope.aliyuncs.com/api/v1")
	t.Setenv("QIANWEN_ASR_MODEL", "fun-asr-flash-2026-06-15")
	t.Setenv("QIANWEN_ASR_TIMEOUT", "5s")
	t.Setenv("SPEECH_SYNTHESIS_PROVIDER", "qianwen")
	t.Setenv("QIANWEN_TTS_BASE_URL", "https://dashscope.aliyuncs.com/api/v1")
	t.Setenv("QIANWEN_TTS_MODEL", "qwen-audio-3.0-tts-flash")
	t.Setenv("QIANWEN_TTS_VOICE", "loongeva_v3.6")
	t.Setenv("QIANWEN_TTS_LANGUAGE", "en")
	t.Setenv("QIANWEN_TTS_TIMEOUT", "5s")
	t.Setenv("QIANWEN_TTS_TEMP_DIRECTORY", "")
	t.Setenv("DASHSCOPE_API_KEY", "ci-test-key-not-a-secret")
}

type voiceTestRecognizer struct {
	ai.SpeechRecognizer
}

type voiceTestSynthesizer struct {
	ai.SpeechSynthesizer
}

type voiceTestVault struct {
	conversation.TemporaryAudioVault
}

type voiceTestStore struct {
	conversation.VoiceRoundStore
}

type voiceTestPractice struct {
	agent.VoicePracticePort
}

type voiceTestSessions struct {
	agent.VoiceSessionPort
}

type voiceTestQuestions struct {
	agent.VoiceQuestionPort
}

type voiceTestCheckpoints struct {
	agent.VoiceCheckpointPort
}

type voiceTestReviews struct {
	agent.VoiceReviewPort
	agent.VoiceReviewReader
}

type voiceTestMatters struct {
	matter.Reader
}
