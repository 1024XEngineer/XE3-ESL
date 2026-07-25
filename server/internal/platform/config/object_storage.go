package config

import (
	"errors"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	defaultObjectStoragePrefix = "audio/v1"
	defaultSignedURLTTL        = 2 * time.Minute
	maxSignedURLTTL            = 2 * time.Minute
)

var (
	ErrObjectStorageEnabledInvalid = errors.New("OSS_ENABLED must be 0, 1, false, or true")
	ErrObjectStorageRegionRequired = errors.New("OSS_REGION is required when object storage is enabled")
	ErrObjectStorageEndpoint       = errors.New("OSS_ENDPOINT must be an HTTPS origin without credentials, query, or fragment")
	ErrObjectStorageBucketRequired = errors.New("OSS_BUCKET is required when object storage is enabled")
	ErrObjectStoragePrefix         = errors.New("OSS_AUDIO_PREFIX must be a normalized relative object prefix")
	ErrObjectStorageSignedURLTTL   = errors.New("OSS_SIGNED_URL_TTL must be positive and no greater than 2m")
)

// ObjectStorageConfig contains only non-secret object-storage settings.
// Access credentials stay in the SDK credential provider so Config values can
// be logged during local debugging without disclosing an AccessKey secret.
type ObjectStorageConfig struct {
	Enabled      bool
	Region       string
	Endpoint     string
	Bucket       string
	AudioPrefix  string
	SignedURLTTL time.Duration
}

// LoadObjectStorage reads and validates the server-only OSS configuration.
// Disabled storage does not require credentials or remote settings.
func LoadObjectStorage() (ObjectStorageConfig, error) {
	enabled, err := parseObjectStorageEnabled(os.Getenv("OSS_ENABLED"))
	if err != nil {
		return ObjectStorageConfig{}, err
	}

	config := ObjectStorageConfig{
		Enabled:      enabled,
		Region:       strings.TrimSpace(os.Getenv("OSS_REGION")),
		Endpoint:     strings.TrimSpace(os.Getenv("OSS_ENDPOINT")),
		Bucket:       strings.TrimSpace(os.Getenv("OSS_BUCKET")),
		AudioPrefix:  valueOrDefault("OSS_AUDIO_PREFIX", defaultObjectStoragePrefix),
		SignedURLTTL: defaultSignedURLTTL,
	}

	if rawTTL := strings.TrimSpace(os.Getenv("OSS_SIGNED_URL_TTL")); rawTTL != "" {
		ttl, parseErr := time.ParseDuration(rawTTL)
		if parseErr != nil {
			return ObjectStorageConfig{}, ErrObjectStorageSignedURLTTL
		}
		config.SignedURLTTL = ttl
	}

	config.AudioPrefix = strings.TrimSuffix(strings.TrimSpace(config.AudioPrefix), "/")
	if err := validateObjectStoragePrefix(config.AudioPrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if config.SignedURLTTL <= 0 || config.SignedURLTTL > maxSignedURLTTL {
		return ObjectStorageConfig{}, ErrObjectStorageSignedURLTTL
	}

	if !config.Enabled {
		return config, nil
	}
	if config.Region == "" {
		return ObjectStorageConfig{}, ErrObjectStorageRegionRequired
	}
	if config.Bucket == "" {
		return ObjectStorageConfig{}, ErrObjectStorageBucketRequired
	}
	if err := validateObjectStorageEndpoint(config.Endpoint); err != nil {
		return ObjectStorageConfig{}, err
	}

	return config, nil
}

func parseObjectStorageEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, ErrObjectStorageEnabledInvalid
	}
}

func validateObjectStorageEndpoint(raw string) error {
	endpoint, err := url.Parse(raw)
	if err != nil ||
		endpoint.Scheme != "https" ||
		endpoint.Host == "" ||
		(endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		return ErrObjectStorageEndpoint
	}
	return nil
}

func validateObjectStoragePrefix(prefix string) error {
	if prefix == "" ||
		strings.HasPrefix(prefix, "/") ||
		strings.Contains(prefix, "\\") ||
		path.Clean(prefix) != prefix ||
		prefix == "." ||
		prefix == ".." ||
		strings.HasPrefix(prefix, "../") {
		return ErrObjectStoragePrefix
	}
	return nil
}
