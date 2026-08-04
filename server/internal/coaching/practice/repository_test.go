package practice

import (
	"errors"
	"testing"
)

func TestActiveSessionConflictPreservesConflictClassification(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrActiveSessionConflict, ErrActiveSessionConflict) {
		t.Fatal("active session conflict does not match its specific error")
	}
	if !errors.Is(ErrActiveSessionConflict, ErrConflict) {
		t.Fatal("active session conflict does not match the generic conflict")
	}
	if errors.Is(ErrConflict, ErrActiveSessionConflict) {
		t.Fatal("generic conflict unexpectedly matches active session conflict")
	}
}
