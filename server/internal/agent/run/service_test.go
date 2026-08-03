package run

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSubmitWithImagesRequiresImageInputCapability(t *testing.T) {
	service := &Service{}
	_, err := service.SubmitWithImages(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		"30000000-0000-4000-8000-000000000001",
		"image-message-1",
		"Please review this image.",
		[]string{"40000000-0000-4000-8000-000000000001"},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("SubmitWithImages error = %v, want %v", err, ErrInvalidRequest)
	}
}

func TestRunConfigurationMatchesPersistedProviderModelAndBudget(t *testing.T) {
	configuration := Configuration{
		Provider:           "qianwen",
		Model:              "qwen-test",
		MaxOutputTokens:    512,
		MaxInputCharacters: 12000,
	}
	run := Run{
		RequestedProvider:  configuration.Provider,
		RequestedModel:     configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
		MaxInputCharacters: configuration.MaxInputCharacters,
	}
	tests := map[string]struct {
		configuration Configuration
		want          bool
	}{
		"matches": {
			configuration: configuration,
			want:          true,
		},
		"provider drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.Provider = "qianwen_next"
				return changed
			}(),
			want: false,
		},
		"model drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.Model = "qwen-next"
				return changed
			}(),
			want: false,
		},
		"output budget drift": {
			configuration: func() Configuration {
				changed := configuration
				changed.MaxOutputTokens++
				return changed
			}(),
			want: false,
		},
		"context budget drift": {
			configuration: func() Configuration {
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
