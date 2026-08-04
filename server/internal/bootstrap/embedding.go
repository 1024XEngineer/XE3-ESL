package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

func NewEmbedder(configuration config.EmbeddingConfig) (ai.Embedder, error) {
	switch configuration.Provider {
	case config.EmbeddingProviderQianwen:
		return qianwen.NewEmbeddingClient(
			qianwen.EmbeddingConfig{
				BaseURL:    configuration.BaseURL,
				Model:      configuration.Model,
				Dimensions: configuration.Dimensions,
				Timeout:    configuration.Timeout,
			},
			configuration.APIKey.Reveal(),
		)
	default:
		return nil, errors.New(
			"bootstrap: embedding provider is not registered",
		)
	}
}
