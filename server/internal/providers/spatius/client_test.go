package spatius

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

	"github.com/1024XEngineer/XE3-ESL/server/internal/avatar"
)

func TestClientSendsServerCredentialAndParsesResponse(t *testing.T) {
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
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if len(payload) != 1 {
				t.Fatalf("payload = %#v", payload)
			}
			var expireAt int64
			if err := json.Unmarshal(payload["expireAt"], &expireAt); err != nil {
				t.Fatalf("decode expireAt: %v", err)
			}
			if expireAt != expiry.Unix() {
				t.Fatalf("expireAt = %d", expireAt)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(
				`{"sessionToken":"provider-session-token"}`,
			))
		},
	))
	defer server.Close()
	configuration := clientTestConfiguration(server.URL + "/v1/console")
	client, err := newClient(configuration, server.Client())
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
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

func TestClientRequiresHTTPSProviderEndpoint(t *testing.T) {
	_, err := NewClient(clientTestConfiguration(
		"http://console.example.test/v1/console",
	))
	if err == nil {
		t.Fatal("NewClient() error = nil")
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
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
	client, err := newClient(
		clientTestConfiguration(source.URL),
		source.Client(),
	)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	_, err = client.CreateSessionToken(
		t.Context(),
		"app-1",
		time.Now().Add(5*time.Minute),
	)
	if !errors.Is(err, avatar.ErrProviderUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target requests = %d", redirected.Load())
	}
}

func TestClientRejectsUnsafeProviderResponses(t *testing.T) {
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
			expected:    avatar.ErrProviderQuotaExhausted,
		},
		{
			name:        "wrong content type",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        `<p>provider-session-token</p>`,
			expected:    avatar.ErrInvalidProviderResponse,
		},
		{
			name:        "missing token",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"data":{}}`,
			expected:    avatar.ErrInvalidProviderResponse,
		},
		{
			name:        "provider error",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{"errors":[{"status":400,` +
				`"detail":"invalid api key"}]}`,
			expected: avatar.ErrProviderUnavailable,
		},
		{
			name:        "provider quota error",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{"errors":[{"status":429,` +
				`"detail":"secret diagnostics"}]}`,
			expected: avatar.ErrProviderQuotaExhausted,
		},
		{
			name:        "oversized",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        strings.Repeat("x", maxResponseBody+1),
			expected:    avatar.ErrInvalidProviderResponse,
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
			client, err := newClient(
				clientTestConfiguration(server.URL),
				server.Client(),
			)
			if err != nil {
				t.Fatalf("newClient() error = %v", err)
			}
			_, err = client.CreateSessionToken(
				t.Context(),
				"app-1",
				time.Now().Add(5*time.Minute),
			)
			if !errors.Is(err, test.expected) {
				t.Fatalf("error = %v, want %v", err, test.expected)
			}
			if strings.Contains(err.Error(), "server-only-key") {
				t.Fatal("error exposed the API key")
			}
		})
	}
}

func clientTestConfiguration(consoleBaseURL string) Config {
	return Config{
		Enabled:        true,
		ConsoleBaseURL: consoleBaseURL,
		APIKey:         "server-only-key",
		Timeout:        5 * time.Second,
	}
}
