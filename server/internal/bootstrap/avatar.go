package bootstrap

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/avatar"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/spatius"
)

func NewAvatarTokenProvider(
	configuration config.SpatiusConfig,
) (avatar.TokenProvider, error) {
	if !configuration.Enabled {
		return nil, nil
	}
	return spatius.NewClient(spatius.Config{
		Enabled:        configuration.Enabled,
		ConsoleBaseURL: configuration.ConsoleBaseURL,
		APIKey:         configuration.APIKey.Reveal(),
		Timeout:        configuration.Timeout,
	})
}
