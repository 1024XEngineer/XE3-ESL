package avatar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

const maxSpatiusResponseBody = 64 * 1024

var (
	ErrProviderUnavailable     = errors.New("avatar provider unavailable")
	ErrProviderQuotaExhausted  = errors.New("avatar provider quota exhausted")
	ErrInvalidProviderResponse = errors.New("invalid avatar provider response")
)

type SpatiusClient struct {
	endpoint string
	apiKey   config.Secret
	client   *http.Client
}

func NewSpatiusClient(
	configuration config.SpatiusConfig,
) (*SpatiusClient, error) {
	return newSpatiusClient(configuration, nil)
}

func newSpatiusClient(
	configuration config.SpatiusConfig,
	client *http.Client,
) (*SpatiusClient, error) {
	if !configuration.Enabled {
		return nil, errors.New("avatar: Spatius provider is disabled")
	}
	if strings.TrimSpace(configuration.ConsoleBaseURL) == "" ||
		strings.TrimSpace(configuration.AppID) == "" ||
		configuration.APIKey.Reveal() == "" ||
		configuration.Timeout <= 0 {
		return nil, errors.New("avatar: Spatius client configuration is required")
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
	return &SpatiusClient{
		endpoint: strings.TrimSuffix(
			configuration.ConsoleBaseURL,
			"/",
		) + "/session-tokens",
		apiKey: configuration.APIKey,
		client: client,
	}, nil
}

func (client *SpatiusClient) CreateSessionToken(
	ctx context.Context,
	appID string,
	expiresAt time.Time,
) (ProviderSessionToken, error) {
	if client == nil || client.client == nil || ctx == nil ||
		strings.TrimSpace(appID) == "" ||
		!expiresAt.After(time.Now().UTC()) {
		return ProviderSessionToken{}, ErrProviderUnavailable
	}
	payload, err := json.Marshal(struct {
		ExpireAt int64 `json:"expireAt"`
	}{
		ExpireAt: expiresAt.UTC().Unix(),
	})
	if err != nil {
		return ProviderSessionToken{}, ErrProviderUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return ProviderSessionToken{}, ErrProviderUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", client.apiKey.Reveal())

	response, err := client.client.Do(request)
	if err != nil {
		return ProviderSessionToken{}, fmt.Errorf(
			"create avatar provider session token: %w",
			ErrProviderUnavailable,
		)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusPaymentRequired:
		discardSpatiusResponse(response.Body)
		return ProviderSessionToken{}, ErrProviderQuotaExhausted
	case response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices:
		discardSpatiusResponse(response.Body)
		return ProviderSessionToken{}, ErrProviderUnavailable
	}
	if !spatiusJSONContentType(response.Header.Get("Content-Type")) {
		discardSpatiusResponse(response.Body)
		return ProviderSessionToken{}, ErrInvalidProviderResponse
	}
	body, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxSpatiusResponseBody+1,
	))
	if err != nil || len(body) == 0 || len(body) > maxSpatiusResponseBody {
		return ProviderSessionToken{}, ErrInvalidProviderResponse
	}
	token, err := decodeSpatiusSessionToken(body)
	if err != nil {
		return ProviderSessionToken{}, err
	}
	return ProviderSessionToken{
		Value:     token,
		ExpiresAt: expiresAt.UTC().Truncate(time.Second),
	}, nil
}

func decodeSpatiusSessionToken(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var document struct {
		SessionToken string `json:"sessionToken"`
		Errors       []struct {
			Status int `json:"status"`
		} `json:"errors"`
	}
	if err := decoder.Decode(&document); err != nil {
		return "", ErrInvalidProviderResponse
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return "", ErrInvalidProviderResponse
	}
	for _, providerError := range document.Errors {
		if providerError.Status == http.StatusPaymentRequired ||
			providerError.Status == http.StatusTooManyRequests {
			return "", ErrProviderQuotaExhausted
		}
	}
	if len(document.Errors) > 0 {
		return "", ErrProviderUnavailable
	}
	if !validSessionToken(document.SessionToken) {
		return "", ErrInvalidProviderResponse
	}
	return document.SessionToken, nil
}

func spatiusJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func discardSpatiusResponse(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxSpatiusResponseBody))
}
