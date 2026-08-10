package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoadObjectStorageUsesSafeDefaultsWhenDisabled(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PROVIDER", "")
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
		config.Provider != ObjectStorageProviderAliyunOSS ||
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
	t.Setenv("OBJECT_STORAGE_PROVIDER", ObjectStorageProviderAliyunOSS)
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
		config.Provider != ObjectStorageProviderAliyunOSS ||
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

func TestLoadObjectStorageReadsQiniuKodoConfigurationWithoutSecrets(t *testing.T) {
	t.Setenv("OSS_ENABLED", "1")
	t.Setenv("OBJECT_STORAGE_PROVIDER", ObjectStorageProviderQiniuKodo)
	t.Setenv("QINIU_KODO_S3_REGION", "cn-east-1")
	t.Setenv("QINIU_KODO_S3_ENDPOINT", "https://s3.cn-east-1.qiniucs.com")
	t.Setenv("QINIU_KODO_S3_BUCKET", "speakup-private")
	t.Setenv("QINIU_KODO_SERVER_SIDE_ENCRYPTION", "true")
	t.Setenv("QINIU_ACCESS_KEY", "must-not-be-copied")
	t.Setenv("QINIU_SECRET_KEY", "must-not-be-copied")

	configuration, err := LoadObjectStorage()
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if !configuration.Enabled ||
		configuration.Provider != ObjectStorageProviderQiniuKodo ||
		configuration.Bucket != "speakup-private" ||
		configuration.Region != "cn-east-1" ||
		configuration.Endpoint != "https://s3.cn-east-1.qiniucs.com" ||
		!configuration.ServerSideEncryption ||
		configuration.AudioPrefix != "audio/v1" ||
		configuration.ImagePrefix != "image/v1" ||
		configuration.ResumePrefix != "resume/v1" {
		t.Fatalf("unexpected Qiniu config: %#v", configuration)
	}
}

func TestLoadObjectStorageRejectsUnsafeQiniuKodoConfiguration(t *testing.T) {
	testCases := []struct {
		name       string
		region     string
		endpoint   string
		bucket     string
		encryption string
		expected   error
	}{
		{
			name: "missing bucket", region: "cn-east-1",
			endpoint:   "https://s3.cn-east-1.qiniucs.com",
			encryption: "1", expected: ErrObjectStorageQiniuBucket,
		},
		{
			name: "invalid region", region: "z0", bucket: "speakup-private",
			endpoint: "https://s3.cn-east-1.qiniucs.com", encryption: "1",
			expected: ErrObjectStorageQiniuS3Region,
		},
		{
			name: "insecure endpoint", region: "cn-east-1", bucket: "speakup-private",
			endpoint: "http://s3.cn-east-1.qiniucs.com", encryption: "1",
			expected: ErrObjectStorageQiniuS3Endpoint,
		},
		{
			name: "endpoint region mismatch", region: "cn-east-1", bucket: "speakup-private",
			endpoint: "https://s3.cn-south-1.qiniucs.com", encryption: "1",
			expected: ErrObjectStorageQiniuS3Endpoint,
		},
		{
			name: "encryption not attested", bucket: "speakup-private",
			region: "cn-east-1", endpoint: "https://s3.cn-east-1.qiniucs.com", encryption: "0",
			expected: ErrObjectStorageEncryption,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OSS_ENABLED", "1")
			t.Setenv("OBJECT_STORAGE_PROVIDER", ObjectStorageProviderQiniuKodo)
			t.Setenv("QINIU_KODO_S3_REGION", testCase.region)
			t.Setenv("QINIU_KODO_S3_ENDPOINT", testCase.endpoint)
			t.Setenv("QINIU_KODO_S3_BUCKET", testCase.bucket)
			t.Setenv("QINIU_KODO_SERVER_SIDE_ENCRYPTION", testCase.encryption)

			_, err := LoadObjectStorage()
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("LoadObjectStorage() error = %v, want %v", err, testCase.expected)
			}
		})
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
			name:     "storage provider",
			key:      "OBJECT_STORAGE_PROVIDER",
			value:    "automatic",
			expected: ErrObjectStorageProvider,
		},
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
