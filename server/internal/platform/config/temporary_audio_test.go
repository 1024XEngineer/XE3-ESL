package config

import (
	"testing"
	"time"
)

func TestLoadTemporaryAudioUsesSafeDefaultsAndOverrides(t *testing.T) {
	clearTemporaryAudioEnvironment(t)
	defaults, err := LoadTemporaryAudio()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if defaults.Lifetime != 2*time.Minute ||
		defaults.MaxItems != 4 ||
		defaults.MaxBytes != 32*1024*1024 ||
		defaults.MaxItemsPerUser != 1 ||
		defaults.MaxBytesPerUser != 8*1024*1024 ||
		defaults.MaxConcurrentCaptures != 2 ||
		defaults.MaxConcurrentCapturesPerUser != 1 ||
		defaults.ReadTimeout != 15*time.Second ||
		defaults.RecordedReadTimeout != 60*time.Second {
		t.Fatalf("temporary audio defaults = %+v", defaults)
	}

	t.Setenv("VOICE_TEMP_AUDIO_LIFETIME", "90s")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_ITEMS", "12")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_BYTES", "48000000")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_ITEMS_PER_USER", "3")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_BYTES_PER_USER", "16000000")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES", "4")
	t.Setenv("VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER", "2")
	t.Setenv("VOICE_AUDIO_READ_TIMEOUT", "20s")
	t.Setenv("VOICE_RECORDED_AUDIO_READ_TIMEOUT", "45s")
	overrides, err := LoadTemporaryAudio()
	if err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if overrides.Lifetime != 90*time.Second ||
		overrides.MaxItems != 12 ||
		overrides.MaxBytes != 48_000_000 ||
		overrides.MaxItemsPerUser != 3 ||
		overrides.MaxBytesPerUser != 16_000_000 ||
		overrides.MaxConcurrentCaptures != 4 ||
		overrides.MaxConcurrentCapturesPerUser != 2 ||
		overrides.ReadTimeout != 20*time.Second ||
		overrides.RecordedReadTimeout != 45*time.Second {
		t.Fatalf("temporary audio overrides = %+v", overrides)
	}
}

func TestLoadTemporaryAudioRejectsUnsafeConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "oversized memory budget",
			key:   "VOICE_TEMP_AUDIO_MAX_BYTES",
			value: "536870913",
		},
		{
			name:  "unbounded concurrency",
			key:   "VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES",
			value: "33",
		},
		{
			name:  "read timeout",
			key:   "VOICE_AUDIO_READ_TIMEOUT",
			value: "61s",
		},
		{
			name:  "recorded read timeout",
			key:   "VOICE_RECORDED_AUDIO_READ_TIMEOUT",
			value: "61s",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearTemporaryAudioEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadTemporaryAudio(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}

	t.Run("per-user concurrency exceeds global", func(t *testing.T) {
		clearTemporaryAudioEnvironment(t)
		t.Setenv("VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES", "2")
		t.Setenv("VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER", "3")
		if _, err := LoadTemporaryAudio(); err == nil {
			t.Fatal("expected per-user concurrency error")
		}
	})
}

func clearTemporaryAudioEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"VOICE_TEMP_AUDIO_LIFETIME",
		"VOICE_TEMP_AUDIO_MAX_ITEMS",
		"VOICE_TEMP_AUDIO_MAX_BYTES",
		"VOICE_TEMP_AUDIO_MAX_ITEMS_PER_USER",
		"VOICE_TEMP_AUDIO_MAX_BYTES_PER_USER",
		"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES",
		"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER",
		"VOICE_AUDIO_READ_TIMEOUT",
		"VOICE_RECORDED_AUDIO_READ_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
