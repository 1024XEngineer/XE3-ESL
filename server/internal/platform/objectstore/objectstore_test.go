package objectstore

import (
	"errors"
	"testing"
)

func TestValidateKeyRequiresConfiguredPrefix(t *testing.T) {
	if err := ValidateKey("audio/v1", "audio/v1/assets/example.wav"); err != nil {
		t.Fatalf("ValidateKey() error = %v", err)
	}

	for _, key := range []string{
		"",
		"/audio/v1/assets/example.wav",
		"audio/v1/../secret",
		"audio/v10/example.wav",
		"other/example.wav",
		`audio\v1\example.wav`,
	} {
		t.Run(key, func(t *testing.T) {
			if err := ValidateKey("audio/v1", key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("ValidateKey(%q) error = %v", key, err)
			}
		})
	}
}
