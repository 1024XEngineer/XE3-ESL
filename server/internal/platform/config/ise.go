package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultISEEndpoint = "wss://ise-api.xfyun.cn/v2/open-ise"
	defaultISETimeout  = 90 * time.Second
)

type ISEConfig struct {
	Endpoint  string
	Timeout   time.Duration
	AppID     Secret
	APIKey    Secret
	APISecret Secret
}

// ISEConfigured reports whether any ISE credential was provided. A completely
// absent credential set means acoustic scoring is intentionally unavailable;
// a partial set is still treated as a configuration error by LoadISE.
func ISEConfigured() bool {
	return strings.TrimSpace(os.Getenv("APPID")) != "" ||
		strings.TrimSpace(os.Getenv("APIKey")) != "" ||
		strings.TrimSpace(os.Getenv("APISecret")) != ""
}

func LoadISE() (ISEConfig, error) {
	endpoint := strings.TrimSpace(os.Getenv("XFYUN_ISE_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultISEEndpoint
	}
	timeout, err := durationOrDefault(
		"XFYUN_ISE_TIMEOUT",
		defaultISETimeout,
	)
	if err != nil {
		return ISEConfig{}, err
	}
	if timeout <= 0 || timeout > 5*time.Minute {
		return ISEConfig{}, fmt.Errorf(
			"XFYUN_ISE_TIMEOUT must be greater than zero and at most %s",
			5*time.Minute,
		)
	}
	appID, err := loadISESecret("APPID")
	if err != nil {
		return ISEConfig{}, err
	}
	apiKey, err := loadISESecret("APIKey")
	if err != nil {
		return ISEConfig{}, err
	}
	apiSecret, err := loadISESecret("APISecret")
	if err != nil {
		return ISEConfig{}, err
	}
	return ISEConfig{
		Endpoint:  endpoint,
		Timeout:   timeout,
		AppID:     appID,
		APIKey:    apiKey,
		APISecret: apiSecret,
	}, nil
}

func loadISESecret(name string) (Secret, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return Secret{}, fmt.Errorf("%s is required", name)
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return Secret{}, fmt.Errorf(
			"%s contains whitespace or control characters",
			name,
		)
	}
	return Secret{value: value}, nil
}
