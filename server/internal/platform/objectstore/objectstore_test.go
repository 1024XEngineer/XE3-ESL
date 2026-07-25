package objectstore

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestPutRequestRequiresReplayableBody(t *testing.T) {
	field, ok := reflect.TypeFor[PutRequest]().FieldByName("Body")
	if !ok {
		t.Fatal("PutRequest.Body field is missing")
	}
	if field.Type != reflect.TypeFor[io.ReadSeeker]() {
		t.Fatalf("PutRequest.Body type = %v, want io.ReadSeeker", field.Type)
	}
}

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
