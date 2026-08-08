package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestBuildAgentVoiceInputApplicationRequiresExplicitEnablementAndStore(
	t *testing.T,
) {
	application, err := buildAgentVoiceInputApplication(
		nil,
		nil,
		nil,
		nil,
		agentrun.Configuration{},
	)
	if err != nil || application != nil {
		t.Fatalf(
			"disabled application = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceInputApplication(
		[]VoiceConfiguration{{
			AgentVoiceInputEnabled: true,
		}},
		nil,
		nil,
		nil,
		agentrun.Configuration{},
	)
	if err == nil || application != nil {
		t.Fatalf(
			"enabled application without store = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceInputApplication(
		[]VoiceConfiguration{{
			AgentVoiceInputEnabled: true,
			ObjectStore:            newVoiceObjectStore(),
		}},
		nil,
		nil,
		nil,
		agentrun.Configuration{},
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
		emptyBootstrapMemorySearcher{},
		VoiceConfiguration{AgentVoiceInputEnabled: true},
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
		emptyBootstrapMemorySearcher{},
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("build composition without Agent voice: %v", err)
	}
	if composition.agentVoiceReclaimer != nil {
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

func TestProductionAgentVoiceCompositionRegistersAllRoutes(t *testing.T) {
	pool := voiceIntegrationDatabase(t)
	textGenerator := &voiceTextGenerator{}
	configuration := VoiceConfiguration{
		Recognizer: newTestSpeechRecognizer(
			agentvoice.TranscriptionResult{
				ID:         "bootstrap-asr-result",
				Provider:   "fake",
				Model:      "fake-asr-v1",
				Transcript: "A bootstrap transcript.",
			},
		),
		Synthesizer: newFailingTestSpeechSynthesizer(
			context.Canceled,
		),
		TemporaryAudio:         newVoiceTestVault(t),
		ObjectStore:            newVoiceObjectStore(),
		AgentVoiceInputEnabled: true,
		ScratchDirectory:       t.TempDir(),
		ObjectReadAllowedHosts: []string{
			"private-audio.example.invalid",
		},
		AudioStagedTTL:         time.Hour,
		ASRLease:               5 * time.Second,
		AudioReadTimeout:       time.Second,
		ReviewHistoryCursorKey: make([]byte, 32),
	}
	configuration.PracticeRecognizer = practiceVoiceRecognizerAdapter{
		recognizer: configuration.Recognizer,
	}
	configuration.PracticeSynthesizer = practiceVoiceSynthesizerAdapter{
		synthesizer: configuration.Synthesizer,
	}
	configuration.QuestionGenerator = practiceVoiceQuestionGeneratorAdapter{
		generator: textGenerator,
	}
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
		emptyBootstrapMemorySearcher{},
		nil,
		nil,
		nil,
		nil,
		configuration,
	)
	if err != nil {
		t.Fatalf("build production Agent voice composition: %v", err)
	}
	if composition.agentVoiceReclaimer == nil {
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
				"/voice-message-candidates/realtime",
		},
		{
			http.MethodGet,
			"/v1/agent-voice-message-candidates/" + resourceID,
		},
		{
			http.MethodPost,
			"/v1/agent-voice-message-candidates/" + resourceID +
				"/retries",
		},
		{
			http.MethodDelete,
			"/v1/agent-voice-message-candidates/" + resourceID,
		},
		{
			http.MethodPost,
			"/v1/agent-voice-message-candidates/" + resourceID +
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

type emptyBootstrapMemorySearcher struct{}

func (emptyBootstrapMemorySearcher) Search(
	context.Context,
	memory.SearchRequest,
) ([]memory.SearchHit, error) {
	return []memory.SearchHit{}, nil
}
