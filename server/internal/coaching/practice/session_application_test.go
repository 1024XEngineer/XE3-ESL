package practice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestSessionApplicationCreatesSessionFromExactExecutablePlan(t *testing.T) {
	t.Parallel()
	plan := practicePlanFixture()
	repository := &sessionRepositoryStub{}
	reader := &planReaderStub{plan: plan}
	application := newContextTestApplication(t, repository, reader)

	created, replayed, err := application.CreateSession(
		context.Background(),
		practiceActorFixture(),
		plan.ID,
		"session-create-0001",
		CreateSessionRequest{ExpectedPlanRevision: 3, UserConfirmed: true},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if replayed || reader.planID != plan.ID || reader.revision != 3 {
		t.Fatalf(
			"CreateSession replay=%t read=(%q,%d)",
			replayed,
			reader.planID,
			reader.revision,
		)
	}
	command := repository.created
	if command.PlanID != plan.ID || command.PlanRevision != plan.Revision ||
		command.Snapshot.Preparation.ID != plan.Preparation.ID ||
		command.Snapshot.SceneSelection.Scene.ID !=
			plan.SceneSelection.Scene.ID ||
		command.Snapshot.SessionPolicy != plan.SessionPolicy ||
		len(command.Snapshot.PracticeObjectives) !=
			len(plan.PracticeObjectives) {
		t.Fatalf("CreateSession command = %#v", command)
	}
	if len(command.Snapshot.Participants) != 2 ||
		command.Snapshot.Participants[0].Role != "FACILITATOR" ||
		command.Snapshot.Participants[1].Role != "LEARNER" {
		t.Fatalf("participants = %#v", command.Snapshot.Participants)
	}
	if created.Session.PlanRevision != plan.Revision ||
		created.Snapshot.PlanRevision != plan.Revision {
		t.Fatalf("created = %#v", created)
	}
}

func TestSessionApplicationCopiesFrozenIELTSAssignmentFromPlan(t *testing.T) {
	t.Parallel()
	plan := practicePlanFixture()
	plan.SceneSelection.Scene.Experience = PracticeExperienceIELTSSpeaking
	plan.SceneSelection.Scene.Category = SceneCategory("EXAM")
	blueprints := []string{
		"question-1", "question-2", "question-3", "question-4",
		"question-5", "question-6", "question-7", "question-8",
	}
	plan.SceneSelection.Scene.Prompt.PublicSceneBrief =
		"完成冻结的三个熟悉话题和八道 Part 1 问题。"
	plan.SceneSelection.Scene.Prompt.TurnBlueprints = append(
		[]string(nil),
		blueprints...,
	)
	plan.SceneSelection.Scene.PracticeOptions[0] = PracticeOption{
		ID:                       "option-full",
		SceneID:                  plan.SceneSelection.Scene.ID,
		Mode:                     PracticeModePart1,
		DisplayName:              "Part 1",
		SuggestedDurationSeconds: 600,
		TurnPolicyRef:            IELTSSpeakingPart1TurnPolicy,
		SessionPolicyRef:         IELTSSpeakingPart1SessionPolicy,
		EvaluationPolicyRef:      "ielts.speaking_part1.evaluation.v1",
	}
	plan.SessionPolicy.MinEffectiveTurns = len(blueprints)
	plan.SessionPolicy.MaxEffectiveTurns = len(blueprints)
	plan.SessionPolicy.CoverageCheckpointTurn = len(blueprints)
	plan.SessionPolicy.MaxFollowUpsPerQuestion = 0
	plan.SessionPolicy.SpeechFeedbackAllowed = true
	plan.IELTSAssignment = &IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   PracticeModePart1,
		Parts: []IELTSPart{{
			Part:           PracticeModePart1,
			SourceID:       "part-1-set-1",
			TurnBlueprints: append([]string(nil), blueprints...),
		}},
	}
	repository := &sessionRepositoryStub{}
	application := newContextTestApplication(
		t,
		repository,
		&planReaderStub{plan: plan},
	)
	_, _, err := application.CreateSession(
		context.Background(),
		practiceActorFixture(),
		plan.ID,
		"session-create-ielts",
		CreateSessionRequest{
			ExpectedPlanRevision: plan.Revision,
			UserConfirmed:        true,
		},
	)
	if err != nil {
		t.Fatalf("CreateSession IELTS: %v", err)
	}
	assignment := repository.created.Snapshot.IELTSAssignment
	if assignment == nil || assignment == plan.IELTSAssignment ||
		assignment.Mode != plan.IELTSAssignment.Mode ||
		len(assignment.Parts) != 1 ||
		assignment.Parts[0].SourceID !=
			plan.IELTSAssignment.Parts[0].SourceID ||
		assignment.Parts[0].TurnBlueprints == nil ||
		!equalStrings(
			assignment.Parts[0].TurnBlueprints,
			plan.IELTSAssignment.Parts[0].TurnBlueprints,
		) {
		t.Fatalf("Session IELTS assignment = %#v", assignment)
	}
}

func TestSessionApplicationRejectsUnconfirmedSessionBeforePlanRead(
	t *testing.T,
) {
	t.Parallel()
	reader := &planReaderStub{err: errors.New("must not read")}
	application := newContextTestApplication(t, &sessionRepositoryStub{}, reader)
	_, _, err := application.CreateSession(
		context.Background(),
		practiceActorFixture(),
		"plan-1",
		"session-create-0002",
		CreateSessionRequest{ExpectedPlanRevision: 1},
	)
	if !errors.Is(err, ErrConfirmationRequired) || reader.calls != 0 {
		t.Fatalf("CreateSession = %v, reads=%d", err, reader.calls)
	}
}

func TestSessionApplicationMapsStaleExecutablePlanToConflict(t *testing.T) {
	t.Parallel()
	application := newContextTestApplication(
		t,
		&sessionRepositoryStub{},
		&planReaderStub{err: ErrConflict},
	)
	_, _, err := application.CreateSession(
		context.Background(),
		practiceActorFixture(),
		"plan-1",
		"session-create-0003",
		CreateSessionRequest{ExpectedPlanRevision: 2, UserConfirmed: true},
	)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateSession stale error = %v", err)
	}
}

func TestSessionApplicationRejectsUnknownPoliciesBeforeRepositoryWrite(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*PlanProjection)
	}{
		{
			name: "unknown Session policy",
			mutate: func(plan *PlanProjection) {
				plan.SceneSelection.Scene.PracticeOptions[0].SessionPolicyRef =
					"unknown.practice.session.v1"
			},
		},
		{
			name: "unknown Turn policy",
			mutate: func(plan *PlanProjection) {
				plan.SceneSelection.Scene.PracticeOptions[0].TurnPolicyRef =
					"unknown.practice.turn.v1"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := practicePlanFixture()
			test.mutate(&plan)
			repository := &sessionRepositoryStub{}
			application := newContextTestApplication(
				t,
				repository,
				&planReaderStub{plan: plan},
			)
			_, _, err := application.CreateSession(
				context.Background(),
				practiceActorFixture(),
				plan.ID,
				"unknown-policy-key",
				CreateSessionRequest{
					ExpectedPlanRevision: plan.Revision,
					UserConfirmed:        true,
				},
			)
			if !errors.Is(err, ErrConflict) || repository.createCalls != 0 {
				t.Fatalf(
					"CreateSession() = %v, writes = %d",
					err,
					repository.createCalls,
				)
			}
		})
	}
}

func TestSessionApplicationReplaysSessionWithoutReadingPlan(t *testing.T) {
	t.Parallel()
	want := contextBootstrapFixture()
	repository := &sessionRepositoryStub{replay: want, replayFound: true}
	reader := &planReaderStub{err: errors.New("must not read")}
	application := newContextTestApplication(t, repository, reader)
	got, replayed, err := application.CreateSession(
		context.Background(),
		practiceActorFixture(),
		"plan-1",
		"session-create-0004",
		CreateSessionRequest{ExpectedPlanRevision: 1, UserConfirmed: true},
	)
	if err != nil || !replayed || got.Session.ID != want.Session.ID ||
		reader.calls != 0 {
		t.Fatalf("CreateSession replay = (%#v,%t,%v), reads=%d", got, replayed, err, reader.calls)
	}
}

func newContextTestApplication(
	t *testing.T,
	repository SessionRepository,
	reader PlanProjectionReader,
) *SessionApplication {
	t.Helper()
	application, err := NewSessionApplication(
		repository,
		&practiceIDStub{values: []string{
			"session-1",
			"snapshot-1",
			"participant-facilitator",
			"participant-learner",
		}},
		reader,
	)
	if err != nil {
		t.Fatalf("NewSessionApplication: %v", err)
	}
	return application
}

func practicePlanFixture() PlanProjection {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	definition := SceneDefinition{
		ID:         "scene-1",
		Experience: PracticeExperienceInterview,
		Category:   SceneCategory("PROFESSIONAL"),
		Name:       "Interview",
		Version:    1,
		Status:     SceneStatusActive,
		Prompt: ScenePrompt{
			PublicSceneBrief: "Brief",
			PracticeGoal:     "Goal",
			UserRole:         "Candidate",
			AIRole:           "Interviewer",
			PersonaSummary:   "Interviewer",
			FocusAreas:       []string{"clarity"},
			TurnBlueprints:   []string{"question"},
		},
		Roles: []RoleDefinition{{
			ID:               "role-1",
			SceneID:          "scene-1",
			Type:             "INTERVIEWER",
			DisplayName:      "Interviewer",
			Responsibilities: "Ask",
			Style:            "Direct",
			PracticeObjectives: []PracticeObjectiveDefinition{{
				ID: "clarity", Description: "Explain the answer clearly.",
			}},
		}},
		PracticeOptions: []PracticeOption{{
			ID:                       "option-full",
			SceneID:                  "scene-1",
			Mode:                     PracticeModeFullSimulation,
			DisplayName:              "Full",
			SuggestedDurationSeconds: 600,
			TurnPolicyRef:            GenericPracticeTurnPolicy,
			SessionPolicyRef:         GenericPracticeSessionPolicy,
			EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
		}},
	}
	return PlanProjection{
		ID:          "plan-1",
		OwnerUserID: "user-1",
		Preparation: PreparationSnapshot{
			ID:                 "snapshot-preparation-1",
			SourceProfileID:    "profile-1",
			SourceVersion:      1,
			BackgroundSnapshot: "Backend engineer",
			CreatedAt:          createdAt,
		},
		SceneSelection: SceneSelection{
			Scene:            definition,
			SelectedRoleIDs:  []string{"role-1"},
			PracticeOptionID: "option-full",
		},
		SessionPolicy: SessionPolicy{
			SuggestedDurationSeconds: 600,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			EarlyCompletionRule:      EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: []PracticeObjective{{
			ID: "clarity", Description: "clarity",
		}},
		Revision: 3,
	}
}

func practiceActorFixture() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-session-1"}
}

type planReaderStub struct {
	plan     PlanProjection
	err      error
	calls    int
	planID   string
	revision int
}

func (s *planReaderStub) ReadExecutablePlan(
	_ context.Context,
	_ requestcontext.Actor,
	planID string,
	revision int,
) (PlanProjection, error) {
	s.calls++
	s.planID = planID
	s.revision = revision
	return s.plan, s.err
}

type practiceIDStub struct {
	values []string
}

func (s *practiceIDStub) NewID() (string, error) {
	if len(s.values) == 0 {
		return "", errors.New("no ID")
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

type sessionRepositoryStub struct {
	replay         SessionBootstrap
	replayFound    bool
	created        CreateSessionCommand
	createErr      error
	resolved       SessionBootstrap
	resolvedPlanID string
	createCalls    int
}

func (s *sessionRepositoryStub) ReplaySession(
	context.Context,
	Actor,
	IdempotencyIntent,
) (SessionBootstrap, bool, error) {
	return s.replay, s.replayFound, nil
}

func (s *sessionRepositoryStub) CreateSession(
	_ context.Context,
	_ Actor,
	command CreateSessionCommand,
) (SessionBootstrap, bool, error) {
	s.createCalls++
	s.created = command
	if s.createErr != nil {
		return SessionBootstrap{}, false, s.createErr
	}
	return SessionBootstrap{
		Session: Session{
			ID:           command.SessionID,
			PlanID:       command.PlanID,
			PlanRevision: command.PlanRevision,
		},
		Snapshot: command.Snapshot,
	}, false, nil
}

func (s *sessionRepositoryStub) GetSession(
	context.Context,
	Actor,
	string,
) (Session, error) {
	return s.resolved.Session, nil
}

func (s *sessionRepositoryStub) GetSessionSnapshot(
	context.Context,
	Actor,
	string,
) (SessionSnapshot, error) {
	return s.resolved.Snapshot, nil
}

func (s *sessionRepositoryStub) ResolveSessionByPlan(
	_ context.Context,
	_ Actor,
	planID string,
) (SessionBootstrap, error) {
	s.resolvedPlanID = planID
	return s.resolved, nil
}

func (s *sessionRepositoryStub) TransitionSession(
	context.Context,
	Actor,
	TransitionSessionCommand,
) (Session, bool, error) {
	return s.resolved.Session, false, nil
}

func (s *sessionRepositoryStub) DeleteUserData(
	context.Context,
	DeletionContext,
) error {
	return nil
}

func contextBootstrapFixture() SessionBootstrap {
	return SessionBootstrap{
		Session: Session{ID: "session-1", PlanID: "plan-1"},
		Snapshot: SessionSnapshot{
			ID: "snapshot-1", SessionID: "session-1",
		},
	}
}
