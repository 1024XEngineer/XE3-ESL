package identity

import (
	"bytes"
	"testing"
)

func TestUUIDv4GeneratorSetsVersionAndVariant(t *testing.T) {
	generator := NewUUIDv4Generator(bytes.NewReader(make([]byte, 16)))
	got, err := generator.NewID()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("unexpected UUID: %q", got)
	}
}
