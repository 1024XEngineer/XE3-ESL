package voice

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestResolveTurnPolicyUsesExactRegisteredReference(t *testing.T) {
	tests := map[string]turnPolicy{
		genericPracticeTurnPolicy:             turnPolicyGenerated,
		dailyHotelCheckinIssueTurnPolicy:      turnPolicyGenerated,
		workplaceProgressRiskUpdateTurnPolicy: turnPolicyGenerated,
		interviewProjectDeepDiveTurnPolicy:    turnPolicyInterview,
		ieltsSpeakingPart1TurnPolicy:          turnPolicyFrozenIELTS,
		ieltsSpeakingPart2TurnPolicy:          turnPolicyFrozenIELTS,
		ieltsSpeakingPart3TurnPolicy:          turnPolicyFrozenIELTS,
		ieltsSpeakingFullMockTurnPolicy:       turnPolicyFrozenIELTS,
	}
	for reference, want := range tests {
		t.Run(reference, func(t *testing.T) {
			got, err := resolveTurnPolicy(reference)
			if err != nil || got != want {
				t.Fatalf("resolveTurnPolicy(%q) = %d, %v", reference, got, err)
			}
		})
	}
	for _, reference := range []string{
		"",
		" interview.project_deep_dive.turn.v1",
		"interview.test.turn.v1",
		"generic.practice.turn.v2",
	} {
		t.Run("reject "+reference, func(t *testing.T) {
			if _, err := resolveTurnPolicy(reference); !errors.Is(err, ErrInvalidContext) {
				t.Fatalf("resolveTurnPolicy(%q) error = %v", reference, err)
			}
		})
	}
}

func TestQuestionAdapterRoutesByTurnPolicyReference(t *testing.T) {
	t.Run("generic policy ignores interview family", func(t *testing.T) {
		repository := newTurnPolicyQuestionRepository()
		generator := &turnPolicyQuestionGenerator{
			response: "What outcome do you need?",
		}
		session := sessionFixture()
		session.TurnPolicyRef = genericPracticeTurnPolicy
		session.SceneFamily = "INTERVIEW"
		session.SceneModel = "IELTS_SPEAKING_FULL_MOCK"
		session.Prompt.TurnBlueprints = []string{"Open", "Clarify"}

		question, err := (&questionAdapter{
			repository: repository,
			generator:  generator,
		}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 2)
		if err != nil {
			t.Fatalf("EnsureQuestion: %v", err)
		}
		if question.Type != "PRIMARY" ||
			question.Content != generator.response ||
			generator.calls != 1 || repository.listCalls != 0 {
			t.Fatalf("generic question = %#v, calls = %d/%d", question, generator.calls, repository.listCalls)
		}
	})

	t.Run("interview policy ignores daily family", func(t *testing.T) {
		repository := newTurnPolicyQuestionRepository()
		repository.history = []practice.Question{{
			ID:   "primary-1",
			Type: "PRIMARY",
		}}
		generator := &turnPolicyQuestionGenerator{
			response: `{"question_type":"FOLLOW_UP","content":"What changed after launch?"}`,
		}
		session := sessionFixture()
		session.TurnPolicyRef = interviewProjectDeepDiveTurnPolicy
		session.SceneFamily = "DAILY"
		session.SceneModel = "DAILY_BASIC_DIALOGUE"
		session.EffectiveTurns = 1
		session.PreviousQuestion = "What did you deliver?"
		session.PreviousUserResponse = "I led the launch."
		session.Prompt.TurnBlueprints = []string{"Open", "Explore impact"}

		question, err := (&questionAdapter{
			repository: repository,
			generator:  generator,
		}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 2)
		if err != nil {
			t.Fatalf("EnsureQuestion: %v", err)
		}
		if question.Type != "FOLLOW_UP" ||
			question.ParentQuestionID != "primary-1" ||
			question.Content != "What changed after launch?" ||
			generator.calls != 1 || repository.listCalls != 1 {
			t.Fatalf("interview question = %#v, calls = %d/%d", question, generator.calls, repository.listCalls)
		}
	})

	t.Run("IELTS policy does not call generator", func(t *testing.T) {
		repository := newTurnPolicyQuestionRepository()
		generator := &turnPolicyQuestionGenerator{response: "must not be used"}
		session := sessionFixture()
		session.TurnPolicyRef = ieltsSpeakingPart1TurnPolicy
		session.SceneFamily = "DAILY"
		session.SceneModel = "DAILY_BASIC_DIALOGUE"
		session.Prompt.TurnBlueprints = []string{"Part 1: What do you do?"}

		question, err := (&questionAdapter{
			repository: repository,
			generator:  generator,
		}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 1)
		if err != nil {
			t.Fatalf("EnsureQuestion: %v", err)
		}
		if question.Content != "What do you do?" || generator.calls != 0 {
			t.Fatalf("IELTS question = %#v, generator calls = %d", question, generator.calls)
		}
	})
}

func TestQuestionAdapterRejectsUnknownPolicyBeforeDependencies(t *testing.T) {
	repository := newTurnPolicyQuestionRepository()
	generator := &turnPolicyQuestionGenerator{response: "must not be used"}
	session := sessionFixture()
	session.TurnPolicyRef = "unknown.practice.turn.v1"

	_, err := (&questionAdapter{
		repository: repository,
		generator:  generator,
	}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 1)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("EnsureQuestion error = %v", err)
	}
	if repository.getCalls != 0 ||
		repository.saveCalls != 0 ||
		repository.listCalls != 0 ||
		generator.calls != 0 {
		t.Fatalf(
			"unknown policy reached dependencies: get=%d save=%d list=%d generator=%d",
			repository.getCalls,
			repository.saveCalls,
			repository.listCalls,
			generator.calls,
		)
	}
}

func TestMapPracticeSessionFreezesTurnPolicyReference(t *testing.T) {
	role := scene.RoleDefinition{ID: "role-1", SceneID: "scene-1"}
	definition := scene.SceneDefinition{
		ID:            "scene-1",
		Family:        scene.SceneFamilyInterview,
		Model:         scene.SceneModelProjectExperienceDeepDive,
		Version:       1,
		TurnPolicyRef: interviewProjectDeepDiveTurnPolicy,
		Prompt:        sessionFixture().Prompt,
		Roles:         []scene.RoleDefinition{role},
	}
	bootstrap := practice.SessionBootstrap{
		Session: practice.Session{
			ID:             "session-1",
			PlanID:         "plan-1",
			PlanRevision:   1,
			SceneFamily:    definition.Family,
			SceneModel:     definition.Model,
			SnapshotID:     "snapshot-1",
			Status:         practice.SessionStarting,
			Version:        1,
			EffectiveTurns: 0,
		},
		Snapshot: practice.SessionSnapshot{
			ID:           "snapshot-1",
			SessionID:    "session-1",
			PlanRevision: 1,
			SceneFamily:  definition.Family,
			SceneModel:   definition.Model,
			SceneSelection: scene.SelectionSnapshot{
				Scene:           definition,
				SelectedRoleIDs: []string{role.ID},
			},
			Participants: []practice.Participant{
				{
					ID:               "facilitator-1",
					SessionID:        "session-1",
					Role:             "FACILITATOR",
					SubjectRef:       practice.SubjectRef{Namespace: "speakup.role", SubjectID: role.ID},
					RoleDefinitionID: role.ID,
					RoleSnapshot:     &role,
					Order:            1,
				},
				{
					ID:        "learner-1",
					SessionID: "session-1",
					Role:      "LEARNER",
					SubjectRef: practice.SubjectRef{
						Namespace: "speakup.user",
						SubjectID: "user-1",
					},
					Order: 2,
				},
			},
			SessionPolicy: preparation.SessionPolicy{
				MaxEffectiveTurns:       3,
				MaxFollowUpsPerQuestion: 1,
			},
		},
	}

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	if mapped.TurnPolicyRef != definition.TurnPolicyRef {
		t.Fatalf("TurnPolicyRef = %q", mapped.TurnPolicyRef)
	}

	bootstrap.Snapshot.SceneSelection.Scene.TurnPolicyRef = "unknown.practice.turn.v1"
	if _, err := mapPracticeSession(bootstrap, "user-1"); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("unknown TurnPolicyRef error = %v", err)
	}
}

type turnPolicyQuestionRepository struct {
	questions map[string]practice.Question
	history   []practice.Question
	getCalls  int
	saveCalls int
	listCalls int
}

func newTurnPolicyQuestionRepository() *turnPolicyQuestionRepository {
	return &turnPolicyQuestionRepository{
		questions: make(map[string]practice.Question),
	}
}

func (repository *turnPolicyQuestionRepository) GetQuestion(
	_ context.Context,
	_ Actor,
	questionID string,
) (practice.Question, error) {
	repository.getCalls++
	question, found := repository.questions[questionID]
	if !found {
		return practice.Question{}, ErrPersistenceNotFound
	}
	return question, nil
}

func (repository *turnPolicyQuestionRepository) SaveQuestion(
	_ context.Context,
	_ Actor,
	question practice.Question,
) (practice.Question, error) {
	repository.saveCalls++
	repository.questions[question.ID] = question
	return question, nil
}

func (repository *turnPolicyQuestionRepository) ListSessionQuestions(
	_ context.Context,
	_ Actor,
	_ string,
) ([]practice.Question, error) {
	repository.listCalls++
	return append([]practice.Question(nil), repository.history...), nil
}

type turnPolicyQuestionGenerator struct {
	response string
	calls    int
}

func (generator *turnPolicyQuestionGenerator) GenerateQuestion(
	context.Context,
	QuestionGenerationRequest,
) (string, error) {
	generator.calls++
	return generator.response, nil
}

func persistenceRequestActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"}
}
