package practice

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

func TestTargetedPlanPreviewFreezesServerRecommendationWithoutSession(
	t *testing.T,
) {
	t.Parallel()

	preparation := targetedPreparationFixture()
	catalog := targetedCatalogFixture()
	var captured persistence.CreatePlanCommand
	sessionCreates := 0
	repository := &contextRepositoryStub{
		createPlan: func(
			command persistence.CreatePlanCommand,
		) (persistence.Plan, bool, error) {
			captured = command
			return targetedPlanFromCommand(
				contextActorFixture().UserID,
				command,
			), false, nil
		},
		createSession: func(
			persistence.CreateContextSessionCommand,
		) (persistence.ContextSessionBootstrap, bool, error) {
			sessionCreates++
			return persistence.ContextSessionBootstrap{}, false, nil
		},
	}
	application, err := NewContextApplication(
		repository,
		&targetedContextIDs{values: []string{"plan-targeted"}},
		targetedAnchorReader{},
		targetedPreparationReader{snapshot: preparation},
		targetedCatalogReader{selection: catalog},
	)
	if err != nil {
		t.Fatalf("NewContextApplication: %v", err)
	}

	plan, replayed, err := application.CreatePlan(
		context.Background(),
		contextActorFixture(),
		"targeted-plan-key",
		CreatePlanRequest{
			AgentThreadID:         "thread-1",
			MatterID:              "matter-1",
			PreparationSnapshotID: preparation.ID,
		},
	)
	if err != nil || replayed {
		t.Fatalf("CreatePlan = (%+v, %t, %v)", plan, replayed, err)
	}
	if captured.PreparationSnapshot == nil ||
		captured.PreparationSnapshot.ID != preparation.ID ||
		captured.CatalogSnapshot == nil ||
		captured.CatalogSnapshot.PracticeOption.ID !=
			catalog.PracticeOption.ID ||
		captured.SessionPolicy == nil ||
		len(captured.PracticeFocuses) == 0 ||
		sessionCreates != 0 {
		t.Fatalf("frozen targeted command = %+v", captured)
	}
}

func TestTargetedPlanRejectsSnapshotWithoutConfirmedTarget(t *testing.T) {
	t.Parallel()

	legacy := targetedPreparationFixture()
	legacy.SourceJobTargetID = ""
	legacy.SourceJobTargetConfirmationVersion = 0
	legacy.JobTargetInputSnapshot = nil
	legacy.JobTargetCandidateSnapshot = nil
	application, err := NewContextApplication(
		&contextRepositoryStub{},
		&targetedContextIDs{values: []string{"must-not-be-used"}},
		targetedAnchorReader{},
		targetedPreparationReader{snapshot: legacy},
		panicContextDependency{},
	)
	if err != nil {
		t.Fatalf("NewContextApplication: %v", err)
	}

	_, _, err = application.CreatePlan(
		context.Background(),
		contextActorFixture(),
		"unconfirmed-plan-key",
		CreatePlanRequest{
			AgentThreadID:         "thread-1",
			MatterID:              "matter-1",
			PreparationSnapshotID: legacy.ID,
		},
	)
	if !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("unconfirmed Snapshot error = %v", err)
	}
}

func TestTargetedInterviewCreateAndRevisionRejectMultipleRoles(t *testing.T) {
	t.Parallel()

	multiCatalog := targetedMultiRoleFullCatalogFixture()
	preparation := targetedPreparationFixture()
	preparation.JobTargetCandidateSnapshot.
		CatalogRecommendation.SelectedRoleIDs = roleSnapshotIDs(
		multiCatalog.SelectedRoles,
	)
	preparation.JobTargetCandidateSnapshot.
		CatalogRecommendation.PracticeOptionID =
		multiCatalog.PracticeOption.ID

	createApplication, err := NewContextApplication(
		&contextRepositoryStub{},
		panicIDGenerator{},
		targetedAnchorReader{},
		targetedPreparationReader{snapshot: preparation},
		targetedCatalogReader{selection: multiCatalog},
	)
	if err != nil {
		t.Fatalf("NewContextApplication for create: %v", err)
	}
	if _, _, err := createApplication.CreatePlan(
		context.Background(),
		contextActorFixture(),
		"multi-role-create-key",
		CreatePlanRequest{
			AgentThreadID:         "thread-1",
			MatterID:              "matter-1",
			PreparationSnapshotID: preparation.ID,
		},
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("multi-role targeted CreatePlan error = %v", err)
	}

	plan := targetedPlanFixture()
	revisionApplication, err := NewContextApplication(
		&contextRepositoryStub{
			getPlan: func(string) (persistence.Plan, error) {
				return plan, nil
			},
		},
		panicIDGenerator{},
		panicContextDependency{},
		panicContextDependency{},
		targetedCatalogReader{selection: multiCatalog},
	)
	if err != nil {
		t.Fatalf("NewContextApplication for revision: %v", err)
	}
	if _, _, err := revisionApplication.UpdatePlan(
		context.Background(),
		contextActorFixture(),
		plan.ID,
		"multi-role-revision-key",
		UpdatePlanRequest{
			ExpectedPlanRevision:  1,
			SelectedRoleIDs:       roleSnapshotIDs(multiCatalog.SelectedRoles),
			PracticeOptionID:      multiCatalog.PracticeOption.ID,
			PracticeOptionVersion: multiCatalog.PracticeOption.Version,
			MaxEffectiveTurns:     6,
		},
	); !errors.Is(err, persistence.ErrConflict) {
		t.Fatalf("multi-role targeted UpdatePlan error = %v", err)
	}
}

func TestTargetedSingleRoleFullSimulationCanStart(t *testing.T) {
	t.Parallel()

	plan := targetedPlanFixture()
	catalog := *plan.CatalogSnapshot
	catalog.PracticeOption = persistence.PracticeOptionSnapshot{
		ID:                   "option-full",
		ScenarioDefinitionID: plan.ScenarioDefinitionID,
		Type:                 "FULL_SIMULATION",
		DisplayName:          "Full simulation",
		Version:              1,
	}
	policy := defaultContextSessionPolicy(
		catalog.ScenarioConfig,
		catalog.PracticeOption,
	)
	plan.CatalogSnapshot = &catalog
	plan.SessionPolicy = &policy
	var captured persistence.CreateContextSessionCommand
	repository := &contextRepositoryStub{
		getPlan: func(string) (persistence.Plan, error) {
			return plan, nil
		},
		createSession: func(
			command persistence.CreateContextSessionCommand,
		) (persistence.ContextSessionBootstrap, bool, error) {
			captured = command
			return persistence.ContextSessionBootstrap{
				Session:  persistence.ContextSession{ID: command.SessionID},
				Snapshot: command.Snapshot,
			}, false, nil
		},
	}
	application, err := NewContextApplication(
		repository,
		&targetedContextIDs{values: []string{
			"session-full",
			"snapshot-full",
			"participant-full-interviewer",
			"participant-full-candidate",
		}},
		panicContextDependency{},
		panicContextDependency{},
		panicContextDependency{},
	)
	if err != nil {
		t.Fatalf("NewContextApplication: %v", err)
	}
	if _, _, err := application.CreateSession(
		context.Background(),
		contextActorFixture(),
		plan.ID,
		"single-role-full-start",
		CreateSessionRequest{ExpectedPlanRevision: plan.Revision},
	); err != nil {
		t.Fatalf("single-role FULL_SIMULATION start: %v", err)
	}
	if captured.Snapshot.PracticeOption.Type != "FULL_SIMULATION" ||
		len(captured.Snapshot.Participants) != 2 {
		t.Fatalf("single-role FULL snapshot = %+v", captured.Snapshot)
	}
}

func TestTargetedPlanRevisionAndStartUseFrozenPlan(t *testing.T) {
	t.Parallel()

	plan := targetedPlanFixture()
	var revisedCommand persistence.UpdatePlanCommand
	repository := &contextRepositoryStub{
		getPlan: func(string) (persistence.Plan, error) {
			return plan, nil
		},
		updatePlan: func(
			command persistence.UpdatePlanCommand,
		) (persistence.Plan, bool, error) {
			revisedCommand = command
			revised := plan
			revised.Revision++
			revised.SelectedRoleIDs = append(
				[]string(nil),
				command.SelectedRoleIDs...,
			)
			catalog := command.CatalogSnapshot
			policy := command.SessionPolicy
			revised.CatalogSnapshot = &catalog
			revised.SessionPolicy = &policy
			revised.PracticeFocuses = append(
				[]persistence.PracticeObjective(nil),
				command.PracticeFocuses...,
			)
			return revised, false, nil
		},
	}
	application, err := NewContextApplication(
		repository,
		panicIDGenerator{},
		panicContextDependency{},
		panicContextDependency{},
		targetedCatalogReader{selection: targetedCatalogFixture()},
	)
	if err != nil {
		t.Fatalf("NewContextApplication: %v", err)
	}
	revised, replayed, err := application.UpdatePlan(
		context.Background(),
		contextActorFixture(),
		plan.ID,
		"targeted-revision-key",
		UpdatePlanRequest{
			ExpectedPlanRevision: 1,
			SelectedRoleIDs:      append([]string(nil), plan.SelectedRoleIDs...),
			PracticeOptionID: plan.CatalogSnapshot.
				PracticeOption.ID,
			PracticeOptionVersion: plan.CatalogSnapshot.
				PracticeOption.Version,
			MaxEffectiveTurns: 2,
		},
	)
	if err != nil || replayed || revised.Revision != 2 ||
		revisedCommand.SessionPolicy.MaxEffectiveTurns != 2 ||
		!reflect.DeepEqual(
			revised.PreparationSnapshot,
			plan.PreparationSnapshot,
		) {
		t.Fatalf("UpdatePlan = (%+v, %t, %v)", revised, replayed, err)
	}

	plan = revised
	var startCommand persistence.CreateContextSessionCommand
	repository.createSession = func(
		command persistence.CreateContextSessionCommand,
	) (persistence.ContextSessionBootstrap, bool, error) {
		startCommand = command
		return persistence.ContextSessionBootstrap{
			Session: persistence.ContextSession{
				ID: command.SessionID,
			},
			Snapshot: command.Snapshot,
		}, false, nil
	}
	startApplication, err := NewContextApplication(
		repository,
		&targetedContextIDs{values: []string{
			"session-targeted",
			"snapshot-targeted",
			"participant-interviewer",
			"participant-candidate",
		}},
		panicContextDependency{},
		panicContextDependency{},
		panicContextDependency{},
	)
	if err != nil {
		t.Fatalf("NewContextApplication for start: %v", err)
	}
	bootstrap, replayed, err := startApplication.CreateSession(
		context.Background(),
		contextActorFixture(),
		plan.ID,
		"targeted-start-key",
		CreateSessionRequest{ExpectedPlanRevision: plan.Revision},
	)
	if err != nil || replayed ||
		bootstrap.Session.ID != "session-targeted" ||
		!reflect.DeepEqual(
			startCommand.Snapshot.Preparation,
			*plan.PreparationSnapshot,
		) ||
		!reflect.DeepEqual(
			startCommand.Snapshot.SessionPolicy,
			*plan.SessionPolicy,
		) ||
		startCommand.Snapshot.PlanRevision != plan.Revision {
		t.Fatalf("CreateSession = (%+v, %t, %v)", bootstrap, replayed, err)
	}
}

func targetedPreparationFixture() persistence.PreparationSnapshot {
	input := persistence.JobTargetInputSnapshot{
		Source:              "quick_start",
		JobTitle:            "Backend engineer",
		Seniority:           "Senior",
		CandidateBackground: "Built reliable Go services.",
	}
	candidate := persistence.JobTargetCandidateSnapshot{
		Source:             "quick_start",
		GeneralAdviceOnly:  true,
		JobTitle:           "Backend engineer",
		Seniority:          "Senior",
		Responsibilities:   []string{"Build reliable APIs."},
		CoreSkills:         []string{"Go services"},
		CommunicationFocus: []string{"Explain trade-offs."},
		PracticeGoals:      []string{"Practice a technical deep dive."},
		ScopeNotice:        "Limited to the interview content pack.",
		CatalogRecommendation: persistence.
			JobTargetCatalogRecommendationSnapshot{
			ScenarioDefinitionID:      "scenario-1",
			ScenarioDefinitionVersion: 1,
			SelectedRoleIDs:           []string{"role-1"},
			PracticeOptionID:          "option-focus-role-1",
			PracticeOptionVersion:     1,
		},
	}
	return persistence.PreparationSnapshot{
		ID:                                 "preparation-snapshot-1",
		SourceProfileID:                    "profile-1",
		SourceVersion:                      1,
		SourceJobTargetID:                  "job-target-1",
		SourceJobTargetConfirmationVersion: 1,
		JobTargetInputSnapshot:             &input,
		JobTargetCandidateSnapshot:         &candidate,
		BackgroundSnapshot:                 "Built reliable Go services.",
		CreatedAt: time.Date(
			2026,
			time.July,
			26,
			10,
			0,
			0,
			0,
			time.UTC,
		),
	}
}

func targetedCatalogFixture() PlanCatalogSelection {
	return PlanCatalogSelection{
		ScenarioDefinition: persistence.ScenarioDefinitionSnapshot{
			ID:      "scenario-1",
			Type:    "INTERVIEW",
			Name:    "Technical interview",
			Version: 1,
			Status:  "active",
		},
		ScenarioConfig: persistence.ScenarioConfigSnapshot{
			ID:                   "config-1",
			ScenarioDefinitionID: "scenario-1",
			Type:                 "INTERVIEW",
			Version:              1,
			JobTitle:             "Backend engineer",
			JobDescription:       "Build reliable APIs.",
			FocusAreas:           []string{"system_design"},
		},
		SelectedRoles: []persistence.RoleSnapshot{{
			ID:                   "role-1",
			ScenarioDefinitionID: "scenario-1",
			Type:                 "TECHNICAL_INTERVIEWER",
			DisplayName:          "Technical interviewer",
			Responsibilities:     "Probe technical depth.",
			Style:                "Precise.",
			FocusAreas:           []string{"system_design"},
			Version:              1,
		}},
		PracticeOption: persistence.PracticeOptionSnapshot{
			ID:                   "option-focus-role-1",
			ScenarioDefinitionID: "scenario-1",
			RoleDefinitionID:     "role-1",
			Type:                 "FOCUS",
			DisplayName:          "Technical focus",
			Version:              1,
		},
	}
}

func targetedMultiRoleFullCatalogFixture() PlanCatalogSelection {
	result := targetedCatalogFixture()
	result.SelectedRoles = append(result.SelectedRoles, persistence.RoleSnapshot{
		ID:                   "role-2",
		ScenarioDefinitionID: result.ScenarioDefinition.ID,
		Type:                 "HR_INTERVIEWER",
		DisplayName:          "Recruiter interviewer",
		Responsibilities:     "Probe motivation and communication.",
		Style:                "Warm and structured.",
		FocusAreas:           []string{"communication"},
		Version:              1,
	})
	result.PracticeOption = persistence.PracticeOptionSnapshot{
		ID:                   "option-full",
		ScenarioDefinitionID: result.ScenarioDefinition.ID,
		Type:                 "FULL_SIMULATION",
		DisplayName:          "Full simulation",
		Version:              1,
	}
	return result
}

func targetedPlanFixture() persistence.Plan {
	preparation := targetedPreparationFixture()
	selection := targetedCatalogFixture()
	catalog := planCatalogSnapshot(selection)
	policy := defaultContextSessionPolicy(
		selection.ScenarioConfig,
		selection.PracticeOption,
	)
	return persistence.Plan{
		ID:                        "plan-targeted",
		UserID:                    contextActorFixture().UserID,
		AgentThreadID:             "thread-1",
		MatterID:                  "matter-1",
		ScenarioDefinitionID:      selection.ScenarioDefinition.ID,
		ScenarioDefinitionVersion: selection.ScenarioDefinition.Version,
		ScenarioType:              selection.ScenarioDefinition.Type,
		ScenarioConfigID:          selection.ScenarioConfig.ID,
		ScenarioConfigVersion:     selection.ScenarioConfig.Version,
		PreparationProfileID:      preparation.SourceProfileID,
		SelectedRoleIDs:           roleSnapshotIDs(selection.SelectedRoles),
		PreparationSnapshot:       &preparation,
		CatalogSnapshot:           &catalog,
		SessionPolicy:             &policy,
		PracticeFocuses: contextPracticeFocuses(
			selection.SelectedRoles,
		),
		Revision: 1,
		Status:   persistence.PlanStatusReady,
	}
}

func targetedPlanFromCommand(
	userID string,
	command persistence.CreatePlanCommand,
) persistence.Plan {
	return persistence.Plan{
		ID:                        command.PlanID,
		UserID:                    userID,
		AgentThreadID:             command.AgentThreadID,
		MatterID:                  command.MatterID,
		ScenarioDefinitionID:      command.ScenarioDefinitionID,
		ScenarioDefinitionVersion: command.ScenarioDefinitionVersion,
		ScenarioType:              command.ScenarioType,
		ScenarioConfigID:          command.ScenarioConfigID,
		ScenarioConfigVersion:     command.ScenarioConfigVersion,
		PreparationProfileID:      command.PreparationProfileID,
		SelectedRoleIDs:           append([]string(nil), command.SelectedRoleIDs...),
		PreparationSnapshot:       command.PreparationSnapshot,
		CatalogSnapshot:           command.CatalogSnapshot,
		SessionPolicy:             command.SessionPolicy,
		PracticeFocuses:           append([]persistence.PracticeObjective(nil), command.PracticeFocuses...),
		Revision:                  1,
		Status:                    persistence.PlanStatusReady,
	}
}

type targetedContextIDs struct {
	values []string
	index  int
}

func (g *targetedContextIDs) NewID() (string, error) {
	if g == nil || g.index >= len(g.values) {
		return "", errors.New("no targeted Context ID")
	}
	value := g.values[g.index]
	g.index++
	return value, nil
}

type targetedAnchorReader struct{}

func (targetedAnchorReader) ValidatePracticeAnchor(
	_ context.Context,
	_ requestcontext.Actor,
	threadID string,
	matterID string,
) (PracticeAnchor, error) {
	return PracticeAnchor{ThreadID: threadID, MatterID: matterID}, nil
}

type targetedPreparationReader struct {
	snapshot persistence.PreparationSnapshot
}

func (targetedPreparationReader) ReadPreparationProfile(
	context.Context,
	requestcontext.Actor,
	string,
) (PreparationProfileRef, error) {
	return PreparationProfileRef{}, errors.New("unexpected Profile read")
}

func (r targetedPreparationReader) ReadPreparationSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (persistence.PreparationSnapshot, error) {
	return r.snapshot, nil
}

type targetedCatalogReader struct {
	selection PlanCatalogSelection
}

func (r targetedCatalogReader) ReadPlanCatalog(
	request PlanCatalogRequest,
) (PlanCatalogSelection, error) {
	if request.ScenarioDefinitionID != r.selection.ScenarioDefinition.ID ||
		request.ScenarioDefinitionVersion !=
			r.selection.ScenarioDefinition.Version ||
		!reflect.DeepEqual(
			request.SelectedRoleIDs,
			roleSnapshotIDs(r.selection.SelectedRoles),
		) ||
		request.PracticeOptionID != r.selection.PracticeOption.ID ||
		request.PracticeOptionVersion != r.selection.PracticeOption.Version {
		return PlanCatalogSelection{}, persistence.ErrNotFound
	}
	return r.selection, nil
}

func (targetedCatalogReader) ReadSessionCatalog(
	SessionCatalogRequest,
) (SessionCatalogSelection, error) {
	return SessionCatalogSelection{}, errors.New("unexpected Session Catalog read")
}
