package iserelay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func TestNewClientLoadsMTLSConfiguration(t *testing.T) {
	directory := t.TempDir()
	certificateFile, keyFile := writeClientCertificate(t, directory)
	client, err := NewClient(ClientConfig{
		Endpoint:       "https://relay.example.test/",
		CAFile:         certificateFile,
		ClientCertFile: certificateFile,
		ClientKeyFile:  keyFile,
		PollInterval:   time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil ||
		transport.DialContext == nil ||
		transport.TLSClientConfig.MinVersion != tls.VersionTLS13 ||
		transport.TLSHandshakeTimeout != 15*time.Second ||
		len(transport.TLSClientConfig.Certificates) != 1 ||
		len(transport.TLSClientConfig.RootCAs.Subjects()) != 1 {
		t.Fatalf("unexpected mTLS transport: %#v", client.httpClient.Transport)
	}
	if client.endpoint.String() != "https://relay.example.test" ||
		client.pollInterval != time.Second {
		t.Fatalf("unexpected relay client: %#v", client)
	}
}

func TestNewClientRejectsInvalidTLSConfiguration(t *testing.T) {
	directory := t.TempDir()
	invalidCAFile := filepath.Join(directory, "invalid-ca.pem")
	if err := os.WriteFile(invalidCAFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid CA: %v", err)
	}
	tests := []struct {
		name          string
		configuration ClientConfig
	}{
		{
			name: "HTTP endpoint",
			configuration: ClientConfig{
				Endpoint:       "http://relay.example.test",
				CAFile:         invalidCAFile,
				ClientCertFile: "client.pem",
				ClientKeyFile:  "client-key.pem",
				PollInterval:   time.Second,
			},
		},
		{
			name: "missing CA file",
			configuration: ClientConfig{
				Endpoint:       "https://relay.example.test",
				CAFile:         filepath.Join(directory, "missing.pem"),
				ClientCertFile: "client.pem",
				ClientKeyFile:  "client-key.pem",
				PollInterval:   time.Second,
			},
		},
		{
			name: "invalid CA file",
			configuration: ClientConfig{
				Endpoint:       "https://relay.example.test",
				CAFile:         invalidCAFile,
				ClientCertFile: "client.pem",
				ClientKeyFile:  "client-key.pem",
				PollInterval:   time.Second,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewClient(test.configuration); err == nil {
				t.Fatal("expected invalid mTLS configuration to be rejected")
			}
		})
	}
}

func writeClientCertificate(t *testing.T, directory string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "relay-client"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	certificateFile := filepath.Join(directory, "client.pem")
	keyFile := filepath.Join(directory, "client-key.pem")
	if err := os.WriteFile(certificateFile, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: der,
	}), 0o600); err != nil {
		t.Fatalf("write client certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{
		Type: "PRIVATE KEY", Bytes: keyDER,
	}), 0o600); err != nil {
		t.Fatalf("write client key: %v", err)
	}
	return certificateFile, keyFile
}

func TestClientCreatesAndPollsEvaluation(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			if err := request.ParseMultipartForm(1024); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
			return
		}
		polls.Add(1)
		result := resultResponse(testRequestID, testResult())
		writeJSON(writer, http.StatusOK, result)
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Evaluate(t.Context(), validAssessment())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if polls.Load() != 1 || result.Provider != "xfyun_ise" || result.RawResult != "" {
		t.Fatalf("unexpected relay result: %#v, polls=%d", result, polls.Load())
	}
}

func TestClientRetriesLostCreateResponseWithIdenticalRequest(t *testing.T) {
	var requestBodies [][]byte
	var contentTypes []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			return clientResponse(
				t,
				request,
				http.StatusOK,
				resultResponse(testRequestID, testResult()),
			), nil
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read create request: %v", err)
		}
		requestBodies = append(requestBodies, body)
		contentTypes = append(contentTypes, request.Header.Get("content-type"))
		if len(requestBodies) == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return clientResponse(
			t,
			request,
			http.StatusAccepted,
			processingResponse(testRequestID),
		), nil
	})
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err != nil {
		t.Fatalf("evaluate after lost create response: %v", err)
	}
	if len(requestBodies) != 2 || !bytes.Equal(requestBodies[0], requestBodies[1]) ||
		contentTypes[0] != contentTypes[1] {
		t.Fatalf("create retry changed request: bodies=%d content-types=%v", len(requestBodies), contentTypes)
	}
	_, parameters, err := mime.ParseMediaType(contentTypes[0])
	if err != nil {
		t.Fatalf("parse multipart content type: %v", err)
	}
	form, err := multipart.NewReader(
		bytes.NewReader(requestBodies[0]),
		parameters["boundary"],
	).ReadForm(int64(len(requestBodies[0])))
	if err != nil {
		t.Fatalf("parse create request: %v", err)
	}
	defer form.RemoveAll()
	if values := form.Value["request_id"]; len(values) != 1 || values[0] != testRequestID {
		t.Fatalf("create retry request_id = %v", values)
	}
}

func TestClientRetriesTransientPollFailure(t *testing.T) {
	var polls int
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return clientResponse(
				t,
				request,
				http.StatusAccepted,
				processingResponse(testRequestID),
			), nil
		}
		polls++
		if polls == 1 {
			return nil, io.EOF
		}
		return clientResponse(
			t,
			request,
			http.StatusOK,
			resultResponse(testRequestID, testResult()),
		), nil
	})
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err != nil {
		t.Fatalf("evaluate after transient poll failure: %v", err)
	}
	if polls != 2 {
		t.Fatalf("poll attempts = %d, want 2", polls)
	}
}

func TestClientDoesNotRetryConflictResponse(t *testing.T) {
	var attempts int
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		return clientResponse(
			t,
			request,
			http.StatusConflict,
			map[string]string{"error": "IDEMPOTENCY_CONFLICT"},
		), nil
	})
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || failure.Retryable() || attempts != 1 {
		t.Fatalf("conflict response: error=%#v attempts=%d", err, attempts)
	}
}

func TestClientNormalizesExhaustedTransportRetries(t *testing.T) {
	var attempts int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, io.EOF
	})
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || failure.StableCategory() != FailureProviderUnavailable ||
		!failure.Retryable() || attempts != requestMaxAttempts {
		t.Fatalf("exhausted retries: error=%#v attempts=%d", err, attempts)
	}
}

func TestClientRetriesBoundedDialTimeouts(t *testing.T) {
	var dials int
	transport := &http.Transport{
		DialContext: func(
			context.Context,
			string,
			string,
		) (net.Conn, error) {
			dials++
			return nil, &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: syscall.ETIMEDOUT,
			}
		},
	}
	defer transport.CloseIdleConnections()
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || failure.StableCategory() != FailureProviderUnavailable ||
		!failure.Retryable() || dials != requestMaxAttempts {
		t.Fatalf("dial timeout retries: error=%#v dials=%d", err, dials)
	}
}

func TestClientStopsRetryingWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &relayProviderRecorder{}
	var attempts int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		cancel()
		return nil, io.EOF
	})
	client, err := newClientForTest(
		"http://relay.example.test",
		&http.Client{Transport: transport},
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.observer = recorder
	_, err = client.Evaluate(ctx, validAssessment())
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("canceled request: error=%v attempts=%d", err, attempts)
	}
	if len(recorder.observations) != 1 ||
		recorder.observations[0].ErrorKind != providerobservability.ErrorCancelled {
		t.Fatalf("canceled observation: %#v", recorder.observations)
	}
}

type relayProviderRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *relayProviderRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return roundTrip(request)
}

func clientResponse(
	t *testing.T,
	request *http.Request,
	status int,
	value any,
) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal relay response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"content-type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

func testResult() speechfeedback.AcousticAssessmentResult {
	return speechfeedback.AcousticAssessmentResult{
		Provider:  "xfyun_ise",
		SessionID: "session-1",
	}
}

func TestClientReturnsStableRelayFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response := failureResponse(testRequestID, FailureProcessingTimeout, true)
		writeJSON(writer, http.StatusOK, response)
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || failure.StableCategory() != FailureProcessingTimeout || !failure.Retryable() {
		t.Fatalf("unexpected relay error: %#v", err)
	}
}

func TestClientRejectsMalformedSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"status": StatusSucceeded})
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
		t.Fatal("malformed response must fail closed")
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write(append(
			[]byte(`{"schema_version":"ise-relay/v1","request_id":"`+testRequestID+`","status":"PROCESSING"}`),
			bytes.Repeat([]byte(" "), 64*1024)...,
		))
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
		t.Fatal("oversized response must fail closed")
	}
}

func TestClientRejectsMismatchedResponseRequestID(t *testing.T) {
	otherRequestID := "f7023b34-0000-4000-8000-000000000099"
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "create response",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writeJSON(writer, http.StatusAccepted, processingResponse(otherRequestID))
			},
		},
		{
			name: "poll response",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
					return
				}
				writeJSON(writer, http.StatusOK, resultResponse(otherRequestID, testResult()))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			if _, err := client.Evaluate(t.Context(), validAssessment()); err == nil {
				t.Fatal("mismatched request ID must fail closed")
			}
		})
	}
}

func TestClientTreatsMissingPolledJobAsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			writeJSON(writer, http.StatusAccepted, processingResponse(testRequestID))
			return
		}
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "NOT_FOUND"})
	}))
	defer server.Close()
	client, err := newClientForTest(server.URL, server.Client(), time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Evaluate(t.Context(), validAssessment())
	failure, ok := err.(RelayError)
	if !ok || !failure.Retryable() {
		t.Fatalf("missing polled job must be retryable: %#v", err)
	}
}
