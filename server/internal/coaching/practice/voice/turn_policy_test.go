package voice

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestResolveTurnPolicyUsesExactRegisteredReference(t *testing.T) {
	tests := map[string]practice.TurnPolicyKind{
		practice.GenericPracticeTurnPolicy:             practice.TurnPolicyGenerated,
		practice.InterviewPracticeTurnPolicy:           practice.TurnPolicyInterview,
		practice.DailyHotelCheckinIssueTurnPolicy:      practice.TurnPolicyGenerated,
		practice.WorkplaceProgressRiskUpdateTurnPolicy: practice.TurnPolicyGenerated,
		practice.InterviewProjectDeepDiveTurnPolicy:    practice.TurnPolicyInterview,
		practice.IELTSSpeakingPart1TurnPolicy:          practice.TurnPolicyFrozenIELTS,
		practice.IELTSSpeakingPart2TurnPolicy:          practice.TurnPolicyFrozenIELTS,
		practice.IELTSSpeakingPart3TurnPolicy:          practice.TurnPolicyFrozenIELTS,
		practice.IELTSSpeakingFullMockTurnPolicy:       practice.TurnPolicyFrozenIELTS,
	}
	for reference, want := range tests {
		t.Run(reference, func(t *testing.T) {
			got, err := practice.ResolveTurnPolicy(reference)
			if err != nil || got.Kind != want {
				t.Fatalf("ResolveTurnPolicy(%q) = %#v, %v", reference, got, err)
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
			if _, err := practice.ResolveTurnPolicy(reference); !errors.Is(err, practice.ErrExecutionPolicyNotFound) {
				t.Fatalf("ResolveTurnPolicy(%q) error = %v", reference, err)
			}
		})
	}
}

func TestQuestionAdapterRoutesByTurnPolicyReference(t *testing.T) {
	t.Run("generic policy ignores Scene classification", func(t *testing.T) {
		repository := newTurnPolicyQuestionRepository()
		generator := &turnPolicyQuestionGenerator{
			response: "What outcome do you need?",
		}
		session := sessionFixture()
		session.TurnPolicyRef = practice.GenericPracticeTurnPolicy
		session.PracticeExperience = "IELTS_SPEAKING"
		session.SceneCategory = "IELTS_SPEAKING"
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

	t.Run("interview policy ignores Scene classification", func(t *testing.T) {
		repository := newTurnPolicyQuestionRepository()
		repository.history = []practice.Question{{
			ID:   "primary-1",
			Type: "PRIMARY",
		}}
		generator := &turnPolicyQuestionGenerator{
			response: `{"question_type":"FOLLOW_UP","content":"What changed after launch?"}`,
		}
		session := sessionFixture()
		session.TurnPolicyRef = practice.InterviewProjectDeepDiveTurnPolicy
		session.PracticeExperience = "LIFE_AND_TRAVEL"
		session.SceneCategory = "LIFE_DAILY"
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
		session.TurnPolicyRef = practice.IELTSSpeakingPart1TurnPolicy
		session.PracticeExperience = "LIFE_AND_TRAVEL"
		session.SceneCategory = "LIFE_DAILY"
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

func TestQuestionAdapterUsesFrozenPreparationContext(t *testing.T) {
	repository := newTurnPolicyQuestionRepository()
	generator := &turnPolicyQuestionGenerator{
		response: "Dad, what brings you to the store today?",
	}
	session := sessionFixture()
	session.TurnPolicyRef = practice.GenericPracticeTurnPolicy
	session.ScenarioContext = &practice.ScenarioPreparationContext{
		Situation:          "Report project progress to a direct manager.",
		UserRole:           "项目负责人",
		CounterpartRole:    "项目负责人的男朋友",
		Goal:               "Explain status, risks, and the requested decision.",
		CounterpartPersona: "A direct manager who asks for evidence.",
	}

	_, err := (&questionAdapter{
		repository: repository,
		generator:  generator,
	}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 1)
	if err != nil {
		t.Fatalf("EnsureQuestion: %v", err)
	}
	if generator.request.SystemPrompt != preparationContextQuestionSystemPrompt ||
		!strings.Contains(generator.request.SystemPrompt, "Respond only in English.") ||
		!strings.Contains(generator.request.UserPrompt, `"situation":"Report project progress to a direct manager."`) ||
		!strings.Contains(generator.request.UserPrompt, `"user_role":"项目负责人"`) ||
		!strings.Contains(generator.request.UserPrompt, `"counterpart_role":"项目负责人的男朋友"`) ||
		!strings.Contains(generator.request.UserPrompt, `"goal":"Explain status, risks, and the requested decision."`) ||
		!strings.Contains(generator.request.UserPrompt, `"counterpart_persona":"A direct manager who asks for evidence."`) ||
		!strings.Contains(generator.request.UserPrompt, "Confirmed preparation context JSON") ||
		!strings.Contains(generator.request.UserPrompt, "Scene focus areas (secondary; adapt if conflicting)") ||
		!strings.Contains(generator.request.UserPrompt, "Scene turn blueprint (secondary; adapt if conflicting)") ||
		strings.Contains(generator.request.SystemPrompt, "项目负责人的男朋友") {
		t.Fatalf("preparation context generation request = %#v", generator.request)
	}
}

func TestGeneratedPoliciesUsePreparationContextAuthority(t *testing.T) {
	for _, reference := range []string{
		practice.GenericPracticeTurnPolicy,
		practice.DailyHotelCheckinIssueTurnPolicy,
		practice.WorkplaceProgressRiskUpdateTurnPolicy,
	} {
		t.Run(reference, func(t *testing.T) {
			repository := newTurnPolicyQuestionRepository()
			generator := &turnPolicyQuestionGenerator{response: "What would you like to discuss?"}
			session := sessionFixture()
			session.TurnPolicyRef = reference
			session.ScenarioContext = &practice.ScenarioPreparationContext{
				Situation:          "A private conversation unrelated to the default Scene.",
				UserRole:           "the learner's sibling",
				CounterpartRole:    "the learner's older brother",
				Goal:               "Resolve a family misunderstanding.",
				CounterpartPersona: "Direct and evidence-seeking like the default manager.",
			}

			_, err := (&questionAdapter{
				repository: repository,
				generator:  generator,
			}).EnsureQuestion(context.Background(), persistenceRequestActor(), session, 1)
			if err != nil {
				t.Fatalf("EnsureQuestion: %v", err)
			}
			if generator.request.SystemPrompt != preparationContextQuestionSystemPrompt ||
				!strings.Contains(generator.request.UserPrompt, `"counterpart_role":"the learner's older brother"`) {
				t.Fatalf("preparation context authority request = %#v", generator.request)
			}
		})
	}
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

func TestSessionAdapterRejectsUnknownPolicyBeforeActivation(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(
		"unknown.practice.turn.v1",
	)
	repository := &turnPolicySessionRepository{bootstrap: bootstrap}

	_, err := (&sessionAdapter{repository: repository}).Start(
		context.Background(),
		persistenceRequestActor(),
		bootstrap.Session.ID,
		"voice-start-key",
	)
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Start error = %v", err)
	}
	if repository.activateCalls != 0 {
		t.Fatalf("ActivateSession calls = %d", repository.activateCalls)
	}
}

func TestMapPracticeSessionFreezesTurnPolicyReference(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(
		practice.InterviewProjectDeepDiveTurnPolicy,
	)
	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	if mapped.TurnPolicyRef != practice.InterviewProjectDeepDiveTurnPolicy {
		t.Fatalf("TurnPolicyRef = %q", mapped.TurnPolicyRef)
	}

	bootstrap.Snapshot.SceneSelection.Scene.PracticeOptions[0].TurnPolicyRef =
		"unknown.practice.turn.v1"
	if _, err := mapPracticeSession(
		bootstrap,
		"user-1",
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("unknown TurnPolicyRef error = %v", err)
	}
}

func TestMapPracticeSessionUsesFrozenSelectedInterviewRole(t *testing.T) {
	t.Parallel()
	bootstrap := turnPolicySessionBootstrap(
		practice.InterviewProjectDeepDiveTurnPolicy,
	)
	bootstrap.Snapshot.SceneSelection.Scene.PracticeOptions[0].Mode =
		practice.PracticeModeFocus
	bootstrap.Session.PracticeMode = practice.PracticeModeFocus
	bootstrap.Snapshot.PracticeMode = practice.PracticeModeFocus
	role := &bootstrap.Snapshot.SceneSelection.Scene.Roles[0]
	role.DisplayName = "招聘专员"
	role.Responsibilities = "Explore motivation and communication clarity."
	role.Style = "Warm and structured."
	role.PracticeObjectives = []practice.PracticeObjectiveDefinition{
		{ID: "motivation", Description: "Explain authentic motivation."},
		{ID: "communication", Description: "Communicate ideas clearly."},
	}
	bootstrap.Snapshot.Participants[0].RoleSnapshot = role

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	if mapped.Prompt.AIRole != "招聘专员" ||
		!strings.Contains(mapped.Prompt.PersonaSummary, "Warm and structured") ||
		len(mapped.Prompt.FocusAreas) != 2 ||
		mapped.Prompt.FocusAreas[0] != "Explain authentic motivation." ||
		len(mapped.Prompt.TurnBlueprints) != 2 ||
		!strings.Contains(mapped.Prompt.TurnBlueprints[1], "Communicate ideas clearly") {
		t.Fatalf("selected role prompt = %#v", mapped.Prompt)
	}
	request, err := questionGenerationRequest(mapped, 1)
	if err != nil {
		t.Fatalf("questionGenerationRequest: %v", err)
	}
	if !strings.Contains(request.SystemPrompt, "You are 招聘专员") ||
		!strings.Contains(request.UserPrompt, "Explain authentic motivation") ||
		strings.Contains(request.UserPrompt, "Probe impact") {
		t.Fatalf("selected role generation request = %#v", request)
	}
}

func TestSelectedInterviewRoleKeepsFullSimulationBlueprint(t *testing.T) {
	t.Parallel()
	prompt := sessionFixture().Prompt
	wantBlueprints := append([]string(nil), prompt.TurnBlueprints...)
	role := practice.RoleDefinition{
		ID:               "role-recruiter",
		DisplayName:      "招聘专员",
		Responsibilities: "Explore motivation and communication clarity.",
		Style:            "Warm and structured.",
		PracticeObjectives: []practice.PracticeObjectiveDefinition{{
			ID:          "motivation",
			Description: "Explain authentic motivation.",
		}},
	}

	if !applySelectedInterviewRole(
		&prompt,
		role,
		practice.PracticeModeFullSimulation,
	) {
		t.Fatal("applySelectedInterviewRole rejected a valid role")
	}
	if prompt.AIRole != "招聘专员" ||
		!reflect.DeepEqual(prompt.TurnBlueprints, wantBlueprints) {
		t.Fatalf("full simulation prompt = %#v", prompt)
	}
}

func TestMapPracticeSessionKeepsMultiRoleInterviewCompatible(t *testing.T) {
	t.Parallel()
	bootstrap := turnPolicySessionBootstrap(
		practice.InterviewProjectDeepDiveTurnPolicy,
	)
	role := bootstrap.Snapshot.SceneSelection.Scene.Roles[0]
	role.ID = "role-2"
	bootstrap.Snapshot.SceneSelection.Scene.Roles = append(
		bootstrap.Snapshot.SceneSelection.Scene.Roles,
		role,
	)
	bootstrap.Snapshot.SceneSelection.SelectedRoleIDs = append(
		bootstrap.Snapshot.SceneSelection.SelectedRoleIDs,
		role.ID,
	)
	bootstrap.Snapshot.Participants = append(
		bootstrap.Snapshot.Participants,
		practice.Participant{
			ID:               "facilitator-2",
			SessionID:        "session-1",
			Role:             "FACILITATOR",
			SubjectRef:       practice.SubjectRef{Namespace: "speakup.role", SubjectID: role.ID},
			RoleDefinitionID: role.ID,
			RoleSnapshot:     &role,
			Order:            3,
		},
	)

	if _, err := mapPracticeSession(bootstrap, "user-1"); err != nil {
		t.Fatalf("mapPracticeSession multi-role interview: %v", err)
	}
}

func TestMapPracticeSessionCopiesFrozenIELTSParts(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(
		practice.IELTSSpeakingPart1TurnPolicy,
	)
	blueprints := []string{"Part 1 question: What do you do?"}
	definition := &bootstrap.Snapshot.SceneSelection.Scene
	definition.Experience = practice.PracticeExperienceIELTSSpeaking
	definition.Category = practice.SceneCategory("IELTS_SPEAKING")
	definition.Prompt.TurnBlueprints = append([]string(nil), blueprints...)
	definition.PracticeOptions[0].Mode = practice.PracticeModePart1
	definition.PracticeOptions[0].SessionPolicyRef =
		practice.IELTSSpeakingPart1SessionPolicy
	bootstrap.Session.Experience = definition.Experience
	bootstrap.Session.Category = definition.Category
	bootstrap.Session.PracticeMode = practice.PracticeModePart1
	bootstrap.Snapshot.Experience = definition.Experience
	bootstrap.Snapshot.Category = definition.Category
	bootstrap.Snapshot.PracticeMode = practice.PracticeModePart1
	bootstrap.Snapshot.SessionPolicy.MinEffectiveTurns = 1
	bootstrap.Snapshot.SessionPolicy.MaxEffectiveTurns = 1
	bootstrap.Snapshot.SessionPolicy.CoverageCheckpointTurn = 1
	bootstrap.Snapshot.IELTSAssignment = &practice.IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   practice.PracticeModePart1,
		Parts: []practice.IELTSPart{{
			Part:           practice.PracticeModePart1,
			SourceID:       "part-1-set-1",
			TurnBlueprints: append([]string(nil), blueprints...),
		}},
	}

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	bootstrap.Snapshot.IELTSAssignment.Parts[0].TurnBlueprints[0] = "changed"
	if mapped.IELTSAssignment == nil ||
		mapped.IELTSAssignment.Parts[0].TurnBlueprints[0] != blueprints[0] {
		t.Fatalf("mapped IELTS assignment = %#v", mapped.IELTSAssignment)
	}
}

func TestMapPracticeSessionCopiesFrozenScenarioPreparation(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(practice.GenericPracticeTurnPolicy)
	context := &practice.ScenarioPreparationContext{
		Situation:          "Discuss a return at the store.",
		UserRole:           "店员爸爸",
		CounterpartRole:    "店员",
		Goal:               "Resolve the return request.",
		CounterpartPersona: "A helpful store assistant.",
	}
	bootstrap.Snapshot.Preparation.Kind = "scenario"
	bootstrap.Snapshot.Preparation.ScenarioContext = context

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	context.UserRole = "changed"
	if mapped.ScenarioContext == nil ||
		mapped.ScenarioContext.UserRole != "店员爸爸" {
		t.Fatalf("ScenarioContext = %#v", mapped.ScenarioContext)
	}
}

func turnPolicySessionBootstrap(
	turnPolicyRef string,
) practice.SessionBootstrap {
	role := practice.RoleDefinition{
		ID:               "role-1",
		SceneID:          "scene-1",
		DisplayName:      "技术面试官",
		Responsibilities: "Probe technical depth and trade-offs.",
		Style:            "Precise and evidence seeking.",
		PracticeObjectives: []practice.PracticeObjectiveDefinition{{
			ID:          "technical_tradeoff",
			Description: "Compare technical options and justify the trade-off.",
		}},
	}
	definition := practice.SceneDefinition{
		ID:         "scene-1",
		Experience: practice.PracticeExperienceInterview,
		Category:   practice.SceneCategory("INTERVIEW_PROFESSIONAL"),
		Version:    1,
		Status:     practice.SceneStatusActive,
		Prompt:     sessionFixture().Prompt,
		Roles:      []practice.RoleDefinition{role},
		PracticeOptions: []practice.PracticeOption{{
			ID:                       "option-1",
			SceneID:                  "scene-1",
			Mode:                     practice.PracticeModeFullSimulation,
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            turnPolicyRef,
			SessionPolicyRef:         practice.InterviewProjectDeepDiveSessionPolicy,
			EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
		}},
	}
	return practice.SessionBootstrap{
		Session: practice.Session{
			ID:                  "session-1",
			PlanID:              "plan-1",
			PlanRevision:        1,
			Experience:          definition.Experience,
			Category:            definition.Category,
			PracticeMode:        practice.PracticeModeFullSimulation,
			EvaluationPolicyRef: definition.PracticeOptions[0].EvaluationPolicyRef,
			SnapshotID:          "snapshot-1",
			Status:              practice.SessionStarting,
			Version:             1,
			EffectiveTurns:      0,
		},
		Snapshot: practice.SessionSnapshot{
			ID:           "snapshot-1",
			SessionID:    "session-1",
			PlanRevision: 1,
			Experience:   definition.Experience,
			Category:     definition.Category,
			PracticeMode: practice.PracticeModeFullSimulation,
			SceneSelection: practice.SceneSelection{
				Scene:            definition,
				SelectedRoleIDs:  []string{role.ID},
				PracticeOptionID: "option-1",
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
			SessionPolicy: practice.SessionPolicy{
				SuggestedDurationSeconds:   600,
				MinEffectiveTurns:          4,
				MaxEffectiveTurns:          6,
				CoverageCheckpointTurn:     4,
				MaxFollowUpsPerQuestion:    3,
				EarlyCompletionRule:        practice.EarlyCompletionCoverageSatisfiedAfterCheckpoint,
				QuestionTranslationAllowed: true,
				QuestionTipsAllowed:        true,
				AvatarAllowed:              true,
				SpeechFeedbackAllowed:      true,
			},
		},
	}
}

type turnPolicySessionRepository struct {
	bootstrap     practice.SessionBootstrap
	activateCalls int
}

func (repository *turnPolicySessionRepository) GetSession(
	context.Context,
	practice.Actor,
	string,
) (practice.Session, error) {
	return repository.bootstrap.Session, nil
}

func (repository *turnPolicySessionRepository) GetSessionSnapshot(
	context.Context,
	practice.Actor,
	string,
) (practice.SessionSnapshot, error) {
	return repository.bootstrap.Snapshot, nil
}

func (*turnPolicySessionRepository) ReplayVoiceStart(
	context.Context,
	practice.Actor,
	practice.IdempotencyIntent,
) (practice.SessionBootstrap, bool, error) {
	return practice.SessionBootstrap{}, false, nil
}

func (repository *turnPolicySessionRepository) ActivateSession(
	context.Context,
	practice.Actor,
	string,
	string,
	practice.IdempotencyIntent,
) (practice.SessionBootstrap, error) {
	repository.activateCalls++
	return repository.bootstrap, nil
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
	request  QuestionGenerationRequest
}

func (generator *turnPolicyQuestionGenerator) GenerateQuestion(
	_ context.Context,
	request QuestionGenerationRequest,
) (string, error) {
	generator.calls++
	generator.request = request
	return generator.response, nil
}

func persistenceRequestActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-1"}
}
