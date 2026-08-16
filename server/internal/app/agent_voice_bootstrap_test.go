package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestAgentRealtimePCMRecognizerUsesAvailableCapability(t *testing.T) {
	recognizer := newTestSpeechRecognizer(agentvoice.TranscriptionResult{
		Transcript: "available",
	})
	configured := agentRealtimePCMRecognizer([]RuntimeAudioConfiguration{{
		AgentVoice: AgentVoiceConfiguration{Recognizer: recognizer},
	}})
	if configured != recognizer {
		t.Fatalf("configured realtime recognizer = %T", configured)
	}

	configured = agentRealtimePCMRecognizer([]RuntimeAudioConfiguration{{
		AgentVoice: AgentVoiceConfiguration{
			Recognizer: durableOnlyAgentSpeechRecognizer{
				StreamingSpeechRecognizer: recognizer,
			},
		},
	}})
	if configured != nil {
		t.Fatalf("missing realtime capability = %T", configured)
	}
}

type durableOnlyAgentSpeechRecognizer struct {
	agentvoice.StreamingSpeechRecognizer
}

func TestBuildAgentVoiceInputApplicationRequiresExplicitEnablementAndStore(
	t *testing.T,
) {
	application, err := buildAgentVoiceInputApplication(
		nil,
		nil,
		nil,
		nil,
		agentrun.Configuration{},
		nil,
	)
	if err != nil || application != nil {
		t.Fatalf(
			"disabled application = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceInputApplication(
		[]RuntimeAudioConfiguration{{
			AgentVoice: AgentVoiceConfiguration{InputEnabled: true},
		}},
		nil,
		nil,
		nil,
		agentrun.Configuration{},
		nil,
	)
	if err == nil || application != nil {
		t.Fatalf(
			"enabled application without store = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceInputApplication(
		[]RuntimeAudioConfiguration{{
			AgentVoice: AgentVoiceConfiguration{InputEnabled: true},
			Media:      AudioMediaConfiguration{ObjectStore: newVoiceObjectStore()},
		}},
		nil,
		nil,
		nil,
		agentrun.Configuration{},
		nil,
	)
	if err == nil || application != nil {
		t.Fatalf(
			"Fake store without explicit host allowlist = %#v, %v",
			application,
			err,
		)
	}
}

func TestAgentVoiceCompositionRequiresCleanupAwareConstructor(t *testing.T) {
	if _, _, err := NewIdentityAndAgentModules(
		context.Background(),
		nil,
		nil,
		"",
		AgentModelProviders{},
		agentrun.Configuration{},
		RuntimeAudioConfiguration{
			AgentVoice: AgentVoiceConfiguration{InputEnabled: true},
		},
	); err == nil {
		t.Fatal("legacy composition accepted Agent voice without cleanup")
	}

	pool := voiceIntegrationDatabase(t)
	composition, err := buildIdentityAgentComposition(
		context.Background(),
		pool,
		nil,
		"",
		testAgentModelProviders(&voiceTextGenerator{}),
		agentrun.Configuration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("build composition without Agent voice: %v", err)
	}
	if composition.mediaReclaimer != nil {
		t.Fatal("disabled Agent voice retained a typed-nil reclaimer")
	}
}

func TestAgentVoiceObjectReadAllowedHostsComeFromTrustedStorageConfig(
	t *testing.T,
) {
	hosts, err := AgentVoiceObjectReadAllowedHosts(
		config.ObjectStorageConfig{
			Enabled:  true,
			Endpoint: "https://oss-cn-shanghai.aliyuncs.com",
			Bucket:   "private-audio",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 ||
		hosts[0] != "private-audio.oss-cn-shanghai.aliyuncs.com" ||
		hosts[1] != "oss-cn-shanghai.aliyuncs.com" {
		t.Fatalf("derived signed-URL hosts = %#v", hosts)
	}
	if _, err := AgentVoiceObjectReadAllowedHosts(
		config.ObjectStorageConfig{
			Enabled:  true,
			Endpoint: "https://127.0.0.1",
			Bucket:   "private-audio",
		},
	); err == nil {
		t.Fatal("private IP storage origin was accepted")
	}
}

func TestAgentVoiceObjectReadAllowedHostsUseQiniuS3Endpoint(t *testing.T) {
	hosts, err := AgentVoiceObjectReadAllowedHosts(
		config.ObjectStorageConfig{
			Enabled:  true,
			Provider: config.ObjectStorageProviderQiniuKodo,
			Endpoint: "https://s3.cn-east-1.qiniucs.com",
			Bucket:   "qiniu_bucket_name",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "s3.cn-east-1.qiniucs.com" {
		t.Fatalf("derived Qiniu signed-URL hosts = %#v", hosts)
	}
}

func TestProductionAgentVoiceCompositionRegistersAllRoutes(t *testing.T) {
	pool := voiceIntegrationDatabase(t)
	textGenerator := &voiceTextGenerator{}
	configuration := RuntimeAudioConfiguration{
		AgentVoice: AgentVoiceConfiguration{
			Recognizer: newTestSpeechRecognizer(agentvoice.TranscriptionResult{
				ID: "bootstrap-asr-result", Provider: "fake", Model: "fake-asr-v1",
				Transcript: "A bootstrap transcript.",
			}),
			Synthesizer:  newFailingTestSpeechSynthesizer(context.Canceled),
			InputEnabled: true, ScratchDirectory: t.TempDir(),
			ObjectReadAllowedHosts: []string{"private-audio.example.invalid"},
			ReadTimeout:            time.Second, StagedTTL: time.Hour,
			ASRLease: 5 * time.Second,
		},
		PracticeInteraction: PracticeInteractionConfiguration{
			TemporaryAudio: newVoiceTestVault(t), AudioStagedTTL: time.Hour,
			ASRLease: 5 * time.Second, RealtimeReadTimeout: time.Second,
		},
		Media: AudioMediaConfiguration{
			ObjectStore: newVoiceObjectStore(), UploadLease: time.Minute,
		},
	}
	configuration.PracticeInteraction.Recognizer = practiceVoiceRecognizerAdapter{
		recognizer: configuration.AgentVoice.Recognizer,
	}
	configuration.PracticeInteraction.RecordedRecognizer =
		configuration.PracticeInteraction.Recognizer
	configuration.PracticeInteraction.Synthesizer = practiceVoiceSynthesizerAdapter{
		synthesizer: configuration.AgentVoice.Synthesizer,
	}
	configuration.PracticeInteraction.QuestionGenerator = practiceQuestionGeneratorAdapter{
		generator: textGenerator,
	}
	configuration.PracticeInteraction.Evaluation = voiceTestPracticeEvaluationSchedulers()
	composition, err := buildIdentityAgentComposition(
		context.Background(),
		pool,
		nil,
		"",
		testAgentModelProviders(textGenerator),
		agentrun.Configuration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		nil,
		nil,
		nil,
		configuration,
	)
	if err != nil {
		t.Fatalf("build production Agent voice composition: %v", err)
	}
	if composition.mediaReclaimer == nil {
		t.Fatal("production composition did not retain voice cleanup")
	}
	router := NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{
			composition.identity.module,
			composition.agentModule,
		},
	)

	const resourceID = "20000000-0000-4000-8000-000000000001"
	routes := []struct {
		method string
		path   string
	}{
		{
			http.MethodGet,
			"/v1/agent-threads/" + resourceID +
				"/voice-transcriptions/realtime",
		},
		{
			http.MethodGet,
			"/v1/agent-threads/" + resourceID +
				"/voice-drafts/realtime",
		},
		{
			http.MethodGet,
			"/v1/agent-voice-drafts/" + resourceID,
		},
		{
			http.MethodPost,
			"/v1/agent-voice-drafts/" + resourceID +
				"/retries",
		},
		{
			http.MethodDelete,
			"/v1/agent-voice-drafts/" + resourceID,
		},
		{
			http.MethodPost,
			"/v1/agent-voice-drafts/" + resourceID +
				"/confirmations",
		},
		{
			http.MethodGet,
			"/v1/agent-message-audios/" + resourceID + "/playback",
		},
		{
			http.MethodDelete,
			"/v1/agent-message-audios/" + resourceID,
		},
		{
			http.MethodGet,
			"/v1/agent-messages/" + resourceID + "/speech",
		},
	}
	for _, route := range routes {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(route.method, route.path, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf(
				"%s %s status = %d, want authenticated route",
				route.method,
				route.path,
				response.Code,
			)
		}
	}
}

func TestAgentEphemeralTranscriptionRegistersWithoutObjectStorage(t *testing.T) {
	pool := voiceIntegrationDatabase(t)
	textGenerator := &voiceTextGenerator{}
	configuration := RuntimeAudioConfiguration{
		AgentVoice: AgentVoiceConfiguration{
			Recognizer: newTestSpeechRecognizer(agentvoice.TranscriptionResult{
				ID:         "ephemeral-asr-result",
				Provider:   "fake",
				Model:      "fake-asr-v1",
				Transcript: "An ephemeral transcript.",
			}),
			Synthesizer: newFailingTestSpeechSynthesizer(context.Canceled),
			ReadTimeout: time.Second, StagedTTL: time.Hour,
			ASRLease: 5 * time.Second,
		},
		PracticeInteraction: PracticeInteractionConfiguration{
			TemporaryAudio: newVoiceTestVault(t), AudioStagedTTL: time.Hour,
			ASRLease: 5 * time.Second, RealtimeReadTimeout: time.Second,
		},
	}
	configuration.PracticeInteraction.Recognizer = practiceVoiceRecognizerAdapter{
		recognizer: configuration.AgentVoice.Recognizer,
	}
	configuration.PracticeInteraction.RecordedRecognizer =
		configuration.PracticeInteraction.Recognizer
	configuration.PracticeInteraction.Synthesizer = practiceVoiceSynthesizerAdapter{
		synthesizer: configuration.AgentVoice.Synthesizer,
	}
	configuration.PracticeInteraction.QuestionGenerator = practiceQuestionGeneratorAdapter{
		generator: textGenerator,
	}
	configuration.PracticeInteraction.Evaluation = voiceTestPracticeEvaluationSchedulers()
	composition, err := buildIdentityAgentComposition(
		context.Background(),
		pool,
		nil,
		"",
		testAgentModelProviders(textGenerator),
		agentrun.Configuration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		nil,
		nil,
		nil,
		configuration,
	)
	if err != nil {
		t.Fatalf("build composition without object storage: %v", err)
	}
	if composition.mediaReclaimer != nil {
		t.Fatal("ephemeral transcription constructed durable Voice storage")
	}
	router := NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{
			composition.identity.module,
			composition.agentModule,
		},
	)
	const threadID = "20000000-0000-4000-8000-000000000001"
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-transcriptions/realtime",
		nil,
	)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("ephemeral route status = %d", response.Code)
	}

	legacy := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent-threads/"+threadID+
			"/voice-drafts/realtime",
		nil,
	)
	router.ServeHTTP(legacy, legacyRequest)
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("durable Voice route without storage status = %d", legacy.Code)
	}
}
