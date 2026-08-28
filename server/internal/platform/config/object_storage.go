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
	ObjectStorageProviderAliyunOSS = "aliyun_oss"
	ObjectStorageProviderQiniuKodo = "qiniu_kodo"

	defaultAudioStoragePrefix  = "audio/v1"
	defaultImageStoragePrefix  = "image/v1"
	defaultResumeStoragePrefix = "resume/v1"
	defaultSignedURLTTL        = 2 * time.Minute
	maxSignedURLTTL            = 2 * time.Minute
)

type ObjectStorageCredentialsProvider string

const (
	ObjectStorageCredentialsECSRole     ObjectStorageCredentialsProvider = "ecs_role"
	ObjectStorageCredentialsEnvironment ObjectStorageCredentialsProvider = "environment"
)

var (
	ErrObjectStorageEnabledInvalid  = errors.New("OSS_ENABLED must be 0, 1, false, or true")
	ErrObjectStorageProvider        = errors.New("OBJECT_STORAGE_PROVIDER must be aliyun_oss or qiniu_kodo")
	ErrObjectStorageRegionRequired  = errors.New("OSS_REGION is required when object storage is enabled")
	ErrObjectStorageEndpoint        = errors.New("OSS_ENDPOINT must be an HTTPS origin without credentials, query, or fragment")
	ErrObjectStorageBucketRequired  = errors.New("OSS_BUCKET is required when object storage is enabled")
	ErrObjectStoragePrefix          = errors.New("OSS object prefixes must use their configured v1 namespaces")
	ErrObjectStorageSignedURLTTL    = errors.New("OSS_SIGNED_URL_TTL must be positive and no greater than 2m")
	ErrObjectStorageCredentials     = errors.New("OSS_CREDENTIALS_PROVIDER must be ecs_role or environment")
	ErrObjectStorageRAMRoleName     = errors.New("OSS_RAM_ROLE_NAME must be a valid RAM role name and is only allowed with ecs_role")
	ErrObjectStorageQiniuS3Region   = errors.New("QINIU_KODO_S3_REGION must be a valid Qiniu S3 region")
	ErrObjectStorageQiniuS3Endpoint = errors.New("QINIU_KODO_S3_ENDPOINT must be the official HTTPS endpoint for QINIU_KODO_S3_REGION")
	ErrObjectStorageQiniuBucket     = errors.New("QINIU_KODO_S3_BUCKET is required when Qiniu Kodo is enabled")
)

var (
	ramRoleNamePattern   = regexp.MustCompile(`\A[A-Za-z0-9.@_-]{1,64}\z`)
	qiniuS3RegionPattern = regexp.MustCompile(`\A[a-z]{2}(?:-[a-z0-9]+)+-[0-9]+\z`)
)

// ObjectStorageConfig contains only non-secret object-storage settings.
// Access credentials stay in the SDK credential provider so Config values can
// be logged during local debugging without disclosing an AccessKey secret.
type ObjectStorageConfig struct {
	Enabled             bool
	Provider            string
	Region              string
	Endpoint            string
	Bucket              string
	AudioPrefix         string
	ImagePrefix         string
	ResumePrefix        string
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
		Enabled: enabled,
		Provider: strings.ToLower(strings.TrimSpace(valueOrDefault(
			"OBJECT_STORAGE_PROVIDER",
			ObjectStorageProviderAliyunOSS,
		))),
		Region:       strings.TrimSpace(os.Getenv("OSS_REGION")),
		Endpoint:     strings.TrimSpace(os.Getenv("OSS_ENDPOINT")),
		Bucket:       strings.TrimSpace(os.Getenv("OSS_BUCKET")),
		AudioPrefix:  valueOrDefault("OSS_AUDIO_PREFIX", defaultAudioStoragePrefix),
		ImagePrefix:  valueOrDefault("OSS_IMAGE_PREFIX", defaultImageStoragePrefix),
		ResumePrefix: valueOrDefault("OSS_RESUME_PREFIX", defaultResumeStoragePrefix),
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
	config.ResumePrefix = strings.TrimSuffix(strings.TrimSpace(config.ResumePrefix), "/")
	if err := validateObjectStoragePrefix(config.AudioPrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if err := validateObjectStoragePrefix(config.ImagePrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if err := validateObjectStoragePrefix(config.ResumePrefix); err != nil {
		return ObjectStorageConfig{}, err
	}
	if config.AudioPrefix != defaultAudioStoragePrefix ||
		config.ImagePrefix != defaultImageStoragePrefix ||
		config.ResumePrefix != defaultResumeStoragePrefix ||
		config.AudioPrefix == config.ImagePrefix ||
		config.AudioPrefix == config.ResumePrefix ||
		config.ImagePrefix == config.ResumePrefix {
		return ObjectStorageConfig{}, ErrObjectStoragePrefix
	}
	if config.SignedURLTTL <= 0 || config.SignedURLTTL > maxSignedURLTTL {
		return ObjectStorageConfig{}, ErrObjectStorageSignedURLTTL
	}

	if !config.Enabled {
		return config, nil
	}
	if config.Provider == ObjectStorageProviderQiniuKodo {
		config.Region = strings.TrimSpace(os.Getenv("QINIU_KODO_S3_REGION"))
		config.Endpoint = strings.TrimSpace(os.Getenv("QINIU_KODO_S3_ENDPOINT"))
		config.Bucket = strings.TrimSpace(os.Getenv("QINIU_KODO_S3_BUCKET"))
		if config.Bucket == "" {
			return ObjectStorageConfig{}, ErrObjectStorageQiniuBucket
		}
		if err := validateQiniuS3Endpoint(config.Region, config.Endpoint); err != nil {
			return ObjectStorageConfig{}, err
		}
		return config, nil
	}
	if config.Provider != ObjectStorageProviderAliyunOSS {
		return ObjectStorageConfig{}, ErrObjectStorageProvider
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

func validateQiniuS3Endpoint(region string, raw string) error {
	if !qiniuS3RegionPattern.MatchString(region) {
		return ErrObjectStorageQiniuS3Region
	}
	endpoint, err := url.Parse(raw)
	if err != nil ||
		endpoint.Scheme != "https" ||
		endpoint.Host != "s3."+region+".qiniucs.com" ||
		(endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		return ErrObjectStorageQiniuS3Endpoint
	}
	return nil
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
