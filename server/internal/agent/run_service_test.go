package agent

import "testing"

func TestRunConfigurationMatchesPersistedProviderModelAndBudget(t *testing.T) {
	configuration := RunConfiguration{
		Provider:           "qianwen",
		Model:              "qwen-test",
		MaxOutputTokens:    512,
		MaxInputCharacters: 12000,
	}
	run := Run{
		RequestedProvider: configuration.Provider,
		RequestedModel:    configuration.Model,
		MaxOutputTokens:   configuration.MaxOutputTokens,
	}
	tests := map[string]struct {
		configuration RunConfiguration
		want          bool
	}{
		"matches": {
			configuration: configuration,
			want:          true,
		},
		"provider drift": {
			configuration: func() RunConfiguration {
				changed := configuration
				changed.Provider = "qianwen_next"
				return changed
			}(),
			want: false,
		},
		"model drift": {
			configuration: func() RunConfiguration {
				changed := configuration
				changed.Model = "qwen-next"
				return changed
			}(),
			want: false,
		},
		"output budget drift": {
			configuration: func() RunConfiguration {
				changed := configuration
				changed.MaxOutputTokens++
				return changed
			}(),
			want: false,
		},
		"context budget does not change the persisted request": {
			configuration: func() RunConfiguration {
				changed := configuration
				changed.MaxInputCharacters++
				return changed
			}(),
			want: true,
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
