package iserelay

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

type ClientConfig struct {
	Endpoint       string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
	PollInterval   time.Duration
	Observer       providerobservability.Recorder
}

type Client struct {
	endpoint     *url.URL
	httpClient   *http.Client
	pollInterval time.Duration
	observer     providerobservability.Recorder
}

type RelayError struct {
	code      string
	retryable bool
}

func (failure RelayError) Error() string          { return "ISE relay request failed" }
func (failure RelayError) StableCategory() string { return failure.code }
func (failure RelayError) Retryable() bool        { return failure.retryable }

func NewClient(configuration ClientConfig) (*Client, error) {
	endpoint, err := normalizeEndpoint(configuration.Endpoint)
	if err != nil || strings.TrimSpace(configuration.CAFile) == "" ||
		strings.TrimSpace(configuration.ClientCertFile) == "" ||
		strings.TrimSpace(configuration.ClientKeyFile) == "" ||
		configuration.PollInterval < 100*time.Millisecond ||
		configuration.PollInterval > 10*time.Second {
		return nil, errors.New("ISE relay client configuration is invalid")
	}
	caPEM, err := os.ReadFile(configuration.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read ISE relay CA: %w", err)
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("ISE relay CA is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(
		configuration.ClientCertFile,
		configuration.ClientKeyFile,
	)
	if err != nil {
		return nil, fmt.Errorf("load ISE relay client certificate: %w", err)
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: rootCAs, Certificates: []tls.Certificate{certificate}},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Client{
		endpoint:     endpoint,
		httpClient:   &http.Client{Transport: transport},
		pollInterval: configuration.PollInterval,
		observer:     configuration.Observer,
	}, nil
}

func newClientForTest(
	endpoint string,
	httpClient *http.Client,
	pollInterval time.Duration,
) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		httpClient == nil || pollInterval <= 0 {
		return nil, errors.New("ISE relay test client configuration is invalid")
	}
	return &Client{endpoint: parsed, httpClient: httpClient, pollInterval: pollInterval}, nil
}

func (client *Client) Evaluate(
	ctx context.Context,
	request speechfeedback.AcousticAssessmentRequest,
) (speechfeedback.AcousticAssessmentResult, error) {
	if client == nil || client.endpoint == nil || client.httpClient == nil ||
		ctx == nil || !validRequest(request) {
		return speechfeedback.AcousticAssessmentResult{},
			RelayError{code: FailureInvalidRequest, retryable: false}
	}
	startedAt := time.Now()
	response, err := client.create(ctx, request)
	if err == nil {
		response, err = client.poll(ctx, request.RequestID, response)
	}
	client.record(startedAt, err)
	if err != nil {
		return speechfeedback.AcousticAssessmentResult{}, err
	}
	if !response.valid() || response.Status != StatusSucceeded || response.Result == nil {
		return speechfeedback.AcousticAssessmentResult{},
			RelayError{code: FailureProviderUnavailable, retryable: true}
	}
	return speechfeedback.AcousticAssessmentResult{
		Provider:  response.Result.Provider,
		SessionID: response.Result.SessionID,
		Summary: speechfeedback.AcousticAssessmentSummary{
			AccuracyScore:  response.Result.Summary.AccuracyScore,
			FluencyScore:   response.Result.Summary.FluencyScore,
			IntegrityScore: response.Result.Summary.IntegrityScore,
			PhoneScore:     response.Result.Summary.PhoneScore,
			SpeakingSpeed:  response.Result.Summary.SpeakingSpeed,
			Rejected:       response.Result.Summary.Rejected,
			ExceptionInfo:  response.Result.Summary.ExceptionInfo,
		},
	}, nil
}

func (client *Client) create(
	ctx context.Context,
	assessment speechfeedback.AcousticAssessmentRequest,
) (StatusResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := []struct {
		name  string
		value string
	}{
		{name: "request_id", value: assessment.RequestID},
		{name: "reference_text", value: assessment.ReferenceText},
		{name: "topic_title", value: assessment.TopicTitle},
		{name: "category", value: string(assessment.Category)},
	}
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return StatusResponse{}, err
		}
	}
	audio, err := writer.CreateFormFile("audio", "audio.pcm")
	if err != nil {
		return StatusResponse{}, err
	}
	if _, err := audio.Write(assessment.Audio); err != nil {
		return StatusResponse{}, err
	}
	if err := writer.Close(); err != nil {
		return StatusResponse{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.resolve("/v1/evaluations"),
		&body,
	)
	if err != nil {
		return StatusResponse{}, err
	}
	request.Header.Set("content-type", writer.FormDataContentType())
	return client.do(request, http.StatusAccepted, http.StatusOK)
}

func (client *Client) poll(
	ctx context.Context,
	requestID string,
	response StatusResponse,
) (StatusResponse, error) {
	if response.RequestID != requestID {
		return StatusResponse{}, relayResponseMismatch()
	}
	for response.Status == StatusProcessing {
		timer := time.NewTimer(client.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return StatusResponse{}, ctx.Err()
		case <-timer.C:
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			client.resolve("/v1/evaluations/"+url.PathEscape(requestID)),
			nil,
		)
		if err != nil {
			return StatusResponse{}, err
		}
		response, err = client.do(request, http.StatusOK, http.StatusAccepted)
		if err != nil {
			return StatusResponse{}, err
		}
		if response.RequestID != requestID {
			return StatusResponse{}, relayResponseMismatch()
		}
	}
	if response.Status == StatusFailed && response.Failure != nil &&
		response.Failure.valid() {
		return StatusResponse{}, RelayError{
			code:      response.Failure.Code,
			retryable: response.Failure.Retryable,
		}
	}
	return response, nil
}

func (client *Client) do(
	request *http.Request,
	allowed ...int,
) (StatusResponse, error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return StatusResponse{}, err
	}
	defer response.Body.Close()
	accepted := false
	for _, status := range allowed {
		if response.StatusCode == status {
			accepted = true
			break
		}
	}
	if !accepted {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return StatusResponse{}, RelayError{
			code: FailureProviderUnavailable,
			retryable: response.StatusCode >= 500 ||
				(request.Method == http.MethodGet && response.StatusCode == http.StatusNotFound),
		}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if readErr != nil || len(body) > 64*1024 {
		return StatusResponse{}, RelayError{
			code:      FailureProviderUnavailable,
			retryable: true,
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var decoded StatusResponse
	if err := decoder.Decode(&decoded); err != nil || !decoded.valid() {
		return StatusResponse{}, RelayError{
			code:      FailureProviderUnavailable,
			retryable: true,
		}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return StatusResponse{}, RelayError{
			code:      FailureProviderUnavailable,
			retryable: true,
		}
	}
	return decoded, nil
}

func relayResponseMismatch() RelayError {
	return RelayError{code: FailureProviderUnavailable, retryable: true}
}

func (client *Client) resolve(relative string) string {
	resolved := *client.endpoint
	resolved.Path = path.Join(client.endpoint.Path, relative)
	return resolved.String()
}

func (client *Client) record(startedAt time.Time, err error) {
	if client.observer == nil {
		return
	}
	kind := providerobservability.ErrorNone
	if err != nil {
		kind = providerobservability.ErrorProviderUnavailable
		if errors.Is(err, context.DeadlineExceeded) {
			kind = providerobservability.ErrorTimeout
		}
		var relayFailure RelayError
		if errors.As(err, &relayFailure) {
			switch relayFailure.code {
			case FailureInvalidRequest:
				kind = providerobservability.ErrorInvalidRequest
			case FailureProcessingTimeout:
				kind = providerobservability.ErrorTimeout
			}
		}
	}
	client.observer.Record(providerobservability.Observation{
		Provider:   providerobservability.ProviderXFYunISE,
		Capability: providerobservability.CapabilitySpeechEvaluation,
		Duration:   time.Since(startedAt),
		ErrorKind:  kind,
	})
}

func normalizeEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("ISE relay endpoint must be an HTTPS origin")
	}
	parsed.Path = ""
	return parsed, nil
}

var _ speechfeedback.AcousticEvaluator = (*Client)(nil)
