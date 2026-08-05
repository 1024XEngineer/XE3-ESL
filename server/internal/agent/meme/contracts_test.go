package meme

import (
	"testing"
	"time"
)

func TestConfigValidationKeepsEnabledMemeEnrichmentBounded(t *testing.T) {
	valid := Config{
		Enabled:             true,
		SendProbability:     1,
		MaxPerMessage:       1,
		AvoidRecentCount:    3,
		ClassificationLimit: 3 * time.Second,
		DefaultCategory:     "neutral",
		PackID:              "speakup-default",
		PackVersion:         "1.0.0",
	}
	if !valid.Valid() {
		t.Fatal("enabled default meme config must be valid")
	}

	tests := map[string]Config{
		"probability above one": func() Config {
			value := valid
			value.SendProbability = 1.01
			return value
		}(),
		"unbounded count": func() Config {
			value := valid
			value.MaxPerMessage = 5
			return value
		}(),
		"missing classification limit": func() Config {
			value := valid
			value.ClassificationLimit = 0
			return value
		}(),
		"missing pack": func() Config {
			value := valid
			value.PackID = ""
			return value
		}(),
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			if config.Valid() {
				t.Fatalf("Config.Valid() = true for %#v", config)
			}
		})
	}
}

func TestDisabledMemeConfigDoesNotRequireRuntimeDependencies(t *testing.T) {
	if !(Config{}).Valid() {
		t.Fatal("disabled zero-value config must remain valid")
	}
}
