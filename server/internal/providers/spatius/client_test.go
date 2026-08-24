package spatius

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestClientRecordsValidatedSessionTokenCall(t *testing.T) {
	recorder := &sessionTokenRecorder{}
	client := newObservedClient(t, recorder, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/session-tokens" ||
			request.Header.Get("X-Api-Key") != "test-api-key" {
			t.Fatalf("request = %s %s %#v", request.Method, request.URL, request.Header)
		}
		return httpSessionTokenResponse(http.StatusOK, `{"sessionToken":"session-token"}`), nil
	}))
	result, err := client.CreateSessionToken(
		context.Background(), "app-id", time.Now().UTC().Add(time.Minute),
	)
	if err != nil || result.Value != "session-token" {
		t.Fatalf("CreateSessionToken() = %#v, %v", result, err)
	}
	observation := onlySessionTokenObservation(t, recorder)
	if observation.Provider != providerobservability.ProviderSpatius ||
		observation.Capability != providerobservability.CapabilityAvatarSessionToken ||
		observation.ErrorKind != providerobservability.ErrorNone ||
		observation.Usage != (providerobservability.Usage{}) {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestClientMapsBoundedHTTPAndDocumentErrors(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		body   string
		want   providerobservability.ErrorKind
	}{
		"bad request":    {status: 400, want: providerobservability.ErrorInvalidRequest},
		"authentication": {status: 401, want: providerobservability.ErrorAuthentication},
		"quota":          {status: 402, want: providerobservability.ErrorQuotaExhausted},
		"authorization":  {status: 403, want: providerobservability.ErrorAuthorization},
		"rate limited":   {status: 429, want: providerobservability.ErrorRateLimited},
		"unavailable":    {status: 503, want: providerobservability.ErrorProviderUnavailable},
		"document error": {
			status: 200, body: `{"sessionToken":"","errors":[{"status":403}]}`,
			want: providerobservability.ErrorAuthorization,
		},
		"invalid document": {
			status: 200, body: `{"sessionToken":"short"}`,
			want: providerobservability.ErrorInvalidResponse,
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &sessionTokenRecorder{}
			client := newObservedClient(t, recorder, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return httpSessionTokenResponse(test.status, test.body), nil
			}))
			_, _ = client.CreateSessionToken(
				context.Background(), "app-id", time.Now().UTC().Add(time.Minute),
			)
			if observation := onlySessionTokenObservation(t, recorder); observation.ErrorKind != test.want {
				t.Fatalf("observation = %#v", observation)
			}
		})
	}
}

func TestClientRecordsBoundedTransportOutcome(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		want providerobservability.ErrorKind
	}{
		"cancelled": {err: context.Canceled, want: providerobservability.ErrorCancelled},
		"timeout":   {err: context.DeadlineExceeded, want: providerobservability.ErrorTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &sessionTokenRecorder{}
			client := newObservedClient(t, recorder, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, test.err
			}))
			_, _ = client.CreateSessionToken(
				context.Background(), "app-id", time.Now().UTC().Add(time.Minute),
			)
			if observation := onlySessionTokenObservation(t, recorder); observation.ErrorKind != test.want {
				t.Fatalf("observation = %#v", observation)
			}
		})
	}
}

func TestClientSkipsInvalidInputBeforeExternalCall(t *testing.T) {
	recorder := &sessionTokenRecorder{}
	calls := 0
	client := newObservedClient(t, recorder, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.Canceled
	}))
	_, _ = client.CreateSessionToken(
		context.Background(), "", time.Now().UTC().Add(time.Minute),
	)
	if calls != 0 || len(recorder.observations) != 0 {
		t.Fatalf("calls = %d observations = %#v", calls, recorder.observations)
	}
}

func newObservedClient(
	t *testing.T,
	recorder providerobservability.Recorder,
	transport http.RoundTripper,
) *Client {
	t.Helper()
	client, err := newClient(Config{
		Enabled: true, ConsoleBaseURL: "https://console.spatius.example",
		APIKey: "test-api-key", Timeout: time.Second, Observer: recorder,
	}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return client
}

func httpSessionTokenResponse(status int, body string) *http.Response {
	if body == "" {
		body = `{}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type sessionTokenRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *sessionTokenRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

func onlySessionTokenObservation(
	t *testing.T,
	recorder *sessionTokenRecorder,
) providerobservability.Observation {
	t.Helper()
	if len(recorder.observations) != 1 {
		t.Fatalf("observations = %#v", recorder.observations)
	}
	return recorder.observations[0]
}
