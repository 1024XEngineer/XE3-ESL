package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	SpatiusRegionChinaBeijing = "cn-beijing"
	SpatiusRegionAPNortheast  = "ap-northeast"
	SpatiusRegionUSWest       = "us-west"
	defaultSpatiusRegion      = SpatiusRegionChinaBeijing
	defaultSpatiusTokenTTL    = 10 * time.Minute
	defaultSpatiusTimeout     = 5 * time.Second
	minimumSpatiusTokenTTL    = time.Minute
	maximumSpatiusTokenTTL    = 10 * time.Minute
	maximumSpatiusHTTPTimeout = 30 * time.Second
)

var spatiusConsoleBaseURLs = map[string]string{
	SpatiusRegionChinaBeijing: "https://console.cn-beijing.spatialwalk.top/v1/console",
	SpatiusRegionAPNortheast:  "https://console.ap-northeast.spatius.ai/v1/console",
	SpatiusRegionUSWest:       "https://console.us-west.spatius.ai/v1/console",
}

type SpatiusConfig struct {
	Enabled        bool
	ConsoleBaseURL string
	AppID          string
	AvatarID       string
	Region         string
	APIKey         Secret
	TokenTTL       time.Duration
	Timeout        time.Duration
}

// LoadSpatius validates the server-only avatar provider configuration. When
// disabled, public metadata and credentials are optional so the existing
// voice/text experience can start without the provider.
func LoadSpatius() (SpatiusConfig, error) {
	enabled, err := parseSpatiusEnabled(os.Getenv("SPATIUS_ENABLED"))
	if err != nil {
		return SpatiusConfig{}, err
	}
	if !enabled {
		return SpatiusConfig{
			Enabled:        false,
			ConsoleBaseURL: spatiusConsoleBaseURLs[defaultSpatiusRegion],
			Region:         defaultSpatiusRegion,
			TokenTTL:       defaultSpatiusTokenTTL,
			Timeout:        defaultSpatiusTimeout,
		}, nil
	}
	region := strings.ToLower(strings.TrimSpace(os.Getenv("SPATIUS_REGION")))
	if region == "" {
		region = defaultSpatiusRegion
	}
	defaultBaseURL, regionSupported := spatiusConsoleBaseURLs[region]
	if !regionSupported {
		return SpatiusConfig{}, errors.New("SPATIUS_REGION is not supported")
	}
	baseURL := strings.TrimSpace(os.Getenv("SPATIUS_CONSOLE_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if !validSpatiusConsoleBaseURL(baseURL, defaultBaseURL) {
		return SpatiusConfig{}, errors.New(
			"SPATIUS_CONSOLE_BASE_URL does not match SPATIUS_REGION",
		)
	}
	tokenTTL, err := durationOrDefault(
		"SPATIUS_TOKEN_TTL",
		defaultSpatiusTokenTTL,
	)
	if err != nil {
		return SpatiusConfig{}, err
	}
	if tokenTTL < minimumSpatiusTokenTTL ||
		tokenTTL > maximumSpatiusTokenTTL {
		return SpatiusConfig{}, fmt.Errorf(
			"SPATIUS_TOKEN_TTL must be between %s and %s",
			minimumSpatiusTokenTTL,
			maximumSpatiusTokenTTL,
		)
	}
	timeout, err := durationOrDefault(
		"SPATIUS_TIMEOUT",
		defaultSpatiusTimeout,
	)
	if err != nil {
		return SpatiusConfig{}, err
	}
	if timeout <= 0 || timeout > maximumSpatiusHTTPTimeout {
		return SpatiusConfig{}, fmt.Errorf(
			"SPATIUS_TIMEOUT must be greater than zero and at most %s",
			maximumSpatiusHTTPTimeout,
		)
	}

	appID := strings.TrimSpace(os.Getenv("SPATIUS_APP_ID"))
	avatarID := strings.TrimSpace(os.Getenv("SPATIUS_AVATAR_ID"))
	rawAPIKey := strings.TrimSpace(os.Getenv("SPATIUS_API_KEY"))
	if enabled {
		switch {
		case !validSpatiusIdentifier(appID):
			return SpatiusConfig{}, errors.New(
				"SPATIUS_APP_ID is required and must be a valid identifier",
			)
		case !validSpatiusIdentifier(avatarID):
			return SpatiusConfig{}, errors.New(
				"SPATIUS_AVATAR_ID is required and must be a valid identifier",
			)
		case !validSpatiusSecret(rawAPIKey):
			return SpatiusConfig{}, errors.New(
				"SPATIUS_API_KEY is required and contains an invalid character",
			)
		}
	}
	return SpatiusConfig{
		Enabled:        enabled,
		ConsoleBaseURL: baseURL,
		AppID:          appID,
		AvatarID:       avatarID,
		Region:         region,
		APIKey:         Secret{value: rawAPIKey},
		TokenTTL:       tokenTTL,
		Timeout:        timeout,
	}, nil
}

func parseSpatiusEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "disabled":
		return false, nil
	case "1", "true", "enabled":
		return true, nil
	default:
		return false, errors.New("SPATIUS_ENABLED must be a boolean")
	}
}

func validSpatiusConsoleBaseURL(value string, expected string) bool {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.String() != value {
		return false
	}
	return value == expected
}

func validSpatiusIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-',
			character == '_',
			character == '.':
		default:
			return false
		}
	}
	return true
}

func validSpatiusSecret(value string) bool {
	if len(value) < 8 || len(value) > 4096 {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return character < 0x21 || character > 0x7e
	}) < 0
}
