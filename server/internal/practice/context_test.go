package practice

import (
	"context"
	"errors"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestCreatePlanReplaysBeforeMutableContextValidation(t *testing.T) {
	t.Parallel()

	want := persistence.Plan{
		ID:            "plan-existing",
		AgentThreadID: "thread-1",
		MatterID:      "matter-1",
	}
	application := newContextTestApplication(t, &contextRepositoryStub{
		replayPlan: func(
			_ persistence.ContextIdempotencyIntent,
		) (persistence.Plan, bool, error) {
			return want, true, nil
		},
	})

	got, replayed, err := application.CreatePlan(
		context.Background(),
		contextActorFixture(),
		"intent-plan-0001",
		validContextPlanRequest(),
	)
	if err != nil {
		t.Fatalf("CreatePlan replay: %v", err)
	}
	if !replayed || got.ID != want.ID {
		t.Fatalf("CreatePlan replay = (%+v, %t), want existing result", got, replayed)
	}
}

func TestCreateSessionReplaysBeforeMutableContextValidation(t *testing.T) {
	t.Parallel()

	want := persistence.ContextSessionBootstrap{
		Session: persistence.ContextSession{ID: "session-existing"},
	}
	application := newContextTestApplication(t, &contextRepositoryStub{
		replaySession: func(
			_ persistence.ContextIdempotencyIntent,
		) (persistence.ContextSessionBootstrap, bool, error) {
			return want, true, nil
		},
	})

	got, replayed, err := application.CreateSession(
		context.Background(),
		contextActorFixture(),
		"plan-existing",
		"intent-session-0001",
		CreateSessionRequest{
			ExpectedPlanRevision:  1,
			PreparationSnapshotID: "preparation-snapshot-1",
			PracticeOptionID:      "option_full_simulation",
			RoleDefinitionIDs:     []string{"role_technical_interviewer"},
		},
	)
	if err != nil {
		t.Fatalf("CreateSession replay: %v", err)
	}
	if !replayed || got.Session.ID != want.Session.ID {
		t.Fatalf(
			"CreateSession replay = (%+v, %t), want existing result",
			got,
			replayed,
		)
	}
}

func TestInterviewSessionRequiresExactlyOneInterviewerRole(t *testing.T) {
	t.Parallel()

	application := newContextTestApplication(t, &contextRepositoryStub{
		getPlan: func(string) (persistence.Plan, error) {
			return persistence.Plan{
				ID:           "plan-1",
				ScenarioType: "INTERVIEW",
				Revision:     1,
				Status:       persistence.PlanStatusReady,
			}, nil
		},
	})
	_, _, err := application.CreateSession(
		context.Background(),
		contextActorFixture(),
		"plan-1",
		"intent-session-0002",
		CreateSessionRequest{
			ExpectedPlanRevision:  1,
			PreparationSnapshotID: "preparation-snapshot-1",
			PracticeOptionID:      "option_full_simulation",
			RoleDefinitionIDs: []string{
				"role_technical_interviewer",
				"role_hr_interviewer",
			},
		},
	)
	if !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("CreateSession error = %v, want role cardinality conflict", err)
	}
}

func TestSessionPolicyIsFrozenByPracticeOptionType(t *testing.T) {
	t.Parallel()

	config := persistence.ScenarioConfigSnapshot{
		FocusAreas: []string{"system_design"},
	}
	full := defaultContextSessionPolicy(
		config,
		persistence.PracticeOptionSnapshot{Type: "FULL_SIMULATION"},
	)
	if full.MinEffectiveTurns != 4 ||
		full.CoverageCheckpointTurn != 4 ||
		full.MaxEffectiveTurns != 6 {
		t.Fatalf("FULL_SIMULATION policy = %+v", full)
	}
	focus := defaultContextSessionPolicy(
		config,
		persistence.PracticeOptionSnapshot{Type: "FOCUS"},
	)
	if focus.MinEffectiveTurns != 1 ||
		focus.CoverageCheckpointTurn != 1 ||
		focus.MaxEffectiveTurns != 3 {
		t.Fatalf("FOCUS policy = %+v", focus)
	}
}

func TestFocusOptionMustMatchSelectedInterviewer(t *testing.T) {
	t.Parallel()

	plan := persistence.Plan{
		ScenarioDefinitionID:      "scenario-1",
		ScenarioDefinitionVersion: 1,
		ScenarioType:              "INTERVIEW",
		ScenarioConfigID:          "config-1",
		ScenarioConfigVersion:     1,
		PreparationProfileID:      "profile-1",
		SelectedRoleIDs:           []string{"role-1", "role-2"},
	}
	request := CreateSessionRequest{
		PracticeOptionID:  "option-focus-role-2",
		RoleDefinitionIDs: []string{"role-1"},
	}
	selection := SessionCatalogSelection{
		PlanCatalogSelection: PlanCatalogSelection{
			ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
				ID:      "scenario-1",
				Type:    "INTERVIEW",
				Version: 1,
				Status:  "active",
			},
			ScenarioConfig: persistence.ScenarioConfigSnapshot{
				ID:                   "config-1",
				ScenarioDefinitionID: "scenario-1",
				Type:                 "INTERVIEW",
				Version:              1,
			},
			SelectedRoles: []persistence.RoleSnapshot{{
				ID:                   "role-1",
				ScenarioDefinitionID: "scenario-1",
				Version:              1,
			}},
		},
		PracticeOption: persistence.PracticeOptionSnapshot{
			ID:                   "option-focus-role-2",
			ScenarioDefinitionID: "scenario-1",
			RoleDefinitionID:     "role-2",
			Type:                 "FOCUS",
			Version:              1,
		},
	}
	if validSessionCatalogSelection(plan, request, selection) {
		t.Fatal("FOCUS option accepted a different interviewer role")
	}
}

func TestTransitionSessionUsesCanonicalWireAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transition persistence.ContextSessionTransition
		wireAction string
	}{
		{
			name:       "pause",
			transition: persistence.ContextSessionPause,
			wireAction: "pause",
		},
		{
			name:       "resume",
			transition: persistence.ContextSessionResume,
			wireAction: "resume",
		},
		{
			name:       "end early",
			transition: persistence.ContextSessionEndEarly,
			wireAction: "end-early",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var captured persistence.TransitionContextSessionCommand
			application := newContextTestApplication(
				t,
				&contextRepositoryStub{
					transitionSession: func(
						command persistence.TransitionContextSessionCommand,
					) (persistence.ContextSession, bool, error) {
						captured = command
						return persistence.ContextSession{
							ID: command.SessionID,
						}, false, nil
					},
				},
			)

			session, replayed, err := application.TransitionSession(
				context.Background(),
				contextActorFixture(),
				"session-1",
				"intent-transition-0001",
				2,
				test.transition,
			)
			if err != nil {
				t.Fatalf("TransitionSession: %v", err)
			}
			if replayed || session.ID != "session-1" {
				t.Fatalf(
					"TransitionSession = (%+v, %t), want new session result",
					session,
					replayed,
				)
			}
			wantPath := "/v1/practice-sessions/session-1/" +
				test.wireAction
			if captured.Intent.CanonicalPath != wantPath {
				t.Fatalf(
					"canonical path = %q, want %q",
					captured.Intent.CanonicalPath,
					wantPath,
				)
			}
			if captured.Transition != test.transition {
				t.Fatalf(
					"transition enum = %q, want unchanged %q",
					captured.Transition,
					test.transition,
				)
			}
		})
	}
}

type contextRepositoryStub struct {
	replayPlan        func(persistence.ContextIdempotencyIntent) (persistence.Plan, bool, error)
	createPlan        func(persistence.CreatePlanCommand) (persistence.Plan, bool, error)
	updatePlan        func(persistence.UpdatePlanCommand) (persistence.Plan, bool, error)
	getPlan           func(string) (persistence.Plan, error)
	replaySession     func(persistence.ContextIdempotencyIntent) (persistence.ContextSessionBootstrap, bool, error)
	createSession     func(persistence.CreateContextSessionCommand) (persistence.ContextSessionBootstrap, bool, error)
	transitionSession func(persistence.TransitionContextSessionCommand) (persistence.ContextSession, bool, error)
}

func (s *contextRepositoryStub) ReplayPlan(
	_ context.Context,
	_ persistence.Actor,
	intent persistence.ContextIdempotencyIntent,
) (persistence.Plan, bool, error) {
	if s.replayPlan != nil {
		return s.replayPlan(intent)
	}
	return persistence.Plan{}, false, nil
}

func (s *contextRepositoryStub) CreatePlan(
	_ context.Context,
	_ persistence.Actor,
	command persistence.CreatePlanCommand,
) (persistence.Plan, bool, error) {
	if s.createPlan != nil {
		return s.createPlan(command)
	}
	return persistence.Plan{}, false, errors.New("unexpected CreatePlan")
}

func (s *contextRepositoryStub) UpdatePlan(
	_ context.Context,
	_ persistence.Actor,
	command persistence.UpdatePlanCommand,
) (persistence.Plan, bool, error) {
	if s.updatePlan != nil {
		return s.updatePlan(command)
	}
	return persistence.Plan{}, false, errors.New("unexpected UpdatePlan")
}

func (s *contextRepositoryStub) GetPlan(
	_ context.Context,
	_ persistence.Actor,
	planID string,
) (persistence.Plan, error) {
	if s.getPlan != nil {
		return s.getPlan(planID)
	}
	return persistence.Plan{}, errors.New("unexpected GetPlan")
}

func (s *contextRepositoryStub) ReplayContextSession(
	_ context.Context,
	_ persistence.Actor,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, bool, error) {
	if s.replaySession != nil {
		return s.replaySession(intent)
	}
	return persistence.ContextSessionBootstrap{}, false, nil
}

func (s *contextRepositoryStub) CreateContextSession(
	_ context.Context,
	_ persistence.Actor,
	command persistence.CreateContextSessionCommand,
) (persistence.ContextSessionBootstrap, bool, error) {
	if s.createSession != nil {
		return s.createSession(command)
	}
	return persistence.ContextSessionBootstrap{}, false,
		errors.New("unexpected CreateContextSession")
}

func (s *contextRepositoryStub) GetContextSession(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSession, error) {
	return persistence.ContextSession{}, errors.New("unexpected GetContextSession")
}

func (s *contextRepositoryStub) GetContextSessionSnapshot(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSessionSnapshot, error) {
	return persistence.ContextSessionSnapshot{},
		errors.New("unexpected GetContextSessionSnapshot")
}

func (s *contextRepositoryStub) ResolveContextSessionByThread(
	context.Context,
	persistence.Actor,
	string,
) (persistence.ContextSessionBootstrap, error) {
	return persistence.ContextSessionBootstrap{},
		errors.New("unexpected ResolveContextSessionByThread")
}

func (s *contextRepositoryStub) TransitionContextSession(
	_ context.Context,
	_ persistence.Actor,
	command persistence.TransitionContextSessionCommand,
) (persistence.ContextSession, bool, error) {
	if s.transitionSession != nil {
		return s.transitionSession(command)
	}
	return persistence.ContextSession{}, false,
		errors.New("unexpected TransitionContextSession")
}

func (s *contextRepositoryStub) DeleteUserData(
	context.Context,
	persistence.DeletionContext,
) error {
	return errors.New("unexpected DeleteUserData")
}

type panicContextDependency struct{}

func (panicContextDependency) ValidatePracticeAnchor(
	context.Context,
	requestcontext.Actor,
	string,
	string,
) (PracticeAnchor, error) {
	panic("mutable Agent context must not run on replay")
}

func (panicContextDependency) ReadPreparationProfile(
	context.Context,
	requestcontext.Actor,
	string,
) (PreparationProfileRef, error) {
	panic("mutable Preparation profile must not run on replay")
}

func (panicContextDependency) ReadPreparationSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (persistence.PreparationSnapshot, error) {
	panic("mutable Preparation snapshot must not run on replay")
}

func (panicContextDependency) ReadPlanCatalog(
	PlanCatalogRequest,
) (PlanCatalogSelection, error) {
	panic("mutable Catalog must not run on replay")
}

func (panicContextDependency) ReadSessionCatalog(
	SessionCatalogRequest,
) (SessionCatalogSelection, error) {
	panic("mutable Catalog must not run on replay")
}

type panicIDGenerator struct{}

func (panicIDGenerator) NewID() (string, error) {
	panic("ID generation must not run on replay")
}

func newContextTestApplication(
	t *testing.T,
	repository persistence.ContextRepository,
) *ContextApplication {
	t.Helper()
	application, err := NewContextApplication(
		repository,
		panicIDGenerator{},
		panicContextDependency{},
		panicContextDependency{},
		panicContextDependency{},
	)
	if err != nil {
		t.Fatalf("NewContextApplication: %v", err)
	}
	return application
}

func contextActorFixture() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000112",
		SessionID: "20000000-0000-4000-8000-000000000112",
	}
}

func validContextPlanRequest() CreatePlanRequest {
	return CreatePlanRequest{
		AgentThreadID:             "thread-1",
		MatterID:                  "matter-1",
		ScenarioDefinitionID:      "scenario-1",
		ScenarioDefinitionVersion: 1,
		ScenarioConfigID:          "config-1",
		ScenarioConfigVersion:     1,
		PreparationProfileID:      "profile-1",
		SelectedRoleIDs:           []string{"role-1"},
	}
}
