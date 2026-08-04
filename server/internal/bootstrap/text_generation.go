package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
)

// NewTextGenerator is the production provider registration boundary. Business
// modules receive only ai.TextGenerator and cannot select a provider or fall
// back to a Fake implementation at runtime.
func NewTextGenerator(
	configuration config.TextGenerationConfig,
) (ai.TextGenerator, error) {
	switch configuration.Provider {
	case config.TextProviderQianwen:
		return qianwen.New(
			qianwen.Config{
				BaseURL:         configuration.BaseURL,
				Model:           configuration.Model,
				Timeout:         configuration.Timeout,
				MaxOutputTokens: configuration.MaxOutputTokens,
			},
			configuration.APIKey.Reveal(),
		)
	default:
		return nil, errors.New(
			"bootstrap: text generation provider is not registered",
		)
	}
}
