package practice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestContextApplicationCreatesSessionFromExactExecutablePlan(t *testing.T) {
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
		command.Snapshot.Preparation.ID != plan.PreparationSnapshot.ID ||
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

func TestContextApplicationCopiesFrozenIELTSAssignmentFromPlan(t *testing.T) {
	t.Parallel()
	plan := practicePlanFixture()
	plan.SceneSelection.Scene.Family = scene.SceneFamilyExam
	plan.SceneSelection.Scene.Model = scene.SceneModelIELTSSpeakingPart1
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
	plan.IELTSAssignment = &preparation.IELTSAssignmentSnapshot{
		BankID:         "ielts-bank-1",
		Season:         "2026-05",
		Mode:           scene.IELTSPracticeModePart1,
		Part1SetID:     "part-1-set-1",
		Part1Questions: 8,
		TurnBlueprints: append([]string(nil), blueprints...),
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
		assignment.Part1SetID != plan.IELTSAssignment.Part1SetID ||
		!equalStrings(
			assignment.TurnBlueprints,
			plan.IELTSAssignment.TurnBlueprints,
		) {
		t.Fatalf("Session IELTS assignment = %#v", assignment)
	}
}

func TestContextApplicationRejectsUnconfirmedSessionBeforePlanRead(
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

func TestContextApplicationMapsStaleExecutablePlanToConflict(t *testing.T) {
	t.Parallel()
	application := newContextTestApplication(
		t,
		&sessionRepositoryStub{},
		&planReaderStub{err: preparation.ErrPlanConflict},
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

func TestContextApplicationReplaysSessionWithoutReadingPlan(t *testing.T) {
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

func TestConfirmAndStartRequiresExactPlanSourceThread(t *testing.T) {
	t.Parallel()
	plan := practicePlanFixture()
	plan.SourceThreadID = "thread-1"
	reader := &planReaderStub{plan: plan}
	application := newContextTestApplication(t, &sessionRepositoryStub{}, reader)

	_, err := application.ConfirmAndStartPractice(
		context.Background(),
		practiceActorFixture(),
		"session-create-0005",
		StartConfirmation{
			AgentThreadID:        "thread-other",
			PracticePlanID:       plan.ID,
			ExpectedPlanRevision: plan.Revision,
		},
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmAndStartPractice thread error = %v", err)
	}

	plan.SourceThreadID = ""
	reader.plan = plan
	_, err = application.ConfirmAndStartPractice(
		context.Background(),
		practiceActorFixture(),
		"session-create-0006",
		StartConfirmation{
			AgentThreadID:        "thread-1",
			PracticePlanID:       plan.ID,
			ExpectedPlanRevision: plan.Revision,
		},
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmAndStartPractice empty source error = %v", err)
	}
}

func TestConfirmAndStartResolvesActiveConflictByPlan(t *testing.T) {
	t.Parallel()
	plan := practicePlanFixture()
	plan.SourceThreadID = "thread-1"
	want := contextBootstrapFixture()
	repository := &sessionRepositoryStub{
		createErr: ErrActiveSessionConflict,
		resolved:  want,
	}
	application := newContextTestApplication(
		t,
		repository,
		&planReaderStub{plan: plan},
	)
	result, err := application.ConfirmAndStartPractice(
		context.Background(),
		practiceActorFixture(),
		"session-create-0007",
		StartConfirmation{
			AgentThreadID:        plan.SourceThreadID,
			PracticePlanID:       plan.ID,
			ExpectedPlanRevision: plan.Revision,
		},
	)
	if err != nil || !result.ActiveConflict ||
		repository.resolvedPlanID != plan.ID {
		t.Fatalf("ConfirmAndStartPractice = (%#v,%v), resolved=%q", result, err, repository.resolvedPlanID)
	}
}

func newContextTestApplication(
	t *testing.T,
	repository SessionRepository,
	reader preparation.PlanReader,
) *ContextApplication {
	t.Helper()
	application, err := NewContextApplication(
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
		t.Fatalf("NewContextApplication: %v", err)
	}
	return application
}

func practicePlanFixture() preparation.PracticePlan {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	definition := scene.SceneDefinition{
		ID:      "scene-1",
		Family:  scene.SceneFamilyInterview,
		Model:   scene.SceneModelInterviewBasicDialogue,
		Name:    "Interview",
		Version: 1,
		Status:  scene.SceneStatusActive,
		Prompt: scene.ScenePrompt{
			PublicSceneBrief:         "Brief",
			PracticeGoal:             "Goal",
			UserRole:                 "Candidate",
			AIRole:                   "Interviewer",
			PersonaSummary:           "Interviewer",
			FocusAreas:               []string{"clarity"},
			TurnBlueprints:           []string{"question"},
			SuggestedDurationSeconds: 600,
		},
		Roles: []scene.RoleDefinition{{
			ID:               "role-1",
			SceneID:          "scene-1",
			Type:             "INTERVIEWER",
			DisplayName:      "Interviewer",
			Responsibilities: "Ask",
			Style:            "Direct",
			PracticeObjectives: []scene.PracticeObjectiveDefinition{{
				ID: "clarity", Description: "Explain the answer clearly.",
			}},
		}},
		PracticeOptions: []scene.PracticeOption{{
			ID:          "option-full",
			SceneID:     "scene-1",
			Type:        scene.PracticeOptionFullSimulation,
			DisplayName: "Full",
		}},
	}
	return preparation.PracticePlan{
		ID:             "plan-1",
		UserID:         "user-1",
		SourceThreadID: "thread-1",
		PreparationSnapshot: preparation.Snapshot{
			ID:                 "snapshot-preparation-1",
			SourceProfileID:    "profile-1",
			SourceVersion:      1,
			BackgroundSnapshot: "Backend engineer",
			CreatedAt:          createdAt,
		},
		SceneSelection: scene.SelectionSnapshot{
			Scene:            definition,
			SelectedRoleIDs:  []string{"role-1"},
			PracticeOptionID: "option-full",
		},
		SessionPolicy: preparation.SessionPolicy{
			SuggestedDurationSeconds: 600,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			EarlyCompletionRule: preparation.
				EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: []preparation.PracticeObjective{{
			ID: "clarity", Description: "clarity",
		}},
		Revision:  3,
		Status:    preparation.PlanStatusReady,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func practiceActorFixture() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-session-1"}
}

type planReaderStub struct {
	plan     preparation.PracticePlan
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
) (preparation.PracticePlan, error) {
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
