package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/meme"
)

const (
	defaultMemePackID      = "official-001"
	defaultMemePackVersion = "1.0.0"
)

type MemeConfig struct {
	AssetRoot string
	Runtime   meme.Config
}

func LoadMeme() (MemeConfig, error) {
	enabled, err := boolOrDefault("AGENT_MEME_ENABLED", true)
	if err != nil {
		return MemeConfig{}, err
	}
	probability, err := floatOrDefault("AGENT_MEME_SEND_PROBABILITY", 1)
	if err != nil {
		return MemeConfig{}, err
	}
	maximum, err := nonNegativeIntOrDefault("AGENT_MEME_MAX_PER_MESSAGE", 1)
	if err != nil {
		return MemeConfig{}, err
	}
	recent, err := nonNegativeIntOrDefault("AGENT_MEME_AVOID_RECENT_COUNT", 12)
	if err != nil {
		return MemeConfig{}, err
	}
	timeout, err := durationOrDefault("AGENT_MEME_CLASSIFICATION_TIMEOUT", 8*time.Second)
	if err != nil {
		return MemeConfig{}, err
	}
	result := MemeConfig{
		AssetRoot: valueOrDefault("AGENT_MEME_ASSET_ROOT", "assets/memes"),
		Runtime: meme.Config{
			Enabled: enabled, SendProbability: probability,
			MaxPerMessage: maximum, AvoidRecentCount: recent,
			ClassificationLimit: timeout,
			DefaultCategory:     meme.Category(valueOrDefault("AGENT_MEME_DEFAULT_CATEGORY", "happy")),
			PackID:              valueOrDefault("AGENT_MEME_PACK_ID", defaultMemePackID),
			PackVersion:         valueOrDefault("AGENT_MEME_PACK_VERSION", defaultMemePackVersion),
		},
	}
	if strings.TrimSpace(result.AssetRoot) == "" || !result.Runtime.Valid() {
		return MemeConfig{}, fmt.Errorf("Agent Meme configuration is invalid")
	}
	return result, nil
}

func boolOrDefault(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func floatOrDefault(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return value, nil
}

func nonNegativeIntOrDefault(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}
