package voice

import (
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestQuestionGenerationRequestAllowsUserControlledTurnsPastBlueprints(t *testing.T) {
	t.Parallel()
	session := Session{
		PracticeExperience: string(practice.PracticeExperienceLifeAndTravel),
		SceneCategory:      "LIFE_TRAVEL",
		PracticeMode:       string(practice.PracticeModeFullSimulation),
		TurnLimit:          0,
		CompletionMode:     practice.CompletionModeUserControlled,
		Prompt: practice.ScenePrompt{
			PublicSceneBrief: "Check in at a hotel.",
			PracticeGoal:     "Complete the check-in conversation.",
			UserRole:         "Guest",
			AIRole:           "Receptionist",
			PersonaSummary:   "Professional and helpful.",
			FocusAreas:       []string{"check_in"},
			TurnBlueprints:   []string{"Confirm the booking."},
		},
	}

	request, err := questionGenerationRequest(session, 65)
	if err != nil {
		t.Fatalf("questionGenerationRequest() error = %v", err)
	}
	if !strings.Contains(request.UserPrompt, "This is turn 65") ||
		!strings.Contains(request.UserPrompt, "learner chooses to finish") {
		t.Fatalf("user prompt = %q", request.UserPrompt)
	}
}
