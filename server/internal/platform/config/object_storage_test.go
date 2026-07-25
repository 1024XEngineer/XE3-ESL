package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadObjectStorageUsesSafeDefaultsWhenDisabled(t *testing.T) {
	t.Setenv("OSS_ENABLED", "")
	t.Setenv("OSS_REGION", "")
	t.Setenv("OSS_ENDPOINT", "")
	t.Setenv("OSS_BUCKET", "")
	t.Setenv("OSS_AUDIO_PREFIX", "")
	t.Setenv("OSS_SIGNED_URL_TTL", "")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if config.Enabled ||
		config.AudioPrefix != "audio/v1" ||
		config.SignedURLTTL != 2*time.Minute {
		t.Fatalf("unexpected disabled config: %#v", config)
	}
}

func TestLoadObjectStorageReadsEnabledConfigurationWithoutSecrets(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "cn-shanghai")
	t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
	t.Setenv("OSS_BUCKET", "speakup-audio-dev-cn-shanghai-lq0412")
	t.Setenv("OSS_AUDIO_PREFIX", "audio/v1/")
	t.Setenv("OSS_SIGNED_URL_TTL", "90s")
	t.Setenv("OSS_ACCESS_KEY_ID", "must-not-be-copied")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "must-not-be-copied")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if !config.Enabled ||
		config.Region != "cn-shanghai" ||
		config.Endpoint != "https://oss-cn-shanghai.aliyuncs.com" ||
		config.Bucket != "speakup-audio-dev-cn-shanghai-lq0412" ||
		config.AudioPrefix != "audio/v1" ||
		config.SignedURLTTL != 90*time.Second {
		t.Fatalf("unexpected enabled config: %#v", config)
	}
}

func TestLoadObjectStorageRejectsUnsafeValues(t *testing.T) {
	testCases := []struct {
		name     string
		key      string
		value    string
		expected error
	}{
		{
			name:     "enabled",
			key:      "OSS_ENABLED",
			value:    "sometimes",
			expected: ErrObjectStorageEnabledInvalid,
		},
		{
			name:     "endpoint scheme",
			key:      "OSS_ENDPOINT",
			value:    "http://oss-cn-shanghai.aliyuncs.com",
			expected: ErrObjectStorageEndpoint,
		},
		{
			name:     "endpoint credentials",
			key:      "OSS_ENDPOINT",
			value:    "https://user:secret@oss-cn-shanghai.aliyuncs.com",
			expected: ErrObjectStorageEndpoint,
		},
		{
			name:     "prefix traversal",
			key:      "OSS_AUDIO_PREFIX",
			value:    "../audio/v1",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "ttl above product limit",
			key:      "OSS_SIGNED_URL_TTL",
			value:    "3m",
			expected: ErrObjectStorageSignedURLTTL,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OSS_ENABLED", "1")
			t.Setenv("OSS_REGION", "cn-shanghai")
			t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
			t.Setenv("OSS_BUCKET", "example-bucket")
			t.Setenv("OSS_AUDIO_PREFIX", "audio/v1")
			t.Setenv("OSS_SIGNED_URL_TTL", "2m")
			t.Setenv(testCase.key, testCase.value)

			_, err := LoadObjectStorage()
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("LoadObjectStorage() error = %v, want %v", err, testCase.expected)
			}
		})
	}
}

func TestLoadObjectStorageRequiresRemoteSettingsWhenEnabled(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "")
	t.Setenv("OSS_ENDPOINT", "")
	t.Setenv("OSS_BUCKET", "")

	_, err := LoadObjectStorage()
	if !errors.Is(err, ErrObjectStorageRegionRequired) {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
}
