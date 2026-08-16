package interaction

import (
	"errors"
	"testing"
)

func TestMapVoiceError(t *testing.T) {
	for _, test := range []struct {
		input error
		want  error
	}{
		{ErrPersistenceInvalid, ErrVoiceRoundInvalid},
		{ErrPersistenceNotFound, ErrVoiceRoundNotFound},
		{ErrPersistenceConflict, ErrVoiceRoundConflict},
	} {
		if got := mapPersistenceError(test.input); !errors.Is(got, test.want) {
			t.Fatalf("mapPersistenceError(%v) = %v", test.input, got)
		}
	}
}

func TestNewRuntimeApplicationsRequiresDependencies(t *testing.T) {
	if _, _, err := NewRuntimeApplications(RuntimeConfiguration{}); err == nil {
		t.Fatal("NewRuntimeApplications accepted empty configuration")
	}
}
