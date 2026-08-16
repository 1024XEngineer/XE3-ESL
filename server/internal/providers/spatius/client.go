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
)

const maxResponseBody = 64 * 1024

type Config struct {
	Enabled        bool
	ConsoleBaseURL string
	APIKey         string
	Timeout        time.Duration
}

type Client struct {
	endpoint string
	apiKey   string
	client   *http.Client
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
		apiKey: configuration.APIKey,
		client: client,
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

	response, err := client.client.Do(request)
	if err != nil {
		return avatar.ProviderSessionToken{}, fmt.Errorf(
			"create avatar provider session token: %w",
			avatar.ErrProviderUnavailable,
		)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusPaymentRequired:
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrProviderQuotaExhausted
	case response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices:
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrProviderUnavailable
	}
	if !jsonContentType(response.Header.Get("Content-Type")) {
		discardResponse(response.Body)
		return avatar.ProviderSessionToken{}, avatar.ErrInvalidProviderResponse
	}
	body, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxResponseBody+1,
	))
	if err != nil || len(body) == 0 || len(body) > maxResponseBody {
		return avatar.ProviderSessionToken{}, avatar.ErrInvalidProviderResponse
	}
	token, err := decodeSessionToken(body)
	if err != nil {
		return avatar.ProviderSessionToken{}, err
	}
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

func decodeSessionToken(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var document sessionTokenResponse
	if err := decoder.Decode(&document); err != nil {
		return "", avatar.ErrInvalidProviderResponse
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return "", avatar.ErrInvalidProviderResponse
	}
	for _, providerError := range document.Errors {
		if providerError.Status == http.StatusPaymentRequired ||
			providerError.Status == http.StatusTooManyRequests {
			return "", avatar.ErrProviderQuotaExhausted
		}
	}
	if len(document.Errors) > 0 {
		return "", avatar.ErrProviderUnavailable
	}
	if !validSessionToken(document.SessionToken) {
		return "", avatar.ErrInvalidProviderResponse
	}
	return document.SessionToken, nil
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
