package run

import (
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
)

func TestRunConfigurationMatchesPersistedProviderModelAndBudget(t *testing.T) {
	configuration := core.RunConfiguration{
		Provider:           "qianwen",
		Model:              "qwen-test",
		MaxOutputTokens:    512,
		MaxInputCharacters: 12000,
	}
	run := core.Run{
		RequestedProvider:  configuration.Provider,
		RequestedModel:     configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
		MaxInputCharacters: configuration.MaxInputCharacters,
	}
	tests := map[string]struct {
		configuration core.RunConfiguration
		want          bool
	}{
		"matches": {
			configuration: configuration,
			want:          true,
		},
		"provider drift": {
			configuration: func() core.RunConfiguration {
				changed := configuration
				changed.Provider = "qianwen_next"
				return changed
			}(),
			want: false,
		},
		"model drift": {
			configuration: func() core.RunConfiguration {
				changed := configuration
				changed.Model = "qwen-next"
				return changed
			}(),
			want: false,
		},
		"output budget drift": {
			configuration: func() core.RunConfiguration {
				changed := configuration
				changed.MaxOutputTokens++
				return changed
			}(),
			want: false,
		},
		"context budget drift": {
			configuration: func() core.RunConfiguration {
				changed := configuration
				changed.MaxInputCharacters++
				return changed
			}(),
			want: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := runConfigurationMatches(
				run,
				test.configuration,
			); got != test.want {
				t.Fatalf(
					"runConfigurationMatches() = %v, want %v",
					got,
					test.want,
				)
			}
		})
	}
}
