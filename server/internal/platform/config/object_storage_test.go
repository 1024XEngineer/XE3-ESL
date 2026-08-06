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
	t.Setenv("OSS_IMAGE_PREFIX", "")
	t.Setenv("OSS_RESUME_PREFIX", "")
	t.Setenv("OSS_SIGNED_URL_TTL", "")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "")
	t.Setenv("OSS_RAM_ROLE_NAME", "")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if config.Enabled ||
		config.AudioPrefix != "audio/v1" ||
		config.ImagePrefix != "image/v1" ||
		config.ResumePrefix != "resume/v1" ||
		config.SignedURLTTL != 2*time.Minute ||
		config.CredentialsProvider != ObjectStorageCredentialsECSRole ||
		config.RAMRoleName != "" {
		t.Fatalf("unexpected disabled config: %#v", config)
	}
}

func TestLoadObjectStorageReadsEnabledConfigurationWithoutSecrets(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "cn-shanghai")
	t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
	t.Setenv("OSS_BUCKET", "example-private-audio-bucket")
	t.Setenv("OSS_AUDIO_PREFIX", "audio/v1/")
	t.Setenv("OSS_IMAGE_PREFIX", "image/v1/")
	t.Setenv("OSS_RESUME_PREFIX", "resume/v1/")
	t.Setenv("OSS_SIGNED_URL_TTL", "90s")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "environment")
	t.Setenv("OSS_RAM_ROLE_NAME", "")
	t.Setenv("OSS_ACCESS_KEY_ID", "must-not-be-copied")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "must-not-be-copied")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if !config.Enabled ||
		config.Region != "cn-shanghai" ||
		config.Endpoint != "https://oss-cn-shanghai.aliyuncs.com" ||
		config.Bucket != "example-private-audio-bucket" ||
		config.AudioPrefix != "audio/v1" ||
		config.ImagePrefix != "image/v1" ||
		config.ResumePrefix != "resume/v1" ||
		config.SignedURLTTL != 90*time.Second ||
		config.CredentialsProvider != ObjectStorageCredentialsEnvironment {
		t.Fatalf("unexpected enabled config: %#v", config)
	}
}

func TestLoadObjectStorageReadsECSRoleConfiguration(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "cn-shanghai")
	t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
	t.Setenv("OSS_BUCKET", "example-private-audio-bucket")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "ecs_role")
	t.Setenv("OSS_RAM_ROLE_NAME", "SpeakUp.Audio_Role-1")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if config.CredentialsProvider != ObjectStorageCredentialsECSRole ||
		config.RAMRoleName != "SpeakUp.Audio_Role-1" {
		t.Fatalf("unexpected ECS role config: %#v", config)
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
			name:     "unsupported normalized prefix",
			key:      "OSS_AUDIO_PREFIX",
			value:    "custom/audio",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "image prefix traversal",
			key:      "OSS_IMAGE_PREFIX",
			value:    "../image/v1",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "unsupported image prefix",
			key:      "OSS_IMAGE_PREFIX",
			value:    "custom/image",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "resume prefix traversal",
			key:      "OSS_RESUME_PREFIX",
			value:    "../resume/v1",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "unsupported resume prefix",
			key:      "OSS_RESUME_PREFIX",
			value:    "custom/resume",
			expected: ErrObjectStoragePrefix,
		},
		{
			name:     "ttl above product limit",
			key:      "OSS_SIGNED_URL_TTL",
			value:    "3m",
			expected: ErrObjectStorageSignedURLTTL,
		},
		{
			name:     "credentials provider",
			key:      "OSS_CREDENTIALS_PROVIDER",
			value:    "automatic",
			expected: ErrObjectStorageCredentials,
		},
		{
			name:     "RAM role name",
			key:      "OSS_RAM_ROLE_NAME",
			value:    "invalid role name",
			expected: ErrObjectStorageRAMRoleName,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OSS_ENABLED", "1")
			t.Setenv("OSS_REGION", "cn-shanghai")
			t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
			t.Setenv("OSS_BUCKET", "example-bucket")
			t.Setenv("OSS_AUDIO_PREFIX", "audio/v1")
			t.Setenv("OSS_IMAGE_PREFIX", "image/v1")
			t.Setenv("OSS_RESUME_PREFIX", "resume/v1")
			t.Setenv("OSS_SIGNED_URL_TTL", "2m")
			t.Setenv("OSS_CREDENTIALS_PROVIDER", "ecs_role")
			t.Setenv("OSS_RAM_ROLE_NAME", "")
			t.Setenv(testCase.key, testCase.value)

			_, err := LoadObjectStorage()
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("LoadObjectStorage() error = %v, want %v", err, testCase.expected)
			}
		})
	}
}

func TestLoadObjectStorageRejectsRAMRoleWithEnvironmentCredentials(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "cn-shanghai")
	t.Setenv("OSS_ENDPOINT", "https://oss-cn-shanghai.aliyuncs.com")
	t.Setenv("OSS_BUCKET", "example-private-audio-bucket")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "environment")
	t.Setenv("OSS_RAM_ROLE_NAME", "unexpected-role")

	_, err := LoadObjectStorage()
	if !errors.Is(err, ErrObjectStorageRAMRoleName) {
		t.Fatalf("LoadObjectStorage() error = %v, want %v", err, ErrObjectStorageRAMRoleName)
	}
}

func TestLoadObjectStorageDisabledIgnoresUnusedCredentialSettings(t *testing.T) {
	t.Setenv("OSS_ENABLED", "false")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "unused-provider")
	t.Setenv("OSS_RAM_ROLE_NAME", "invalid role name")

	config, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if config.Enabled {
		t.Fatal("LoadObjectStorage() enabled = true, want false")
	}
}

func TestLoadObjectStorageRequiresRemoteSettingsWhenEnabled(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OSS_REGION", "")
	t.Setenv("OSS_ENDPOINT", "")
	t.Setenv("OSS_BUCKET", "")
	t.Setenv("OSS_CREDENTIALS_PROVIDER", "")
	t.Setenv("OSS_RAM_ROLE_NAME", "")

	_, err := LoadObjectStorage()
	if !errors.Is(err, ErrObjectStorageRegionRequired) {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
}
