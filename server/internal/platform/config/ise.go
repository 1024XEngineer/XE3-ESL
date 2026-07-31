package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
)

const defaultISETimeout = 90 * time.Second

type ISEConfig struct {
	Endpoint  string
	Timeout   time.Duration
	AppID     Secret
	APIKey    Secret
	APISecret Secret
}

func LoadISE() (ISEConfig, error) {
	endpoint := strings.TrimSpace(os.Getenv("XFYUN_ISE_ENDPOINT"))
	if endpoint == "" {
		endpoint = xfyun.DefaultISEEndpoint
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
