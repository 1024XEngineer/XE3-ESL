package practice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestServicePracticeFlowAndIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})

	planCommand := CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "create-plan-1"}
	plan, err := service.CreatePracticePlan(ctx, planCommand)
	if err != nil {
		t.Fatalf("CreatePracticePlan() error = %v", err)
	}
	replayedPlan, err := service.CreatePracticePlan(ctx, planCommand)
	if err != nil || replayedPlan.ID() != plan.ID() {
		t.Fatalf("replayed plan = %v, %v", replayedPlan, err)
	}
	changedPlanCommand := planCommand
	changedPlanCommand.ScenarioConfigID = "config-other"
	if _, err := service.CreatePracticePlan(ctx, changedPlanCommand); !errors.Is(err, ErrPracticeIdempotencyConflict) {
		t.Fatalf("changed plan replay error = %v", err)
	}

	sessionCommand := CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "create-session-1"}
	session, err := service.CreatePracticeSession(ctx, sessionCommand)
	if err != nil {
		t.Fatalf("CreatePracticeSession() error = %v", err)
	}
	replayedSession, err := service.CreatePracticeSession(ctx, sessionCommand)
	if err != nil || replayedSession.ID() != session.ID() {
		t.Fatalf("replayed session = %v, %v", replayedSession, err)
	}
	changedSessionCommand := sessionCommand
	changedSessionCommand.PracticeOptionID = "focused"
	if _, err := service.CreatePracticeSession(ctx, changedSessionCommand); !errors.Is(err, ErrPracticeIdempotencyConflict) {
		t.Fatalf("changed session replay error = %v", err)
	}
	if _, err := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "other"}); !errors.Is(err, ErrPracticePlanHasActiveSession) {
		t.Fatalf("second active session error = %v", err)
	}

	if _, err := service.StartPracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("StartPracticeSession() error = %v", err)
	}
	if _, err := service.StartPracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("replayed StartPracticeSession() error = %v", err)
	}
	outcome := TurnOutcome{TurnID: "turn-1", SessionID: session.ID(), AnswerValid: true, ObjectiveCoverage: []ObjectiveCoverage{{ObjectiveID: "objective-1", Level: CoverageUncovered}}, CompletedPrimaryQuestionCount: 1}
	action, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: outcome})
	if err != nil {
		t.Fatalf("ApplyTurnOutcome() error = %v", err)
	}
	replayedAction, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: outcome})
	if err != nil || replayedAction != action {
		t.Fatalf("replayed action = %q, %v", replayedAction, err)
	}
	if got := repo.effectiveTurns[session.ID()]; got != 1 {
		t.Fatalf("effective turns = %d, want 1", got)
	}
	snapshot, err := service.GetPracticeSessionSnapshot(ctx, "user-1", session.ID())
	if err != nil || snapshot.PracticeFocuses()[0] != "focus" {
		t.Fatalf("GetPracticeSessionSnapshot() = %v, %v", snapshot, err)
	}
	if got := snapshot.PreparationSnapshotID(); got != "preparation-snapshot-1" {
		t.Fatalf("PreparationSnapshotID() = %q", got)
	}
	participants := snapshot.Participants()
	if len(participants) != 1 {
		t.Fatalf("Participants() count = %d, want 1", len(participants))
	}
	if participants[0].ParticipantRole != ParticipantRoleInterviewer || participants[0].SubjectRef != (SubjectRef{Namespace: "preparation", SubjectID: "interviewer-1"}) {
		t.Fatalf("interviewer participant = %#v", participants[0])
	}
	if _, err := service.PausePracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("PausePracticeSession() error = %v", err)
	}
	if _, err := service.PausePracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("replayed PausePracticeSession() error = %v", err)
	}
	if _, err := service.ResumePracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("ResumePracticeSession() error = %v", err)
	}
	if _, err := service.ResumePracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatalf("replayed ResumePracticeSession() error = %v", err)
	}
	if _, err := service.EndPracticeSessionEarly(ctx, "user-1", session.ID(), "user_requested"); err != nil {
		t.Fatalf("EndPracticeSessionEarly() error = %v", err)
	}
	if _, err := service.EndPracticeSessionEarly(ctx, "user-1", session.ID(), "user_requested"); err != nil {
		t.Fatalf("replayed EndPracticeSessionEarly() error = %v", err)
	}
	if _, err := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "after-terminal"}); err != nil {
		t.Fatalf("CreatePracticeSession() after terminal error = %v", err)
	}
}

func TestCreatePracticePlanReplayUsesOriginalRequest(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	preparation := newFakePreparationReader()
	service := newTestServiceWithPreparation(repo, fakeTransactions{repo: repo}, preparation)
	command := CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"}
	plan, err := service.CreatePracticePlan(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdatePracticePlan(ctx, UpdatePracticePlanCommand{UserID: "user-1", PracticePlanID: plan.ID(), ScenarioConfigID: "config-2", PreparationProfileID: "profile-2", SelectedRoleIDs: []string{"role-1"}, ExpectedRevision: plan.Revision()}); err != nil {
		t.Fatal(err)
	}
	calls := preparation.validateCalls
	preparation.validateErr = errors.New("preparation unavailable")
	replayed, err := service.CreatePracticePlan(ctx, command)
	if err != nil || replayed.ID() != plan.ID() {
		t.Fatalf("CreatePracticePlan() replay = %v, %v", replayed, err)
	}
	if preparation.validateCalls != calls {
		t.Fatalf("ValidatePlan() calls = %d, want %d", preparation.validateCalls, calls)
	}
}

func TestCreatePracticeSessionReplaySkipsCurrentPlanAndPreparation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	preparation := newFakePreparationReader()
	service := newTestServiceWithPreparation(repo, fakeTransactions{repo: repo}, preparation)
	plan, err := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	command := CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"}
	session, err := service.CreatePracticeSession(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartPracticeSession(ctx, "user-1", session.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EndPracticeSessionEarly(ctx, "user-1", session.ID(), "user_requested"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdatePracticePlan(ctx, UpdatePracticePlanCommand{UserID: "user-1", PracticePlanID: plan.ID(), ScenarioConfigID: "config-2", PreparationProfileID: "profile-2", SelectedRoleIDs: []string{"role-1"}, ExpectedRevision: plan.Revision()}); err != nil {
		t.Fatal(err)
	}
	calls := preparation.prepareCalls
	preparation.prepareErr = errors.New("preparation unavailable")
	replayed, err := service.CreatePracticeSession(ctx, command)
	if err != nil || replayed.ID() != session.ID() {
		t.Fatalf("CreatePracticeSession() replay = %v, %v", replayed, err)
	}
	if preparation.prepareCalls != calls {
		t.Fatalf("PrepareSession() calls = %d, want %d", preparation.prepareCalls, calls)
	}
}

func TestServiceRejectsInvalidCreateCommandsBeforeRepositoryAccess(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{name: "创建计划缺少用户", run: func(s *Service) error {
			_, err := s.CreatePracticePlan(context.Background(), CreatePracticePlanCommand{IdempotencyKey: "key"})
			return err
		}},
		{name: "创建计划缺少幂等键", run: func(s *Service) error {
			_, err := s.CreatePracticePlan(context.Background(), CreatePracticePlanCommand{UserID: "user-1"})
			return err
		}},
		{name: "创建场次缺少用户", run: func(s *Service) error {
			_, err := s.CreatePracticeSession(context.Background(), CreatePracticeSessionCommand{IdempotencyKey: "key"})
			return err
		}},
		{name: "创建场次缺少幂等键", run: func(s *Service) error {
			_, err := s.CreatePracticeSession(context.Background(), CreatePracticeSessionCommand{UserID: "user-1"})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			service := newTestService(repo, fakeTransactions{repo: repo})
			if err := tt.run(service); !errors.Is(err, ErrPracticeCommandInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrPracticeCommandInvalid)
			}
			if repo.calls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.calls)
			}
		})
	}
}

func TestCreatePracticeSessionRejectsPreparationIdentityMismatch(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*SessionPreparation)
	}{
		{name: "角色不一致", mutate: func(p *SessionPreparation) { p.Role.RoleDefinitionID = "role-other" }},
		{name: "练习选项不一致", mutate: func(p *SessionPreparation) { p.PracticeOption.PracticeOptionID = "option-other" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepository()
			base := newFakePreparationReader()
			tt.mutate(&base.session)
			service := newTestServiceWithPreparation(repo, fakeTransactions{repo: repo}, fixedPreparationReader{session: base.session})
			plan, err := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
			if err != nil {
				t.Fatalf("CreatePracticePlan() error = %v", err)
			}
			_, err = service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
			if !errors.Is(err, ErrPracticeSessionSnapshotInvalid) {
				t.Fatalf("CreatePracticeSession() error = %v, want %v", err, ErrPracticeSessionSnapshotInvalid)
			}
		})
	}
}

func TestApplyTurnOutcomeDerivesRemainingTimeFromSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	clock := &fakeClock{now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)}
	service := newTestServiceWithClock(repo, fakeTransactions{repo: repo}, newFakePreparationReader(), clock)
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	session, _ := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	_, _ = service.StartPracticeSession(ctx, "user-1", session.ID())
	repo.effectiveTurns[session.ID()] = 3
	clock.now = clock.now.Add(10 * time.Minute)
	action, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: TurnOutcome{TurnID: "turn-4", SessionID: session.ID(), AnswerValid: true}})
	if err != nil || action != NextActionCompleteSession {
		t.Fatalf("ApplyTurnOutcome() = %q, %v, want %q", action, err, NextActionCompleteSession)
	}
}

func TestServiceReplaysCompletingTurnAfterSessionIsTerminal(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	session, _ := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	_, _ = service.StartPracticeSession(ctx, "user-1", session.ID())
	repo.effectiveTurns[session.ID()] = 5
	outcome := TurnOutcome{TurnID: "turn-complete", SessionID: session.ID(), AnswerValid: true, CompletedPrimaryQuestionCount: 4}
	first, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: outcome})
	if err != nil || first != NextActionCompleteSession {
		t.Fatalf("first action = %q, %v", first, err)
	}
	replayed, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: outcome})
	if err != nil || replayed != first {
		t.Fatalf("replayed action = %q, %v", replayed, err)
	}
}

func TestServiceRejectsOwnershipAndRevisionMismatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	command := CreatePracticeSessionCommand{UserID: "other", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"}
	if _, err := service.CreatePracticeSession(ctx, command); !errors.Is(err, ErrPracticeResourceForbidden) {
		t.Fatalf("ownership error = %v", err)
	}
	command.UserID, command.PlanRevision = "user-1", plan.Revision()+1
	if _, err := service.CreatePracticeSession(ctx, command); !errors.Is(err, ErrPracticePlanRevisionConflict) {
		t.Fatalf("revision error = %v", err)
	}
}

func TestServiceTransactionFailureLeavesNoPlan(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo, fail: true})
	_, err := service.CreatePracticePlan(context.Background(), CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	if err == nil {
		t.Fatal("CreatePracticePlan() error = nil")
	}
	if len(repo.plans) != 0 {
		t.Fatalf("plans = %d, want 0", len(repo.plans))
	}
}

func TestCreatePracticePlanConcurrentReplayIsAtomic(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	command := CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "same-key"}
	const workers = 32
	results := make(chan *PracticePlan, workers)
	errorsFound := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			plan, err := service.CreatePracticePlan(context.Background(), command)
			results <- plan
			errorsFound <- err
		}()
	}
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("CreatePracticePlan() error = %v", err)
		}
	}
	var id string
	for plan := range results {
		if id == "" {
			id = plan.ID()
		}
		if plan.ID() != id {
			t.Fatalf("plan ID = %q, want %q", plan.ID(), id)
		}
	}
	if len(repo.plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(repo.plans))
	}
}

func TestCreatePracticeSessionConcurrentReplayAndActiveConstraintAreAtomic(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, err := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	command := CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "same-key"}
	const workers = 32
	results := make(chan *PracticeSession, workers)
	errorsFound := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			session, err := service.CreatePracticeSession(ctx, command)
			results <- session
			errorsFound <- err
		}()
	}
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("CreatePracticeSession() error = %v", err)
		}
	}
	var id string
	for session := range results {
		if id == "" {
			id = session.ID()
		}
		if session.ID() != id {
			t.Fatalf("session ID = %q, want %q", session.ID(), id)
		}
	}
	if len(repo.sessions) != 1 || len(repo.snapshots) != 1 || len(repo.active) != 1 {
		t.Fatalf("repository state = sessions:%d snapshots:%d active:%d", len(repo.sessions), len(repo.snapshots), len(repo.active))
	}
}

func TestApplyTurnOutcomeConcurrentUpdatesDoNotLoseProgress(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	session, _ := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	_, _ = service.StartPracticeSession(ctx, "user-1", session.ID())
	turnIDs := []string{"turn-1", "turn-1", "turn-2", "turn-3"}
	var group sync.WaitGroup
	errorsFound := make(chan error, len(turnIDs))
	for _, turnID := range turnIDs {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: TurnOutcome{TurnID: turnID, SessionID: session.ID(), AnswerValid: true}})
			errorsFound <- err
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("ApplyTurnOutcome() error = %v", err)
		}
	}
	if got := repo.effectiveTurns[session.ID()]; got != 3 {
		t.Fatalf("effective turns = %d, want 3", got)
	}
	if len(repo.actions) != 3 {
		t.Fatalf("turn actions = %d, want 3", len(repo.actions))
	}
}

func TestTransactionFailuresRollbackCompleteSessionAndTurnState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	failingCreate := newTestService(repo, fakeTransactions{repo: repo, fail: true})
	_, err := failingCreate.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "failed-session"})
	if err == nil {
		t.Fatal("CreatePracticeSession() error = nil")
	}
	if len(repo.sessions) != 0 || len(repo.snapshots) != 0 || len(repo.active) != 0 || len(repo.sessionKeys) != 0 {
		t.Fatalf("partial session state remains")
	}
	session, _ := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	_, _ = service.StartPracticeSession(ctx, "user-1", session.ID())
	repo.failOperation = "save_turn_progress"
	_, err = service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: TurnOutcome{TurnID: "turn-1", SessionID: session.ID(), AnswerValid: true}})
	if err == nil {
		t.Fatal("ApplyTurnOutcome() error = nil")
	}
	if repo.effectiveTurns[session.ID()] != 0 || len(repo.actions) != 0 {
		t.Fatalf("partial turn state remains")
	}
	repo.failOperation = ""
	repo.effectiveTurns[session.ID()] = 5
	repo.failOperation = "save_turn_progress"
	_, err = service.ApplyTurnOutcome(ctx, ApplyTurnOutcomeCommand{UserID: "user-1", Outcome: TurnOutcome{TurnID: "turn-complete", SessionID: session.ID(), AnswerValid: true}})
	if err == nil {
		t.Fatal("completing ApplyTurnOutcome() error = nil")
	}
	stored := repo.sessions[session.ID()]
	if stored.Status() != PracticeSessionInProgress || repo.active[plan.ID()] != session.ID() {
		t.Fatalf("completed session was not rolled back: status=%q active=%q", stored.Status(), repo.active[plan.ID()])
	}
	if repo.effectiveTurns[session.ID()] != 5 || len(repo.actions) != 0 {
		t.Fatalf("completing turn progress was not rolled back")
	}
}

func TestServiceFreezesPreparationInputInSessionSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	preparation := newFakePreparationReader()
	service := newTestServiceWithPreparation(repo, fakeTransactions{repo: repo}, preparation)
	plan, err := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioType: ScenarioTypeInterview, ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	if err != nil {
		t.Fatalf("CreatePracticePlan() error = %v", err)
	}
	session, err := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	if err != nil {
		t.Fatalf("CreatePracticeSession() error = %v", err)
	}

	preparation.session.Role.FocusAreas[0] = "changed-by-a"
	preparation.session.PracticeFocuses[0] = "changed-by-a"
	preparation.session.ScenarioDefinition.Name = "changed-by-a"
	first, err := service.GetPracticeSessionSnapshot(ctx, "user-1", session.ID())
	if err != nil {
		t.Fatalf("GetPracticeSessionSnapshot() error = %v", err)
	}
	participants := first.Participants()
	participants[0].RoleSnapshot.FocusAreas[0] = "changed-by-consumer"

	second, err := service.GetPracticeSessionSnapshot(ctx, "user-1", session.ID())
	if err != nil {
		t.Fatalf("GetPracticeSessionSnapshot() second error = %v", err)
	}
	if got := second.Participants()[0].RoleSnapshot.FocusAreas[0]; got != "communication" {
		t.Fatalf("role focus = %q, want communication", got)
	}
	if got := second.PracticeFocuses()[0]; got != "focus" {
		t.Fatalf("practice focus = %q, want focus", got)
	}
	if got := second.ScenarioDefinition().Name; got != "Interview" {
		t.Fatalf("scenario name = %q, want Interview", got)
	}
	if got := second.ScenarioType(); got != ScenarioTypeInterview {
		t.Fatalf("scenario type = %q, want %q", got, ScenarioTypeInterview)
	}
	if got := second.Preparation().ResumeSnapshot; got != "resume" {
		t.Fatalf("resume snapshot = %q, want resume", got)
	}
}

func TestServiceManagesPracticePlanLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, err := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	if err != nil {
		t.Fatalf("CreatePracticePlan() error = %v", err)
	}

	updated, err := service.UpdatePracticePlan(ctx, UpdatePracticePlanCommand{UserID: "user-1", PracticePlanID: plan.ID(), ExpectedRevision: plan.Revision(), ScenarioConfigID: "config-2", PreparationProfileID: "profile-2", SelectedRoleIDs: []string{"role-2"}})
	if err != nil {
		t.Fatalf("UpdatePracticePlan() error = %v", err)
	}
	if updated.Revision() != plan.Revision()+1 || updated.ScenarioConfigID() != "config-2" {
		t.Fatalf("updated plan revision/config = %d/%q", updated.Revision(), updated.ScenarioConfigID())
	}
	archived, err := service.ArchivePracticePlan(ctx, "user-1", plan.ID(), updated.Revision())
	if err != nil || archived.Status() != PracticePlanArchived {
		t.Fatalf("ArchivePracticePlan() = %v, %v", archived, err)
	}
	if _, err := service.ArchivePracticePlan(ctx, "user-1", plan.ID(), updated.Revision()); err != nil {
		t.Fatalf("replayed ArchivePracticePlan() error = %v", err)
	}
	restored, err := service.RestorePracticePlan(ctx, "user-1", plan.ID(), updated.Revision())
	if err != nil || restored.Status() != PracticePlanReady {
		t.Fatalf("RestorePracticePlan() = %v, %v", restored, err)
	}
	if _, err := service.RestorePracticePlan(ctx, "user-1", plan.ID(), updated.Revision()); err != nil {
		t.Fatalf("replayed RestorePracticePlan() error = %v", err)
	}
	if err := service.DeleteEmptyPracticePlan(ctx, "user-1", plan.ID(), updated.Revision()); err != nil {
		t.Fatalf("DeleteEmptyPracticePlan() error = %v", err)
	}
	if _, err := repo.GetPlan(ctx, plan.ID()); !errors.Is(err, ErrPracticePlanNotFound) {
		t.Fatalf("GetPlan() error = %v, want %v", err, ErrPracticePlanNotFound)
	}
}

func TestRetryPracticePlanConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	preparation := newFakePreparationReader()
	service := newTestServiceWithPreparation(repo, fakeTransactions{repo: repo}, preparation)
	plan, _ := NewPracticePlan("plan-1", "user-1", "scenario-1", ScenarioTypeInterview, "config-1", "profile-1", []string{"role-1"})
	_ = plan.MarkConfigurationFailed()
	repo.plans[plan.ID()] = clonePlan(plan)

	retried, err := service.RetryPlanConfiguration(ctx, "user-1", plan.ID(), plan.Revision())
	if err != nil || retried.Status() != PracticePlanReady {
		t.Fatalf("RetryPlanConfiguration() = %v, %v", retried, err)
	}
}

func TestPlanLifecycleRejectsInvalidAccessAndState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	update := UpdatePracticePlanCommand{UserID: "user-1", PracticePlanID: plan.ID(), ExpectedRevision: plan.Revision(), ScenarioConfigID: "config-2", PreparationProfileID: "profile-2", SelectedRoleIDs: []string{"role-1"}}

	update.UserID = "other"
	if _, err := service.UpdatePracticePlan(ctx, update); !errors.Is(err, ErrPracticeResourceForbidden) {
		t.Fatalf("UpdatePracticePlan() ownership error = %v", err)
	}
	update.UserID, update.ExpectedRevision = "user-1", plan.Revision()+1
	if _, err := service.UpdatePracticePlan(ctx, update); !errors.Is(err, ErrPracticePlanRevisionConflict) {
		t.Fatalf("UpdatePracticePlan() revision error = %v", err)
	}
	update.ExpectedRevision = plan.Revision()
	session, _ := service.CreatePracticeSession(ctx, CreatePracticeSessionCommand{UserID: "user-1", PracticePlanID: plan.ID(), PlanRevision: plan.Revision(), ParticipantRoleID: "role-1", PracticeOptionID: "full", IdempotencyKey: "session"})
	if _, err := service.UpdatePracticePlan(ctx, update); !errors.Is(err, ErrPracticePlanHasActiveSession) {
		t.Fatalf("UpdatePracticePlan() active error = %v", err)
	}
	if _, err := service.ArchivePracticePlan(ctx, "user-1", plan.ID(), plan.Revision()); !errors.Is(err, ErrPracticePlanHasActiveSession) {
		t.Fatalf("ArchivePracticePlan() active error = %v", err)
	}
	_, _ = service.EndPracticeSessionEarly(ctx, "user-1", session.ID(), "done")
	if err := service.DeleteEmptyPracticePlan(ctx, "user-1", plan.ID(), plan.Revision()); !errors.Is(err, ErrPracticePlanHasSessions) {
		t.Fatalf("DeleteEmptyPracticePlan() history error = %v", err)
	}
}

func TestPlanLifecycleTransactionFailureRollsBackChanges(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := newTestService(repo, fakeTransactions{repo: repo})
	plan, _ := service.CreatePracticePlan(ctx, CreatePracticePlanCommand{UserID: "user-1", ScenarioDefinitionID: "scenario-1", ScenarioConfigID: "config-1", PreparationProfileID: "profile-1", SelectedRoleIDs: []string{"role-1"}, IdempotencyKey: "plan"})
	failing := newTestService(repo, fakeTransactions{repo: repo, fail: true})

	_, err := failing.UpdatePracticePlan(ctx, UpdatePracticePlanCommand{UserID: "user-1", PracticePlanID: plan.ID(), ExpectedRevision: plan.Revision(), ScenarioConfigID: "config-2", PreparationProfileID: "profile-2", SelectedRoleIDs: []string{"role-2"}})
	if err == nil {
		t.Fatal("UpdatePracticePlan() error = nil")
	}
	stored, _ := repo.GetPlan(ctx, plan.ID())
	if stored.Revision() != plan.Revision() || stored.ScenarioConfigID() != plan.ScenarioConfigID() {
		t.Fatalf("stored plan changed after rollback: revision/config = %d/%q", stored.Revision(), stored.ScenarioConfigID())
	}
}

type fakePreparationReader struct {
	session       SessionPreparation
	validateErr   error
	prepareErr    error
	validateCalls int
	prepareCalls  int
}

type fixedPreparationReader struct{ session SessionPreparation }

func (fixedPreparationReader) ValidatePlan(context.Context, CreatePracticePlanCommand) error {
	return nil
}
func (f fixedPreparationReader) PrepareSession(context.Context, *PracticePlan, string, string) (SessionPreparation, error) {
	return f.session, nil
}

func newFakePreparationReader() *fakePreparationReader {
	return &fakePreparationReader{session: SessionPreparation{
		ScenarioDefinition: ScenarioDefinitionSnapshot{ScenarioDefinitionID: "scenario-1", ScenarioType: ScenarioTypeInterview, Name: "Interview", Version: 1, Status: "active"},
		ScenarioConfig:     ScenarioConfigSnapshot{ScenarioConfigID: "config-1", ScenarioDefinitionID: "scenario-1", ConfigType: "INTERVIEW", Version: 1},
		Preparation:        PreparationSnapshot{PreparationSnapshotID: "preparation-snapshot-1", SourceProfileID: "profile-1", SourceVersion: 1, ResumeSnapshot: "resume", JobDescriptionSnapshot: "job", BackgroundSnapshot: "background", CreatedAt: time.Date(2026, 7, 17, 7, 0, 0, 0, time.UTC)},
		Role:               RoleSnapshot{RoleDefinitionID: "role-1", ScenarioDefinitionID: "scenario-1", RoleType: "INTERVIEWER", DisplayName: "Mia", FocusAreas: []string{"communication"}, Version: 1},
		InterviewerSubject: SubjectRef{Namespace: "preparation", SubjectID: "interviewer-1"},
		PracticeOption:     PracticeOptionSnapshot{PracticeOptionID: "full", ScenarioDefinitionID: "scenario-1", PracticeOptionType: "FULL_SIMULATION", DisplayName: "Full", Version: 1},
		PracticeFocuses:    []string{"focus"}, Mode: PracticeModeFullSimulation, TargetObjectiveIDs: []string{"objective-1"},
	}}
}

func (f *fakePreparationReader) ValidatePlan(context.Context, CreatePracticePlanCommand) error {
	f.validateCalls++
	return f.validateErr
}
func (f *fakePreparationReader) PrepareSession(_ context.Context, _ *PracticePlan, roleID, optionID string) (SessionPreparation, error) {
	f.prepareCalls++
	if f.prepareErr != nil {
		return SessionPreparation{}, f.prepareErr
	}
	mode := PracticeModeFullSimulation
	if optionID == "focused" {
		mode = PracticeModeFocusedPractice
	}
	result := f.session
	result.Role.RoleDefinitionID = roleID
	result.PracticeOption.PracticeOptionID = optionID
	result.PracticeOption.RoleDefinitionID = roleID
	result.Mode = mode
	return result, nil
}

type fakeIDs struct {
	mu   sync.Mutex
	next int
}

func (f *fakeIDs) NewID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	return fmt.Sprintf("id-%d", f.next)
}

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time {
	if f.now.IsZero() {
		return time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	}
	return f.now
}

type fakeRepository struct {
	mu             sync.Mutex
	plans          map[string]*PracticePlan
	planKeys       map[string]string
	planCommands   map[string]CreatePracticePlanCommand
	sessions       map[string]*PracticeSession
	sessionKeys    map[string]string
	snapshots      map[string]PracticeSessionSnapshot
	active         map[string]string
	actions        map[string]NextAction
	effectiveTurns map[string]int
	calls          int
	failOperation  string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{plans: map[string]*PracticePlan{}, planKeys: map[string]string{}, planCommands: map[string]CreatePracticePlanCommand{}, sessions: map[string]*PracticeSession{}, sessionKeys: map[string]string{}, snapshots: map[string]PracticeSessionSnapshot{}, active: map[string]string{}, actions: map[string]NextAction{}, effectiveTurns: map[string]int{}}
}
func (r *fakeRepository) FindPlanCreation(_ context.Context, key string) (*PracticePlan, CreatePracticePlanCommand, bool, error) {
	id, ok := r.planKeys[key]
	if !ok {
		return nil, CreatePracticePlanCommand{}, false, nil
	}
	return clonePlan(r.plans[id]), cloneCreatePlanCommand(r.planCommands[key]), true, nil
}
func (r *fakeRepository) CreatePlan(_ context.Context, key string, command CreatePracticePlanCommand, plan *PracticePlan) (*PracticePlan, CreatePracticePlanCommand, bool, error) {
	r.calls++
	if r.failOperation == "create_plan" {
		return nil, CreatePracticePlanCommand{}, false, errors.New("create plan failed")
	}
	if id, ok := r.planKeys[key]; ok {
		return clonePlan(r.plans[id]), cloneCreatePlanCommand(r.planCommands[key]), false, nil
	}
	r.plans[plan.ID()] = clonePlan(plan)
	r.planKeys[key] = plan.ID()
	r.planCommands[key] = cloneCreatePlanCommand(command)
	return clonePlan(plan), cloneCreatePlanCommand(command), true, nil
}
func (r *fakeRepository) FindSessionCreation(_ context.Context, key string) (*PracticeSession, PracticeSessionSnapshot, bool, error) {
	id, ok := r.sessionKeys[key]
	if !ok {
		return nil, PracticeSessionSnapshot{}, false, nil
	}
	session := r.sessions[id]
	return cloneSession(session), cloneSnapshot(r.snapshots[session.SnapshotID()]), true, nil
}
func (r *fakeRepository) GetPlan(_ context.Context, id string) (*PracticePlan, error) {
	p, ok := r.plans[id]
	if !ok {
		return nil, ErrPracticePlanNotFound
	}
	return clonePlan(p), nil
}
func (r *fakeRepository) LockPlanForUpdate(ctx context.Context, id string) (*PracticePlan, error) {
	if !inFakeTransaction(ctx) {
		return nil, errors.New("plan lock requires transaction")
	}
	return r.GetPlan(ctx, id)
}
func (r *fakeRepository) SavePlan(_ context.Context, plan *PracticePlan) error {
	if r.failOperation == "save_plan" {
		return errors.New("save plan failed")
	}
	r.plans[plan.ID()] = clonePlan(plan)
	return nil
}
func (r *fakeRepository) HasActiveSession(_ context.Context, planID string) (bool, error) {
	_, ok := r.active[planID]
	return ok, nil
}
func (r *fakeRepository) HasSessions(_ context.Context, planID string) (bool, error) {
	for _, session := range r.sessions {
		if session.PlanID() == planID {
			return true, nil
		}
	}
	return false, nil
}
func (r *fakeRepository) DeletePlan(_ context.Context, planID string) error {
	if _, ok := r.plans[planID]; !ok {
		return ErrPracticePlanNotFound
	}
	delete(r.plans, planID)
	for key, id := range r.planKeys {
		if id == planID {
			delete(r.planKeys, key)
		}
	}
	return nil
}
func (r *fakeRepository) CreateActiveSession(_ context.Context, key, planID string, session *PracticeSession, snapshot PracticeSessionSnapshot) (*PracticeSession, PracticeSessionSnapshot, bool, error) {
	if r.failOperation == "create_session" {
		return nil, PracticeSessionSnapshot{}, false, errors.New("create session failed")
	}
	if id, ok := r.sessionKeys[key]; ok {
		existing := r.sessions[id]
		return cloneSession(existing), cloneSnapshot(r.snapshots[existing.SnapshotID()]), false, nil
	}
	if _, ok := r.active[planID]; ok {
		return nil, PracticeSessionSnapshot{}, false, ErrPracticePlanHasActiveSession
	}
	r.sessions[session.ID()] = cloneSession(session)
	r.sessionKeys[key] = session.ID()
	r.snapshots[snapshot.ID()] = cloneSnapshot(snapshot)
	r.active[planID] = session.ID()
	return cloneSession(session), cloneSnapshot(snapshot), true, nil
}
func (r *fakeRepository) GetSession(_ context.Context, id string) (*PracticeSession, error) {
	s, ok := r.sessions[id]
	if !ok {
		return nil, ErrPracticeSessionNotFound
	}
	return cloneSession(s), nil
}
func (r *fakeRepository) LockSessionForUpdate(ctx context.Context, id string) (*PracticeSession, error) {
	if !inFakeTransaction(ctx) {
		return nil, errors.New("session lock requires transaction")
	}
	return r.GetSession(ctx, id)
}
func (r *fakeRepository) SaveSession(_ context.Context, session *PracticeSession) error {
	if r.failOperation == "save_session" {
		return errors.New("save session failed")
	}
	r.sessions[session.ID()] = cloneSession(session)
	if session.Status() == PracticeSessionCompleted || session.Status() == PracticeSessionEndedEarly {
		delete(r.active, session.PlanID())
	}
	return nil
}
func (r *fakeRepository) GetSnapshot(_ context.Context, snapshotID string) (PracticeSessionSnapshot, error) {
	if r.failOperation == "get_snapshot" {
		return PracticeSessionSnapshot{}, errors.New("get snapshot failed")
	}
	return cloneSnapshot(r.snapshots[snapshotID]), nil
}

func newTestService(repository Repository, transactions TransactionManager) *Service {
	return newTestServiceWithPreparation(repository, transactions, newFakePreparationReader())
}
func newTestServiceWithPreparation(repository Repository, transactions TransactionManager, preparation PreparationReader) *Service {
	return newTestServiceWithClock(repository, transactions, preparation, fakeClock{})
}
func newTestServiceWithClock(repository Repository, transactions TransactionManager, preparation PreparationReader, clock Clock) *Service {
	return newService(Dependencies{PreparationReader: preparation, Repository: repository, TransactionManager: transactions, IDGenerator: &fakeIDs{}, Clock: clock})
}
func (r *fakeRepository) FindTurnAction(_ context.Context, sessionID, turnID string) (NextAction, bool, error) {
	a, ok := r.actions[sessionID+":"+turnID]
	return a, ok, nil
}
func (r *fakeRepository) SaveTurnProgress(_ context.Context, sessionID, turnID string, turns int, action NextAction) error {
	if r.failOperation == "save_turn_progress" {
		return errors.New("save turn progress failed")
	}
	r.effectiveTurns[sessionID] = turns
	r.actions[sessionID+":"+turnID] = action
	return nil
}
func (r *fakeRepository) EffectiveTurnCount(_ context.Context, sessionID string) (int, error) {
	return r.effectiveTurns[sessionID], nil
}

type fakeTransactions struct {
	repo *fakeRepository
	fail bool
}

func (f fakeTransactions) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	f.repo.mu.Lock()
	defer f.repo.mu.Unlock()
	before := cloneRepositoryState(f.repo)
	err := fn(context.WithValue(ctx, fakeTransactionKey{}, true))
	if err == nil && f.fail {
		err = errors.New("transaction failed")
	}
	if err != nil {
		restoreRepositoryState(f.repo, before)
	}
	return err
}

type fakeTransactionKey struct{}

func inFakeTransaction(ctx context.Context) bool {
	value, _ := ctx.Value(fakeTransactionKey{}).(bool)
	return value
}

type fakeRepositoryState struct {
	plans          map[string]*PracticePlan
	planKeys       map[string]string
	planCommands   map[string]CreatePracticePlanCommand
	sessions       map[string]*PracticeSession
	sessionKeys    map[string]string
	snapshots      map[string]PracticeSessionSnapshot
	active         map[string]string
	actions        map[string]NextAction
	effectiveTurns map[string]int
	calls          int
}

func cloneRepositoryState(r *fakeRepository) fakeRepositoryState {
	state := fakeRepositoryState{plans: map[string]*PracticePlan{}, planKeys: cloneMap(r.planKeys), planCommands: map[string]CreatePracticePlanCommand{}, sessions: map[string]*PracticeSession{}, sessionKeys: cloneMap(r.sessionKeys), snapshots: map[string]PracticeSessionSnapshot{}, active: cloneMap(r.active), actions: cloneMap(r.actions), effectiveTurns: cloneMap(r.effectiveTurns), calls: r.calls}
	for key, command := range r.planCommands {
		state.planCommands[key] = cloneCreatePlanCommand(command)
	}
	for id, plan := range r.plans {
		state.plans[id] = clonePlan(plan)
	}
	for id, session := range r.sessions {
		state.sessions[id] = cloneSession(session)
	}
	for id, snapshot := range r.snapshots {
		state.snapshots[id] = cloneSnapshot(snapshot)
	}
	return state
}

func restoreRepositoryState(r *fakeRepository, state fakeRepositoryState) {
	r.plans, r.planKeys, r.planCommands, r.sessions, r.sessionKeys = state.plans, state.planKeys, state.planCommands, state.sessions, state.sessionKeys
	r.snapshots, r.active, r.actions, r.effectiveTurns, r.calls = state.snapshots, state.active, state.actions, state.effectiveTurns, state.calls
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
func cloneCreatePlanCommand(command CreatePracticePlanCommand) CreatePracticePlanCommand {
	command.SelectedRoleIDs = cloneStrings(command.SelectedRoleIDs)
	return command
}
func clonePlan(plan *PracticePlan) *PracticePlan {
	if plan == nil {
		return nil
	}
	result := *plan
	result.selectedRoleIDs = cloneStrings(plan.selectedRoleIDs)
	return &result
}
func cloneSession(session *PracticeSession) *PracticeSession {
	if session == nil {
		return nil
	}
	result := *session
	if session.startedAt != nil {
		value := *session.startedAt
		result.startedAt = &value
	}
	if session.endedAt != nil {
		value := *session.endedAt
		result.endedAt = &value
	}
	return &result
}
func cloneSnapshot(snapshot PracticeSessionSnapshot) PracticeSessionSnapshot {
	result := snapshot
	result.practiceFocuses = cloneStrings(snapshot.practiceFocuses)
	result.sessionPolicy = snapshot.sessionPolicy
	result.sessionPolicy.targetObjectiveIDs = cloneStrings(snapshot.sessionPolicy.targetObjectiveIDs)
	result.participants = append([]PracticeParticipant(nil), snapshot.participants...)
	for index := range result.participants {
		result.participants[index].roleSnapshot = cloneRoleSnapshot(result.participants[index].roleSnapshot)
	}
	return result
}
