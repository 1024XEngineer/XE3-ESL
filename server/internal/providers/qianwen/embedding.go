package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

const embeddingsPath = "/embeddings"

type EmbeddingConfig struct {
	BaseURL    string
	Model      string
	Dimensions int
	Timeout    time.Duration
}

type embeddingClient struct {
	endpoint   string
	model      string
	dimensions int
	timeout    time.Duration
	apiKey     providerSecret
	client     httpDoer
}

func newEmbeddingClient(
	config EmbeddingConfig,
	apiKey string,
) (*embeddingClient, error) {
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newEmbeddingClientWithHTTP(config, apiKey, client)
}

func newEmbeddingClientWithHTTP(
	config EmbeddingConfig,
	apiKey string,
	client httpDoer,
) (*embeddingClient, error) {
	baseURL, err := normalizeBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model, err := normalizeEmbeddingModel(config.Model)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	if config.Dimensions < 64 ||
		config.Dimensions > protocol.MaxEmbeddingDimensions {
		return nil, fmt.Errorf(
			"Qianwen embedding dimensions must be between 64 and %d",
			protocol.MaxEmbeddingDimensions,
		)
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf(
			"Qianwen embedding timeout must be greater than zero and at most %s",
			maxTimeout,
		)
	}
	if client == nil {
		return nil, errors.New("Qianwen embedding HTTP client is required")
	}
	return &embeddingClient{
		endpoint:   baseURL + embeddingsPath,
		model:      model,
		dimensions: config.Dimensions,
		timeout:    config.Timeout,
		apiKey:     newProviderSecret(apiKey),
		client:     client,
	}, nil
}

func (client *embeddingClient) String() string {
	if client == nil {
		return "QianwenEmbeddingClient(<nil>)"
	}
	return fmt.Sprintf(
		"QianwenEmbeddingClient(model=%q, dimensions=%d, timeout=%s, api_key=[REDACTED])",
		client.model,
		client.dimensions,
		client.timeout,
	)
}

func (client *embeddingClient) GoString() string {
	return client.String()
}

func (client *embeddingClient) Embed(
	ctx context.Context,
	request protocol.EmbeddingRequest,
) (protocol.EmbeddingResult, error) {
	if ctx == nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidRequest, 0, "", "",
			errors.New("embedding context is required"),
		)
	}
	if err := protocol.ValidateEmbeddingRequest(request); err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	if request.Dimensions != client.dimensions {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidRequest, 0, "", "",
			errors.New("embedding request dimension does not match configuration"),
		)
	}
	body, err := json.Marshal(embeddingRequest{
		Model:          client.model,
		Input:          request.Inputs,
		Dimensions:     client.dimensions,
		EncodingFormat: "float",
	})
	if err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidRequest, 0, "", "", err,
		)
	}
	callContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		callContext,
		http.MethodPost,
		client.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorConfiguration, 0, "", "", err,
		)
	}
	httpRequest.Header.Set(
		authorizationHeaderName,
		"Bearer "+client.apiKey.reveal(),
	)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := client.client.Do(httpRequest)
	if err != nil {
		return protocol.EmbeddingResult{}, embeddingTransportError(callContext, err)
	}
	if response == nil || response.Body == nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidResponse, 0, "", "",
			errors.New("Qianwen returned an incomplete embedding response"),
		)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		generationErr := decodeStatusError(response)
		var providerErr *protocol.GenerationError
		if errors.As(generationErr, &providerErr) {
			return protocol.EmbeddingResult{}, embeddingError(
				providerErr.Kind,
				providerErr.StatusCode,
				providerErr.ProviderCode,
				providerErr.RequestID,
				providerErr,
			)
		}
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			"",
			generationErr,
		)
	}
	responseBody, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	var decoded embeddingResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			errors.New("decode Qianwen embedding response"),
		)
	}
	result, err := decoded.result(request, client.model, client.dimensions)
	if err != nil {
		return protocol.EmbeddingResult{}, embeddingError(
			protocol.ErrorInvalidResponse,
			response.StatusCode,
			"",
			sanitizeIdentifier(response.Header.Get("X-Request-Id")),
			err,
		)
	}
	return result, nil
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions"`
	EncodingFormat string   `json:"encoding_format"`
}

type embeddingResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage *struct {
		PromptTokens *int `json:"prompt_tokens"`
		TotalTokens  *int `json:"total_tokens"`
	} `json:"usage"`
}

func (response embeddingResponse) result(
	request protocol.EmbeddingRequest,
	model string,
	dimensions int,
) (protocol.EmbeddingResult, error) {
	if response.Model != model ||
		response.Usage == nil ||
		response.Usage.PromptTokens == nil ||
		response.Usage.TotalTokens == nil ||
		*response.Usage.PromptTokens < 0 ||
		*response.Usage.TotalTokens < *response.Usage.PromptTokens ||
		len(response.Data) != len(request.Inputs) {
		return protocol.EmbeddingResult{}, errors.New(
			"Qianwen embedding response metadata is invalid",
		)
	}
	vectors := make([][]float32, len(request.Inputs))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(vectors) ||
			vectors[item.Index] != nil {
			return protocol.EmbeddingResult{}, errors.New(
				"Qianwen embedding response index is invalid",
			)
		}
		vectors[item.Index] = item.Embedding
	}
	result := protocol.EmbeddingResult{
		Provider:    providerName,
		Model:       model,
		Dimensions:  dimensions,
		Vectors:     vectors,
		InputTokens: *response.Usage.PromptTokens,
		TotalTokens: *response.Usage.TotalTokens,
	}
	if err := protocol.ValidateEmbeddingResult(request, result); err != nil {
		return protocol.EmbeddingResult{}, err
	}
	return result, nil
}

func normalizeEmbeddingModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if len(model) == 0 || len(model) > maxProviderIdentifier ||
		!strings.HasPrefix(strings.ToLower(model), "text-embedding-") {
		return "", errors.New(
			"Qianwen embedding adapter only accepts text-embedding model IDs",
		)
	}
	for _, value := range model {
		if !((value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') ||
			value == '-' || value == '_' || value == '.') {
			return "", errors.New(
				"Qianwen embedding model contains unsupported characters",
			)
		}
	}
	return model, nil
}

func embeddingTransportError(ctx context.Context, cause error) error {
	generationErr := transportError(ctx, cause)
	var providerErr *protocol.GenerationError
	if errors.As(generationErr, &providerErr) {
		return embeddingError(
			providerErr.Kind,
			providerErr.StatusCode,
			providerErr.ProviderCode,
			providerErr.RequestID,
			providerErr,
		)
	}
	return embeddingError(protocol.ErrorProviderUnavailable, 0, "", "", cause)
}

func embeddingError(
	kind protocol.ErrorKind,
	status int,
	code string,
	requestID string,
	cause error,
) error {
	return protocol.NewEmbeddingError(kind, status, code, requestID, cause)
}

var _ protocol.Embedder = (*embeddingClient)(nil)
