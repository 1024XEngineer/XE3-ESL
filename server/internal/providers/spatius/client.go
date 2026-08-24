package spatius

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/avatar"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

const maxResponseBody = 64 * 1024

type Config struct {
	Enabled        bool
	ConsoleBaseURL string
	APIKey         string
	Timeout        time.Duration
	Observer       providerobservability.Recorder
}

type Client struct {
	endpoint string
	apiKey   string
	client   *http.Client
	observer providerobservability.Recorder
}

var _ avatar.TokenProvider = (*Client)(nil)

func NewClient(configuration Config) (*Client, error) {
	return newClient(configuration, nil)
}

func newClient(
	configuration Config,
	client *http.Client,
) (*Client, error) {
	if !configuration.Enabled {
		return nil, errors.New("spatius: provider is disabled")
	}
	if !validConsoleBaseURL(configuration.ConsoleBaseURL) ||
		strings.TrimSpace(configuration.APIKey) == "" ||
		configuration.Timeout <= 0 {
		return nil, errors.New("spatius: client configuration is required")
	}
	if client == nil {
		client = &http.Client{}
	}
	client.Timeout = configuration.Timeout
	client.CheckRedirect = func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		endpoint: strings.TrimSuffix(
			configuration.ConsoleBaseURL,
			"/",
		) + "/session-tokens",
		apiKey:   configuration.APIKey,
		client:   client,
		observer: configuration.Observer,
	}, nil
}

func (client *Client) CreateSessionToken(
	ctx context.Context,
	appID string,
	expiresAt time.Time,
) (avatar.ProviderSessionToken, error) {
	if client == nil || client.client == nil || ctx == nil ||
		strings.TrimSpace(appID) == "" ||
		!expiresAt.After(time.Now().UTC()) {
		return avatar.ProviderSessionToken{}, avatar.ErrProviderUnavailable
	}
	payload, err := json.Marshal(sessionTokenRequest{
		ExpireAt: expiresAt.UTC().Unix(),
	})
	if err != nil {
		return avatar.ProviderSessionToken{}, avatar.ErrProviderUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return avatar.ProviderSessionToken{}, avatar.ErrProviderUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", client.apiKey)

	startedAt := time.Now()
	metricKind := providerobservability.ErrorProviderUnavailable
	defer func() {
		recordSessionTokenCall(client.observer, startedAt, metricKind)
	}()
	response, err := client.client.Do(request)
	if err != nil {
		metricKind = sessionTokenTransportKind(ctx, err)
		return avatar.ProviderSessionToken{}, fmt.Errorf(
			"create avatar provider session token: %w",
			avatar.ErrProviderUnavailable,
		)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusPaymentRequired:
		metricKind = sessionTokenStatusKind(response.StatusCode)
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrProviderQuotaExhausted
	case response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices:
		metricKind = sessionTokenStatusKind(response.StatusCode)
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrProviderUnavailable
	}
	if !jsonContentType(response.Header.Get("Content-Type")) {
		metricKind = providerobservability.ErrorInvalidResponse
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrInvalidProviderResponse
	}
	body, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxResponseBody+1,
	))
	if err != nil {
		metricKind = sessionTokenTransportKind(ctx, err)
		return avatar.ProviderSessionToken{}, avatar.ErrInvalidProviderResponse
	}
	if len(body) == 0 || len(body) > maxResponseBody {
		metricKind = providerobservability.ErrorInvalidResponse
		return avatar.ProviderSessionToken{}, avatar.ErrInvalidProviderResponse
	}
	token, tokenKind, err := decodeSessionToken(body)
	metricKind = tokenKind
	if err != nil {
		return avatar.ProviderSessionToken{}, err
	}
	metricKind = providerobservability.ErrorNone
	return avatar.ProviderSessionToken{
		Value:     token,
		ExpiresAt: expiresAt.UTC().Truncate(time.Second),
	}, nil
}

type sessionTokenRequest struct {
	ExpireAt int64 `json:"expireAt"`
}

type sessionTokenResponse struct {
	SessionToken string          `json:"sessionToken"`
	Errors       []providerError `json:"errors"`
}

type providerError struct {
	Status int `json:"status"`
}

func decodeSessionToken(
	body []byte,
) (string, providerobservability.ErrorKind, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var document sessionTokenResponse
	if err := decoder.Decode(&document); err != nil {
		return "", providerobservability.ErrorInvalidResponse, avatar.ErrInvalidProviderResponse
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return "", providerobservability.ErrorInvalidResponse, avatar.ErrInvalidProviderResponse
	}
	for _, providerError := range document.Errors {
		if providerError.Status == http.StatusPaymentRequired ||
			providerError.Status == http.StatusTooManyRequests {
			return "", sessionTokenStatusKind(providerError.Status), avatar.ErrProviderQuotaExhausted
		}
	}
	if len(document.Errors) > 0 {
		return "", sessionTokenStatusKind(document.Errors[0].Status), avatar.ErrProviderUnavailable
	}
	if !validSessionToken(document.SessionToken) {
		return "", providerobservability.ErrorInvalidResponse, avatar.ErrInvalidProviderResponse
	}
	return document.SessionToken, providerobservability.ErrorNone, nil
}

func validConsoleBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil &&
		parsed.Scheme == "https" &&
		parsed.Host != "" &&
		parsed.User == nil &&
		parsed.RawQuery == "" &&
		parsed.Fragment == "" &&
		parsed.Opaque == "" &&
		parsed.String() == raw
}

func validSessionToken(value string) bool {
	if len(value) < 8 || len(value) > 8192 {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return character <= 0x20 || character > 0x7e
	}) < 0
}

func jsonContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func discardResponse(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBody))
}
