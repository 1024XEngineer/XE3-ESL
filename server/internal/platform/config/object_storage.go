package config

import (
	"errors"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	defaultAudioStoragePrefix = "audio/v1"
	defaultImageStoragePrefix = "image/v1"
	defaultSignedURLTTL       = 2 * time.Minute
	maxSignedURLTTL           = 2 * time.Minute
)

type ObjectStorageCredentialsProvider string

const (
	ObjectStorageCredentialsECSRole     ObjectStorageCredentialsProvider = "ecs_role"
	ObjectStorageCredentialsEnvironment ObjectStorageCredentialsProvider = "environment"
)

var (
	ErrObjectStorageEnabledInvalid = errors.New("OSS_ENABLED must be 0, 1, false, or true")
	ErrObjectStorageRegionRequired = errors.New("OSS_REGION is required when object storage is enabled")
	ErrObjectStorageEndpoint       = errors.New("OSS_ENDPOINT must be an HTTPS origin without credentials, query, or fragment")
	ErrObjectStorageBucketRequired = errors.New("OSS_BUCKET is required when object storage is enabled")
	ErrObjectStoragePrefix         = errors.New("OSS object prefixes must use their configured v1 namespaces")
	ErrObjectStorageSignedURLTTL   = errors.New("OSS_SIGNED_URL_TTL must be positive and no greater than 2m")
	ErrObjectStorageCredentials    = errors.New("OSS_CREDENTIALS_PROVIDER must be ecs_role or environment")
	ErrObjectStorageRAMRoleName    = errors.New("OSS_RAM_ROLE_NAME must be a valid RAM role name and is only allowed with ecs_role")
)

var ramRoleNamePattern = regexp.MustCompile(`\A[A-Za-z0-9.@_-]{1,64}\z`)

// ObjectStorageConfig contains only non-secret object-storage settings.
// Access credentials stay in the SDK credential provider so Config values can
// be logged during local debugging without disclosing an AccessKey secret.
type ObjectStorageConfig struct {
	Enabled             bool
	Region              string
	Endpoint            string
	Bucket              string
	AudioPrefix         string
	ImagePrefix         string
	SignedURLTTL        time.Duration
	CredentialsProvider ObjectStorageCredentialsProvider
	RAMRoleName         string
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
		AudioPrefix:  valueOrDefault("OSS_AUDIO_PREFIX", defaultAudioStoragePrefix),
		ImagePrefix:  valueOrDefault("OSS_IMAGE_PREFIX", defaultImageStoragePrefix),
		SignedURLTTL: defaultSignedURLTTL,
		CredentialsProvider: ObjectStorageCredentialsProvider(strings.TrimSpace(
			valueOrDefault(
				"OSS_CREDENTIALS_PROVIDER",
				string(ObjectStorageCredentialsECSRole),
			),
		)),
		RAMRoleName: strings.TrimSpace(os.Getenv("OSS_RAM_ROLE_NAME")),
	}

	if rawTTL := strings.TrimSpace(os.Getenv("OSS_SIGNED_URL_TTL")); rawTTL != "" {
		ttl, parseErr := time.ParseDuration(rawTTL)
		if parseErr != nil {
			return ObjectStorageConfig{}, ErrObjectStorageSignedURLTTL
		}
		config.SignedURLTTL = ttl
	}

	config.AudioPrefix = strings.TrimSuffix(strings.TrimSpace(config.AudioPrefix), "/")
	config.ImagePrefix = strings.TrimSuffix(strings.TrimSpace(config.ImagePrefix), "/")
	if err := validateObjectStoragePrefix(config.AudioPrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if err := validateObjectStoragePrefix(config.ImagePrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if config.AudioPrefix != defaultAudioStoragePrefix ||
		config.ImagePrefix != defaultImageStoragePrefix ||
		config.AudioPrefix == config.ImagePrefix {
		return ObjectStorageConfig{}, ErrObjectStoragePrefix
	}
	if config.SignedURLTTL <= 0 || config.SignedURLTTL > maxSignedURLTTL {
		return ObjectStorageConfig{}, ErrObjectStorageSignedURLTTL
	}

	if !config.Enabled {
		return config, nil
	}
	switch config.CredentialsProvider {
	case ObjectStorageCredentialsECSRole:
		if config.RAMRoleName != "" &&
			!ramRoleNamePattern.MatchString(config.RAMRoleName) {
			return ObjectStorageConfig{}, ErrObjectStorageRAMRoleName
		}
	case ObjectStorageCredentialsEnvironment:
		if config.RAMRoleName != "" {
			return ObjectStorageConfig{}, ErrObjectStorageRAMRoleName
		}
	default:
		return ObjectStorageConfig{}, ErrObjectStorageCredentials
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
