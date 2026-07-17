package practice_test

import (
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
)

func TestPracticePlanLifecycle(t *testing.T) {
	plan, err := practice.NewPracticePlan("plan-1", "user-1", "scenario-1", practice.ScenarioTypeInterview, "config-1", "profile-1", []string{"role-1"})
	if err != nil {
		t.Fatalf("NewPracticePlan() error = %v", err)
	}

	if got := plan.Status(); got != practice.PracticePlanConfiguring {
		t.Fatalf("Status() = %q, want %q", got, practice.PracticePlanConfiguring)
	}
	if err := plan.MarkConfigurationFailed(); err != nil {
		t.Fatalf("MarkConfigurationFailed() error = %v", err)
	}
	if err := plan.MarkReady(); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := plan.Update("config-2", "profile-2", []string{"role-2"}, false); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := plan.Revision(); got != 2 {
		t.Fatalf("Revision() = %d, want 2", got)
	}
	if err := plan.Archive(false); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if err := plan.Restore(); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if got := plan.Status(); got != practice.PracticePlanReady {
		t.Fatalf("Status() = %q, want %q", got, practice.PracticePlanReady)
	}
}

func TestPracticePlanRejectsActiveSessionChanges(t *testing.T) {
	plan := readyPlan(t)

	if err := plan.Update("config-2", "profile-2", []string{"role-2"}, true); !errors.Is(err, practice.ErrPracticePlanHasActiveSession) {
		t.Fatalf("Update() error = %v, want %v", err, practice.ErrPracticePlanHasActiveSession)
	}
	if err := plan.Archive(true); !errors.Is(err, practice.ErrPracticePlanHasActiveSession) {
		t.Fatalf("Archive() error = %v, want %v", err, practice.ErrPracticePlanHasActiveSession)
	}
}

func TestPracticePlanRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		userID   string
		scenario string
		config   string
		profile  string
		roleIDs  []string
	}{
		{name: "empty plan id", userID: "user-1", scenario: "scenario-1", config: "config-1", profile: "profile-1", roleIDs: []string{"role-1"}},
		{name: "empty user id", id: "plan-1", scenario: "scenario-1", config: "config-1", profile: "profile-1", roleIDs: []string{"role-1"}},
		{name: "empty scenario id", id: "plan-1", userID: "user-1", config: "config-1", profile: "profile-1", roleIDs: []string{"role-1"}},
		{name: "no roles", id: "plan-1", userID: "user-1", scenario: "scenario-1", config: "config-1", profile: "profile-1"},
		{name: "five roles", id: "plan-1", userID: "user-1", scenario: "scenario-1", config: "config-1", profile: "profile-1", roleIDs: []string{"role-1", "role-2", "role-3", "role-4", "role-5"}},
		{name: "empty role", id: "plan-1", userID: "user-1", scenario: "scenario-1", config: "config-1", profile: "profile-1", roleIDs: []string{"role-1", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := practice.NewPracticePlan(tt.id, tt.userID, tt.scenario, practice.ScenarioTypeInterview, tt.config, tt.profile, tt.roleIDs)
			if !errors.Is(err, practice.ErrPracticePlanInvalid) {
				t.Fatalf("NewPracticePlan() error = %v, want %v", err, practice.ErrPracticePlanInvalid)
			}
		})
	}
}

func TestPracticePlanAcceptsFourRoles(t *testing.T) {
	roles := []string{"role-1", "role-2", "role-3", "role-4"}
	plan, err := practice.NewPracticePlan("plan-1", "user-1", "scenario-1", practice.ScenarioTypeInterview, "config-1", "profile-1", roles)
	if err != nil {
		t.Fatalf("NewPracticePlan() error = %v", err)
	}
	if err := plan.MarkReady(); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := plan.Update("config-2", "profile-2", roles, false); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestPracticeSessionLifecycle(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	session, err := practice.NewPracticeSession("session-1", "plan-1", practice.ScenarioTypeInterview, "snapshot-1", createdAt)
	if err != nil {
		t.Fatalf("NewPracticeSession() error = %v", err)
	}
	if err := session.Start(createdAt.Add(time.Minute)); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := session.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if err := session.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := session.Complete(createdAt.Add(10 * time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := session.Resume(); !errors.Is(err, practice.ErrPracticeSessionTransitionNotAllowed) {
		t.Fatalf("Resume() after completion error = %v, want %v", err, practice.ErrPracticeSessionTransitionNotAllowed)
	}
}

func TestPracticeSessionRejectsStartingEarlyEnd(t *testing.T) {
	session, err := practice.NewPracticeSession("session-1", "plan-1", practice.ScenarioTypeInterview, "snapshot-1", time.Now())
	if err != nil {
		t.Fatalf("NewPracticeSession() error = %v", err)
	}

	err = session.EndEarly(time.Now(), "user_requested")
	if !errors.Is(err, practice.ErrPracticeSessionTransitionNotAllowed) {
		t.Fatalf("EndEarly() error = %v, want %v", err, practice.ErrPracticeSessionTransitionNotAllowed)
	}
}

func TestPracticeSessionRejectsInvalidTime(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	session, err := practice.NewPracticeSession("session-1", "plan-1", practice.ScenarioTypeInterview, "snapshot-1", createdAt)
	if err != nil {
		t.Fatalf("NewPracticeSession() error = %v", err)
	}

	err = session.Start(createdAt.Add(-time.Second))
	if !errors.Is(err, practice.ErrPracticeSessionInvalidTime) {
		t.Fatalf("Start() error = %v, want %v", err, practice.ErrPracticeSessionInvalidTime)
	}
}

func TestPracticeSessionSnapshotCopiesCollections(t *testing.T) {
	roleFocusAreas := []string{"communication", "clarity"}
	focuses := []string{"communication", "clarity"}
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}
	snapshot, err := practice.NewPracticeSessionSnapshot(practice.PracticeSessionSnapshotInput{
		ID:           "snapshot-1",
		SessionID:    "session-1",
		PlanRevision: 1,
		ScenarioType: practice.ScenarioTypeInterview,
		ScenarioDefinition: practice.ScenarioDefinitionSnapshot{
			ScenarioDefinitionID: "scenario-1", ScenarioType: practice.ScenarioTypeInterview, Name: "Interview", Version: 1, Status: "active",
		},
		ScenarioConfig: practice.ScenarioConfigSnapshot{
			ScenarioConfigID: "config-1", ScenarioDefinitionID: "scenario-1", ConfigType: "INTERVIEW", Version: 1,
		},
		Preparation: practice.PreparationSnapshot{
			PreparationSnapshotID: "preparation-1", SourceProfileID: "profile-1", SourceVersion: 1, ResumeSnapshot: "resume", JobDescriptionSnapshot: "job", BackgroundSnapshot: "background", CreatedAt: time.Now(),
		},
		Participants: []practice.PracticeParticipantInput{{
			ID: "participant-1", SessionID: "session-1", ParticipantRole: practice.ParticipantRoleInterviewer,
			SubjectRef: practice.SubjectRef{Namespace: "preparation", SubjectID: "interviewer-1"}, RoleDefinitionID: "role-1", ParticipantOrder: 1,
			RoleSnapshot: practice.RoleSnapshot{RoleDefinitionID: "role-1", ScenarioDefinitionID: "scenario-1", RoleType: "INTERVIEWER", DisplayName: "Mia", FocusAreas: roleFocusAreas, Version: 1},
		}},
		PracticeOption:  practice.PracticeOptionSnapshot{PracticeOptionID: "option-1", ScenarioDefinitionID: "scenario-1", RoleDefinitionID: "role-1", PracticeOptionType: "FULL_SIMULATION", DisplayName: "Full", Version: 1},
		PracticeFocuses: focuses,
		SessionPolicy:   policy,
		CreatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("NewPracticeSessionSnapshot() error = %v", err)
	}

	roleFocusAreas[0] = "changed"
	focuses[0] = "changed"
	gotParticipants := snapshot.Participants()
	gotFocuses := snapshot.PracticeFocuses()
	gotParticipants[0].RoleSnapshot.FocusAreas[0] = "changed-again"
	gotFocuses[0] = "changed-again"

	if got := snapshot.Participants()[0].RoleSnapshot.FocusAreas[0]; got != "communication" {
		t.Fatalf("Participants()[0].RoleSnapshot.FocusAreas[0] = %q, want communication", got)
	}
	if got := snapshot.PracticeFocuses()[0]; got != "communication" {
		t.Fatalf("PracticeFocuses()[0] = %q, want communication", got)
	}
}

func TestPracticeSessionSnapshotRequiresMS1InterviewParticipants(t *testing.T) {
	valid := validInterviewSnapshotInput(t)

	tests := []struct {
		name   string
		mutate func(*practice.PracticeSessionSnapshotInput)
	}{
		{name: "没有面试官", mutate: func(input *practice.PracticeSessionSnapshotInput) { input.Participants = nil }},
		{name: "多于一个参与者", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.Participants = append(input.Participants, input.Participants[0])
		}},
		{name: "参与者不是面试官", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.Participants[0].ParticipantRole = practice.ParticipantRole("OBSERVER")
		}},
		{name: "主体命名空间为空", mutate: func(input *practice.PracticeSessionSnapshotInput) { input.Participants[0].SubjectRef.Namespace = "" }},
		{name: "主体标识为空", mutate: func(input *practice.PracticeSessionSnapshotInput) { input.Participants[0].SubjectRef.SubjectID = "" }},
		{name: "面试官缺少角色", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.Participants[0].RoleDefinitionID = ""
			input.Participants[0].RoleSnapshot = practice.RoleSnapshot{}
		}},
		{name: "面试官角色不一致", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.Participants[0].RoleSnapshot.RoleDefinitionID = "other"
		}},
		{name: "角色来自其他场景", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.Participants[0].RoleSnapshot.ScenarioDefinitionID = "other"
		}},
		{name: "选项来自其他场景", mutate: func(input *practice.PracticeSessionSnapshotInput) {
			input.PracticeOption.ScenarioDefinitionID = "other"
		}},
		{name: "选项属于其他角色", mutate: func(input *practice.PracticeSessionSnapshotInput) { input.PracticeOption.RoleDefinitionID = "other" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			input.Participants = append([]practice.PracticeParticipantInput(nil), valid.Participants...)
			tt.mutate(&input)
			_, err := practice.NewPracticeSessionSnapshot(input)
			if !errors.Is(err, practice.ErrPracticeSessionSnapshotInvalid) {
				t.Fatalf("NewPracticeSessionSnapshot() error = %v, want %v", err, practice.ErrPracticeSessionSnapshotInvalid)
			}
		})
	}
}

func TestPracticeSessionSnapshotKeepsOneInterviewer(t *testing.T) {
	input := validInterviewSnapshotInput(t)
	snapshot, err := practice.NewPracticeSessionSnapshot(input)
	if err != nil {
		t.Fatalf("NewPracticeSessionSnapshot() error = %v", err)
	}

	participants := snapshot.Participants()
	if len(participants) != 1 {
		t.Fatalf("Participants() count = %d, want 1", len(participants))
	}
	if got := participants[0].ParticipantRole; got != practice.ParticipantRoleInterviewer {
		t.Fatalf("interviewer role = %q", got)
	}
}

func TestNewPracticeSessionSnapshotRejectsInvalidPolicy(t *testing.T) {
	_, err := practice.NewPracticeSessionSnapshot(practice.PracticeSessionSnapshotInput{
		ID: "snapshot-1", SessionID: "session-1", PlanRevision: 1, ScenarioType: practice.ScenarioTypeInterview,
		ScenarioDefinition: practice.ScenarioDefinitionSnapshot{ScenarioDefinitionID: "scenario-1", ScenarioType: practice.ScenarioTypeInterview, Version: 1},
		ScenarioConfig:     practice.ScenarioConfigSnapshot{ScenarioConfigID: "config-1", ScenarioDefinitionID: "scenario-1", Version: 1},
		Preparation:        practice.PreparationSnapshot{PreparationSnapshotID: "preparation-1", SourceProfileID: "profile-1", SourceVersion: 1, CreatedAt: time.Now()},
		Participants:       []practice.PracticeParticipantInput{{ID: "participant-1", SessionID: "session-1", ParticipantRole: practice.ParticipantRoleInterviewer, SubjectRef: practice.SubjectRef{Namespace: "preparation", SubjectID: "interviewer-1"}, RoleDefinitionID: "role-1", RoleSnapshot: practice.RoleSnapshot{RoleDefinitionID: "role-1", ScenarioDefinitionID: "scenario-1", Version: 1}, ParticipantOrder: 1}},
		PracticeOption:     practice.PracticeOptionSnapshot{PracticeOptionID: "option-1", Version: 1},
		SessionPolicy:      practice.PracticeSessionPolicy{}, CreatedAt: time.Now(),
	})
	if !errors.Is(err, practice.ErrPracticeSessionSnapshotInvalid) {
		t.Fatalf("NewPracticeSessionSnapshot() error = %v, want %v", err, practice.ErrPracticeSessionSnapshotInvalid)
	}
}

func validInterviewSnapshotInput(t *testing.T) practice.PracticeSessionSnapshotInput {
	t.Helper()
	policy, err := practice.NewPracticeSessionPolicy(practice.PracticeModeFullSimulation, []string{"objective-1"})
	if err != nil {
		t.Fatalf("NewPracticeSessionPolicy() error = %v", err)
	}
	return practice.PracticeSessionSnapshotInput{
		ID: "snapshot-1", SessionID: "session-1", PlanRevision: 1, ScenarioType: practice.ScenarioTypeInterview,
		ScenarioDefinition: practice.ScenarioDefinitionSnapshot{ScenarioDefinitionID: "scenario-1", ScenarioType: practice.ScenarioTypeInterview, Version: 1},
		ScenarioConfig:     practice.ScenarioConfigSnapshot{ScenarioConfigID: "config-1", ScenarioDefinitionID: "scenario-1", Version: 1},
		Preparation:        practice.PreparationSnapshot{PreparationSnapshotID: "preparation-1", SourceProfileID: "profile-1", SourceVersion: 1, CreatedAt: time.Now()},
		Participants:       []practice.PracticeParticipantInput{{ID: "participant-1", SessionID: "session-1", ParticipantRole: practice.ParticipantRoleInterviewer, SubjectRef: practice.SubjectRef{Namespace: "preparation", SubjectID: "interviewer-1"}, RoleDefinitionID: "role-1", RoleSnapshot: practice.RoleSnapshot{RoleDefinitionID: "role-1", ScenarioDefinitionID: "scenario-1", Version: 1}, ParticipantOrder: 1}},
		PracticeOption:     practice.PracticeOptionSnapshot{PracticeOptionID: "option-1", ScenarioDefinitionID: "scenario-1", RoleDefinitionID: "role-1", Version: 1},
		SessionPolicy:      policy, CreatedAt: time.Now(),
	}
}

func readyPlan(t *testing.T) *practice.PracticePlan {
	t.Helper()
	plan, err := practice.NewPracticePlan("plan-1", "user-1", "scenario-1", practice.ScenarioTypeInterview, "config-1", "profile-1", []string{"role-1"})
	if err != nil {
		t.Fatalf("NewPracticePlan() error = %v", err)
	}
	if err := plan.MarkReady(); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	return plan
}
