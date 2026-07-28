package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	aifake "github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestBuildAgentVoiceMessageApplicationRequiresExplicitEnablementAndStore(
	t *testing.T,
) {
	application, err := buildAgentVoiceMessageApplication(
		nil,
		nil,
		nil,
		nil,
		agent.RunConfiguration{},
	)
	if err != nil || application != nil {
		t.Fatalf(
			"disabled application = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceMessageApplication(
		[]VoiceConfiguration{{
			AgentVoiceMessagesEnabled: true,
		}},
		nil,
		nil,
		nil,
		agent.RunConfiguration{},
	)
	if err == nil || application != nil {
		t.Fatalf(
			"enabled application without store = %#v, %v",
			application,
			err,
		)
	}

	application, err = buildAgentVoiceMessageApplication(
		[]VoiceConfiguration{{
			AgentVoiceMessagesEnabled: true,
			ObjectStore:               newVoiceObjectStore(),
		}},
		nil,
		nil,
		nil,
		agent.RunConfiguration{},
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
		nil,
		agent.RunConfiguration{},
		VoiceConfiguration{AgentVoiceMessagesEnabled: true},
	); err == nil {
		t.Fatal("legacy composition accepted Agent voice without cleanup")
	}

	pool := voiceIntegrationDatabase(t)
	composition, err := buildIdentityAgentComposition(
		context.Background(),
		pool,
		nil,
		"",
		&voiceTextGenerator{},
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
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
	configuration := VoiceConfiguration{
		Recognizer: aifake.NewSpeechRecognizer(
			ai.TranscriptionResult{
				ID:         "bootstrap-asr-result",
				Provider:   "fake",
				Model:      "fake-asr-v1",
				Transcript: "A bootstrap transcript.",
			},
		),
		Synthesizer: aifake.NewFailingSpeechSynthesizer(
			context.Canceled,
		),
		TemporaryAudio:            newVoiceTestVault(t),
		ObjectStore:               newVoiceObjectStore(),
		AgentVoiceMessagesEnabled: true,
		ScratchDirectory:          t.TempDir(),
		ObjectReadAllowedHosts: []string{
			"private-audio.example.invalid",
		},
		AudioStagedTTL:          time.Hour,
		ASRLease:                5 * time.Second,
		ReviewGenerationTimeout: 2 * time.Second,
		AudioReadTimeout:        time.Second,
		ReviewHistoryCursorKey:  make([]byte, 32),
	}
	composition, err := buildIdentityAgentComposition(
		context.Background(),
		pool,
		nil,
		"",
		&voiceTextGenerator{},
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
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
			http.MethodPost,
			"/v1/agent-threads/" + resourceID +
				"/voice-message-candidates",
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
