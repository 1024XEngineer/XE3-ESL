package config

import (
	"fmt"
	"time"
)

const (
	defaultTemporaryAudioLifetime          = 2 * time.Minute
	defaultTemporaryAudioMaxItems          = 4
	defaultTemporaryAudioMaxBytes          = 32 * 1024 * 1024
	defaultTemporaryAudioMaxItemsPerUser   = 1
	defaultTemporaryAudioMaxBytesPerUser   = 8 * 1024 * 1024
	defaultTemporaryAudioConcurrent        = 2
	defaultTemporaryAudioConcurrentPerUser = 1
	defaultVoiceAudioReadTimeout           = 15 * time.Second
	defaultVoiceRecordedAudioReadTimeout   = 60 * time.Second

	maximumTemporaryAudioLifetime   = 10 * time.Minute
	maximumTemporaryAudioMaxItems   = 1024
	maximumTemporaryAudioMaxBytes   = 512 * 1024 * 1024
	maximumTemporaryAudioConcurrent = 32
	maximumVoiceAudioReadTimeout    = time.Minute
)

type TemporaryAudioConfig struct {
	Lifetime                     time.Duration
	MaxItems                     int
	MaxBytes                     int64
	MaxItemsPerUser              int
	MaxBytesPerUser              int64
	MaxConcurrentCaptures        int
	MaxConcurrentCapturesPerUser int
	ReadTimeout                  time.Duration
	RecordedReadTimeout          time.Duration
}

func LoadTemporaryAudio() (TemporaryAudioConfig, error) {
	lifetime, err := durationOrDefault(
		"VOICE_TEMP_AUDIO_LIFETIME",
		defaultTemporaryAudioLifetime,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	recordedReadTimeout, err := durationOrDefault(
		"VOICE_RECORDED_AUDIO_READ_TIMEOUT",
		defaultVoiceRecordedAudioReadTimeout,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxItems, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_ITEMS",
		defaultTemporaryAudioMaxItems,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxBytes, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_BYTES",
		defaultTemporaryAudioMaxBytes,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxItemsPerUser, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_ITEMS_PER_USER",
		defaultTemporaryAudioMaxItemsPerUser,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxBytesPerUser, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_BYTES_PER_USER",
		defaultTemporaryAudioMaxBytesPerUser,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxConcurrent, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES",
		defaultTemporaryAudioConcurrent,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	maxConcurrentPerUser, err := positiveIntOrDefault(
		"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER",
		defaultTemporaryAudioConcurrentPerUser,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	readTimeout, err := durationOrDefault(
		"VOICE_AUDIO_READ_TIMEOUT",
		defaultVoiceAudioReadTimeout,
	)
	if err != nil {
		return TemporaryAudioConfig{}, err
	}
	switch {
	case lifetime <= 0 || lifetime > maximumTemporaryAudioLifetime:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_LIFETIME must be greater than zero and at most %s",
			maximumTemporaryAudioLifetime,
		)
	case maxItems > maximumTemporaryAudioMaxItems:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_ITEMS must be at most %d",
			maximumTemporaryAudioMaxItems,
		)
	case maxBytes > maximumTemporaryAudioMaxBytes:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_BYTES must be at most %d",
			maximumTemporaryAudioMaxBytes,
		)
	case maxItemsPerUser > maxItems:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_ITEMS_PER_USER must not exceed VOICE_TEMP_AUDIO_MAX_ITEMS",
		)
	case maxBytesPerUser > maxBytes:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_BYTES_PER_USER must not exceed VOICE_TEMP_AUDIO_MAX_BYTES",
		)
	case maxConcurrent > maximumTemporaryAudioConcurrent:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES must be at most %d",
			maximumTemporaryAudioConcurrent,
		)
	case maxConcurrentPerUser > maxConcurrent:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES_PER_USER must not exceed VOICE_TEMP_AUDIO_MAX_CONCURRENT_CAPTURES",
		)
	case readTimeout <= 0 || readTimeout > maximumVoiceAudioReadTimeout:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_AUDIO_READ_TIMEOUT must be greater than zero and at most %s",
			maximumVoiceAudioReadTimeout,
		)
	case recordedReadTimeout <= 0 ||
		recordedReadTimeout > maximumVoiceAudioReadTimeout:
		return TemporaryAudioConfig{}, fmt.Errorf(
			"VOICE_RECORDED_AUDIO_READ_TIMEOUT must be greater than zero and at most %s",
			maximumVoiceAudioReadTimeout,
		)
	}
	return TemporaryAudioConfig{
		Lifetime:                     lifetime,
		MaxItems:                     maxItems,
		MaxBytes:                     int64(maxBytes),
		MaxItemsPerUser:              maxItemsPerUser,
		MaxBytesPerUser:              int64(maxBytesPerUser),
		MaxConcurrentCaptures:        maxConcurrent,
		MaxConcurrentCapturesPerUser: maxConcurrentPerUser,
		ReadTimeout:                  readTimeout,
		RecordedReadTimeout:          recordedReadTimeout,
	}, nil
}
