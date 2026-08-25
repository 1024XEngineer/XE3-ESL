package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultISERelayPollInterval = 500 * time.Millisecond
	defaultISERelayTimeout      = 165 * time.Second
)

type ISERelayConfig struct {
	Endpoint       string
	CAFile         string
	ClientCertFile string
	ClientKeyFile  string
	PollInterval   time.Duration
	Timeout        time.Duration
}

// ISERelayConfigured reports whether any relay setting was provided. LoadISERelay
// rejects partial settings so a broken relay cannot silently disable acoustics.
func ISERelayConfigured() bool {
	for _, name := range []string{
		"ISE_RELAY_ENDPOINT",
		"ISE_RELAY_CA_FILE",
		"ISE_RELAY_CLIENT_CERT_FILE",
		"ISE_RELAY_CLIENT_KEY_FILE",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

func LoadISERelay() (ISERelayConfig, error) {
	configuration := ISERelayConfig{
		Endpoint:       strings.TrimSpace(os.Getenv("ISE_RELAY_ENDPOINT")),
		CAFile:         strings.TrimSpace(os.Getenv("ISE_RELAY_CA_FILE")),
		ClientCertFile: strings.TrimSpace(os.Getenv("ISE_RELAY_CLIENT_CERT_FILE")),
		ClientKeyFile:  strings.TrimSpace(os.Getenv("ISE_RELAY_CLIENT_KEY_FILE")),
	}
	for _, value := range []string{
		configuration.Endpoint,
		configuration.CAFile,
		configuration.ClientCertFile,
		configuration.ClientKeyFile,
	} {
		if value == "" {
			return ISERelayConfig{}, errors.New("ISE relay configuration is incomplete")
		}
	}
	endpoint, err := url.Parse(configuration.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return ISERelayConfig{}, errors.New("ISE_RELAY_ENDPOINT must be an HTTPS origin")
	}
	configuration.PollInterval, err = durationOrDefault(
		"ISE_RELAY_POLL_INTERVAL",
		defaultISERelayPollInterval,
	)
	if err != nil || configuration.PollInterval < 100*time.Millisecond ||
		configuration.PollInterval > 10*time.Second {
		return ISERelayConfig{}, errors.New("ISE_RELAY_POLL_INTERVAL must be between 100ms and 10s")
	}
	configuration.Timeout, err = durationOrDefault(
		"ISE_RELAY_TIMEOUT",
		defaultISERelayTimeout,
	)
	if err != nil || configuration.Timeout <= 0 || configuration.Timeout > 5*time.Minute {
		return ISERelayConfig{}, errors.New("ISE_RELAY_TIMEOUT must be greater than zero and at most 5m")
	}
	return configuration, nil
}
