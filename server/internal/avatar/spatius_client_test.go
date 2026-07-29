package avatar

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestSpatiusClientSendsServerCredentialAndParsesNestedResponse(
	t *testing.T,
) {
	expiry := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost ||
				request.URL.Path != "/v1/console/session-tokens" ||
				request.Header.Get("X-Api-Key") != "server-only-key" ||
				request.Header.Get("Accept") != "application/json" ||
				request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected request: %#v", request)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var payload struct {
				AppID    string `json:"appId"`
				ExpireAt int64  `json:"expireAt"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if payload.AppID != "app-1" ||
				payload.ExpireAt != expiry.Unix() {
				t.Fatalf("payload = %#v", payload)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(
				`{"data":{"sessionKey":"provider-session-token","expireAt":` +
					jsonNumber(expiry.Unix()) + `}}`,
			))
		},
	))
	defer server.Close()
	configuration := spatiusClientTestConfiguration(t)
	configuration.ConsoleBaseURL = server.URL + "/v1/console"
	client, err := newSpatiusClient(configuration, server.Client())
	if err != nil {
		t.Fatalf("newSpatiusClient() error = %v", err)
	}

	result, err := client.CreateSessionToken(
		t.Context(),
		"app-1",
		expiry,
	)
	if err != nil {
		t.Fatalf("CreateSessionToken() error = %v", err)
	}
	if result.Value != "provider-session-token" ||
		!result.ExpiresAt.Equal(expiry) {
		t.Fatalf("result = %#v", result)
	}
}

func TestSpatiusClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			redirected.Add(1)
		},
	))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Location", target.URL)
			response.WriteHeader(http.StatusTemporaryRedirect)
		},
	))
	defer source.Close()
	configuration := spatiusClientTestConfiguration(t)
	configuration.ConsoleBaseURL = source.URL
	client, err := newSpatiusClient(configuration, source.Client())
	if err != nil {
		t.Fatalf("newSpatiusClient() error = %v", err)
	}

	_, err = client.CreateSessionToken(
		t.Context(),
		"app-1",
		time.Now().Add(5*time.Minute),
	)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target requests = %d", redirected.Load())
	}
}

func TestSpatiusClientRejectsUnsafeProviderResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		expected    error
	}{
		{
			name:        "quota",
			status:      http.StatusTooManyRequests,
			contentType: "application/json",
			body:        `{"error":"secret diagnostics"}`,
			expected:    ErrProviderQuotaExhausted,
		},
		{
			name:        "wrong content type",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        `<p>provider-session-token</p>`,
			expected:    ErrInvalidProviderResponse,
		},
		{
			name:        "missing token",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"data":{}}`,
			expected:    ErrInvalidProviderResponse,
		},
		{
			name:        "oversized",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        strings.Repeat("x", maxSpatiusResponseBody+1),
			expected:    ErrInvalidProviderResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(
				func(response http.ResponseWriter, _ *http.Request) {
					response.Header().Set("Content-Type", test.contentType)
					response.WriteHeader(test.status)
					_, _ = response.Write([]byte(test.body))
				},
			))
			defer server.Close()
			configuration := spatiusClientTestConfiguration(t)
			configuration.ConsoleBaseURL = server.URL
			client, err := newSpatiusClient(
				configuration,
				server.Client(),
			)
			if err != nil {
				t.Fatalf("newSpatiusClient() error = %v", err)
			}
			_, err = client.CreateSessionToken(
				t.Context(),
				"app-1",
				time.Now().Add(5*time.Minute),
			)
			if !errors.Is(err, test.expected) {
				t.Fatalf("error = %v, want %v", err, test.expected)
			}
			if strings.Contains(
				err.Error(),
				"server-only-key",
			) {
				t.Fatal("error exposed the API key")
			}
		})
	}
}

func spatiusClientTestConfiguration(
	t *testing.T,
) config.SpatiusConfig {
	t.Helper()
	t.Setenv("SPATIUS_ENABLED", "true")
	t.Setenv("SPATIUS_REGION", "cn-beijing")
	t.Setenv("SPATIUS_CONSOLE_BASE_URL", "")
	t.Setenv("SPATIUS_APP_ID", "app-1")
	t.Setenv("SPATIUS_AVATAR_ID", "avatar-1")
	t.Setenv("SPATIUS_API_KEY", "server-only-key")
	t.Setenv("SPATIUS_TOKEN_TTL", "10m")
	t.Setenv("SPATIUS_TIMEOUT", "5s")
	configuration, err := config.LoadSpatius()
	if err != nil {
		t.Fatalf("LoadSpatius() error = %v", err)
	}
	return configuration
}

func jsonNumber(value int64) string {
	return strings.TrimSpace(
		string(mustJSONMarshal(value)),
	)
}

func mustJSONMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
