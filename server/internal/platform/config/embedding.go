package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	EmbeddingProviderQianwen  = "qianwen"
	defaultEmbeddingTimeout   = 60 * time.Second
	defaultEmbeddingDimension = 1024
	memoryEmbeddingDimension  = 1024
)

type EmbeddingConfig struct {
	Provider   string
	BaseURL    string
	Model      string
	Dimensions int
	Timeout    time.Duration
	APIKey     Secret
}

func LoadEmbedding() (EmbeddingConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(
		os.Getenv("EMBEDDING_PROVIDER"),
	))
	if provider == "" {
		return EmbeddingConfig{}, errors.New("EMBEDDING_PROVIDER is required")
	}
	if provider != EmbeddingProviderQianwen {
		return EmbeddingConfig{}, errors.New(
			"EMBEDDING_PROVIDER is not supported",
		)
	}
	baseURL := strings.TrimSpace(os.Getenv("QIANWEN_EMBEDDING_BASE_URL"))
	if baseURL == "" {
		return EmbeddingConfig{}, errors.New(
			"QIANWEN_EMBEDDING_BASE_URL is required",
		)
	}
	model := strings.TrimSpace(os.Getenv("QIANWEN_EMBEDDING_MODEL"))
	if model == "" {
		return EmbeddingConfig{}, errors.New(
			"QIANWEN_EMBEDDING_MODEL is required",
		)
	}
	dimensions, err := positiveIntOrDefault(
		"QIANWEN_EMBEDDING_DIMENSIONS",
		defaultEmbeddingDimension,
	)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	if dimensions != memoryEmbeddingDimension {
		return EmbeddingConfig{}, fmt.Errorf(
			"QIANWEN_EMBEDDING_DIMENSIONS must be %d for the current Memory index",
			memoryEmbeddingDimension,
		)
	}
	timeout, err := durationOrDefault(
		"QIANWEN_EMBEDDING_TIMEOUT",
		defaultEmbeddingTimeout,
	)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	if timeout <= 0 || timeout > maximumTextTimeout {
		return EmbeddingConfig{}, fmt.Errorf(
			"QIANWEN_EMBEDDING_TIMEOUT must be greater than zero and at most %s",
			maximumTextTimeout,
		)
	}
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		return EmbeddingConfig{}, errors.New("DASHSCOPE_API_KEY is required")
	}
	if strings.IndexFunc(apiKey, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return EmbeddingConfig{}, errors.New(
			"DASHSCOPE_API_KEY contains whitespace or control characters",
		)
	}
	return EmbeddingConfig{
		Provider:   provider,
		BaseURL:    baseURL,
		Model:      model,
		Dimensions: dimensions,
		Timeout:    timeout,
		APIKey:     Secret{value: apiKey},
	}, nil
}
