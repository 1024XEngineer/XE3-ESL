package bootstrap

import (
	"errors"
	"testing"

	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
)

func TestMapConversationError(t *testing.T) {
	for _, test := range []struct {
		input error
		want  error
	}{
		{practiceinput.ErrPersistenceInvalid, practiceinput.ErrVoiceRoundInvalid},
		{practiceinput.ErrPersistenceNotFound, practiceinput.ErrVoiceRoundNotFound},
		{practiceinput.ErrPersistenceConflict, practiceinput.ErrVoiceRoundConflict},
	} {
		if got := mapConversationError(test.input); !errors.Is(got, test.want) {
			t.Fatalf("mapConversationError(%v) = %v", test.input, got)
		}
	}
}

func TestBuildVoiceApplicationRequiresPracticePorts(t *testing.T) {
	if _, err := buildVoiceApplication(VoiceConfiguration{}); err == nil {
		t.Fatal("buildVoiceApplication accepted empty configuration")
	}
}
