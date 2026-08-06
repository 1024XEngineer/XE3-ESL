package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPlanServiceCreateFreezesCompletePlan(t *testing.T) {
	t.Parallel()

	var created CreatePlanCommand
	repository := &planRepositoryStub{
		create: func(
			actor requestcontext.Actor,
			command CreatePlanCommand,
		) (PracticePlan, bool, error) {
			created = command
			return planFromCreateCommand(actor, command), false, nil
		},
	}
	service := newPlanTestService(t, repository, planServiceDependencies{
		goal: goal.Goal{
			ID:      "goal-1",
			Title:   "Prepare for an interview",
			Version: 3,
		},
		thread: SourceThread{ID: "thread-1"},
	})
	request := validPlanCreateRequest()
	request.SourceThreadID = "thread-1"
	request.GoalID = "goal-1"
	request.MaxEffectiveTurns = 5

	plan, replayed, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-0001",
		request,
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if replayed {
		t.Fatal("CreatePlan replayed = true, want false")
	}
	if created.PlanID != "plan-1" ||
		created.SourceThreadID != "thread-1" ||
		created.GoalSnapshot == nil ||
		created.GoalSnapshot.ID != "goal-1" ||
		created.GoalSnapshot.Title != "Prepare for an interview" ||
		created.GoalSnapshot.Version != 3 {
		t.Fatalf("CreatePlan command anchor snapshots = %#v", created)
	}
	if created.PreparationSnapshot.ID != "snapshot-1" ||
		created.SceneSelection.Scene.ID != "scene-1" ||
		created.SceneSelection.Scene.Version != 2 ||
		created.SceneSelection.PracticeOptionID != "option-full" {
		t.Fatalf("CreatePlan command frozen inputs = %#v", created)
	}
	if created.SessionPolicy.MinEffectiveTurns != 4 ||
		created.SessionPolicy.MaxEffectiveTurns != 5 ||
		created.SessionPolicy.CoverageCheckpointTurn != 4 {
		t.Fatalf("CreatePlan policy = %#v", created.SessionPolicy)
	}
	if len(created.PracticeObjectives) != 2 ||
		created.PracticeObjectives[0].ID != "clarity" ||
		created.PracticeObjectives[0].Description != "Explain the answer clearly." ||
		created.PracticeObjectives[1].ID != "evidence" ||
		created.PracticeObjectives[1].Description != "Support claims with concrete evidence." {
		t.Fatalf("CreatePlan objectives = %#v", created.PracticeObjectives)
	}
	if plan.Revision != 1 || plan.Status != PlanStatusReady ||
		plan.UserID != planActor().UserID {
		t.Fatalf("CreatePlan result = %#v", plan)
	}
}

func TestPlanServiceCreateWithoutOptionalGoalOrThread(t *testing.T) {
	t.Parallel()

	goalReads := 0
	threadReads := 0
	repository := &planRepositoryStub{
		create: func(
			actor requestcontext.Actor,
			command CreatePlanCommand,
		) (PracticePlan, bool, error) {
			if command.GoalSnapshot != nil || command.SourceThreadID != "" {
				t.Fatalf("optional values persisted = %#v", command)
			}
			return planFromCreateCommand(actor, command), false, nil
		},
	}
	service := newPlanTestService(t, repository, planServiceDependencies{
		goalRead: func(string) (goal.Goal, error) {
			goalReads++
			return goal.Goal{}, errors.New("unexpected Goal read")
		},
		threadRead: func(string) (SourceThread, error) {
			threadReads++
			return SourceThread{}, errors.New("unexpected Thread read")
		},
	})

	plan, _, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-0002",
		validPlanCreateRequest(),
	)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if goalReads != 0 || threadReads != 0 ||
		plan.GoalSnapshot != nil || plan.SourceThreadID != "" {
		t.Fatalf(
			"optional reads/result = (%d, %d, %#v)",
			goalReads,
			threadReads,
			plan,
		)
	}
}

func TestPlanServiceCreateResolvesSceneForActor(t *testing.T) {
	t.Parallel()

	var resolvedOwnerUserID string
	repository := &planRepositoryStub{create: func(
		actor requestcontext.Actor,
		command CreatePlanCommand,
	) (PracticePlan, bool, error) {
		return planFromCreateCommand(actor, command), false, nil
	}}
	service := newPlanTestService(t, repository, planServiceDependencies{
		resolveAccessibleSelection: func(
			ownerUserID string,
			_ string,
			_ int,
			_ []string,
			_ string,
		) (scene.SelectionSnapshot, error) {
			resolvedOwnerUserID = ownerUserID
			return planSelectionFixture(), nil
		},
	})
	actor := planActor()
	if _, _, err := service.CreatePlan(
		context.Background(),
		actor,
		"plan-create-owner-scene",
		validPlanCreateRequest(),
	); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if resolvedOwnerUserID != actor.UserID {
		t.Fatalf("resolved owner user ID = %q", resolvedOwnerUserID)
	}
}

func TestPlanServiceCreateReplaysBeforeSourceValidation(t *testing.T) {
	t.Parallel()

	want := completePlanFixture()
	repository := &planRepositoryStub{
		replay: func(IdempotencyIntent) (PracticePlan, bool, error) {
			return want, true, nil
		},
	}
	service := newPlanTestService(t, repository, planServiceDependencies{
		snapshotRead: func(string) (Snapshot, error) {
			t.Fatal("replayed CreatePlan read a mutable source")
			return Snapshot{}, nil
		},
	})

	got, replayed, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-0003",
		validPlanCreateRequest(),
	)
	if err != nil {
		t.Fatalf("CreatePlan replay: %v", err)
	}
	if !replayed || got.ID != want.ID {
		t.Fatalf("CreatePlan replay = (%#v, %t)", got, replayed)
	}
}

func TestPlanServiceCreateRejectsInvalidSelection(t *testing.T) {
	t.Parallel()

	service := newPlanTestService(t, &planRepositoryStub{}, planServiceDependencies{
		resolveSelection: func(
			string,
			int,
			[]string,
			string,
		) (scene.SelectionSnapshot, error) {
			return scene.SelectionSnapshot{}, scene.ErrCatalogSelectionInvalid
		},
	})
	_, _, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-0004",
		validPlanCreateRequest(),
	)
	if !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("CreatePlan invalid selection error = %v", err)
	}
}

func TestPlanServiceIELTSCreateRequiresExplicitSelection(t *testing.T) {
	t.Parallel()

	service := newPlanTestService(t, &planRepositoryStub{}, planServiceDependencies{
		resolveSelection: func(
			string,
			int,
			[]string,
			string,
		) (scene.SelectionSnapshot, error) {
			return planIELTSSelectionFixture(), nil
		},
	})
	_, _, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-ielts-missing",
		validPlanCreateRequest(),
	)
	if !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("CreatePlan missing IELTS selection error = %v", err)
	}
}

func TestPlanServiceIELTSCreateResolvesAndFreezesQuestionAssignment(
	t *testing.T,
) {
	t.Parallel()

	var created CreatePlanCommand
	repository := &planRepositoryStub{create: func(
		actor requestcontext.Actor,
		command CreatePlanCommand,
	) (PracticePlan, bool, error) {
		created = command
		return planFromCreateCommand(actor, command), false, nil
	}}
	service := newPlanTestService(t, repository, planServiceDependencies{
		resolveSelection: func(
			string,
			int,
			[]string,
			string,
		) (scene.SelectionSnapshot, error) {
			return planIELTSSelectionFixture(), nil
		},
		resolveIELTS: func(
			selection ielts.QuestionSetSelection,
		) (ielts.ResolvedQuestionSet, error) {
			if selection.Mode != ielts.PracticeModePart2 ||
				selection.Part1SetID != "" ||
				selection.TopicGroupID != "topic-1" {
				t.Fatalf("IELTS selection = %#v", selection)
			}
			return planIELTSResolvedFixture(), nil
		},
	})
	request := validPlanCreateRequest()
	request.IELTSSelection = &IELTSQuestionSelection{
		TopicGroupID: "topic-1",
	}
	plan, _, err := service.CreatePlan(
		context.Background(),
		planActor(),
		"plan-create-ielts",
		request,
	)
	if err != nil {
		t.Fatalf("CreatePlan IELTS: %v", err)
	}
	if created.IELTSAssignment == nil || plan.IELTSAssignment == nil ||
		created.IELTSAssignment.Mode != scene.PracticeModePart2 ||
		len(created.IELTSAssignment.Parts) != 2 ||
		created.IELTSAssignment.Parts[0].Part != scene.PracticeModePart2 ||
		created.IELTSAssignment.Parts[0].SourceID != "topic-1" ||
		created.IELTSAssignment.Parts[1].Part != scene.PracticeModePart3 ||
		created.IELTSAssignment.Parts[1].SourceID != "topic-1" ||
		created.SceneSelection.Scene.Prompt.PublicSceneBrief !=
			"完成“科技与学习”题卡，并可继续同主题 Part 3。" ||
		!equalPlanStrings(
			created.SceneSelection.Scene.Prompt.TurnBlueprints,
			ieltsAssignmentTurnBlueprints(*created.IELTSAssignment),
		) {
		t.Fatalf("frozen IELTS Plan = %#v", created)
	}
}

func TestPlanServiceIELTSReviseRefreezesExplicitAssignment(t *testing.T) {
	t.Parallel()

	current := completeIELTSPlanFixture()
	var revised RevisePlanCommand
	repository := &planRepositoryStub{
		readCurrent: func(string) (PracticePlan, error) { return current, nil },
		revise: func(
			actor requestcontext.Actor,
			command RevisePlanCommand,
		) (PracticePlan, bool, error) {
			revised = command
			result := current
			result.SceneSelection = command.SceneSelection
			result.SessionPolicy = command.SessionPolicy
			result.PracticeObjectives = command.PracticeObjectives
			result.IELTSAssignment = command.IELTSAssignment
			result.Revision++
			result.UpdatedAt = result.UpdatedAt.Add(time.Minute)
			return result, false, nil
		},
	}
	catalogReads := 0
	service := newPlanTestService(t, repository, planServiceDependencies{
		resolveIELTS: func(
			selection ielts.QuestionSetSelection,
		) (ielts.ResolvedQuestionSet, error) {
			catalogReads++
			if selection.Mode != ielts.PracticeModePart2 ||
				selection.TopicGroupID != "topic-1" {
				t.Fatalf("IELTS revision selection = %#v", selection)
			}
			return planIELTSResolvedFixture(), nil
		},
	})
	_, _, err := service.RevisePlan(
		context.Background(),
		planActor(),
		current.ID,
		"plan-revise-ielts",
		RevisePlanRequest{
			ExpectedPlanRevision: current.Revision,
			SelectedRoleIDs:      []string{"role-1"},
			PracticeOptionID:     "option-full",
			MaxEffectiveTurns:    5,
			IELTSSelection: &IELTSQuestionSelection{
				TopicGroupID: "topic-1",
			},
		},
	)
	if err != nil {
		t.Fatalf("RevisePlan IELTS: %v", err)
	}
	if catalogReads != 1 || revised.IELTSAssignment == nil ||
		!equalPlanStrings(
			ieltsAssignmentTurnBlueprints(*revised.IELTSAssignment),
			ieltsAssignmentTurnBlueprints(*current.IELTSAssignment),
		) {
		t.Fatalf("revised IELTS assignment = %#v, reads=%d", revised, catalogReads)
	}
}

func TestReturnedIELTSPlanAcceptsItsFrozenBriefWithoutRecomputingCopy(
	t *testing.T,
) {
	plan := completeIELTSPlanFixture()
	plan.SceneSelection.Scene.Prompt.PublicSceneBrief =
		"This explanation was frozen with the Plan revision."
	if !validReturnedPlan(plan, planActor(), plan.ID) {
		t.Fatalf("validReturnedPlan rejected frozen brief: %#v", plan)
	}
}

func TestPracticeObjectivesRejectsConflictingDescriptions(t *testing.T) {
	roles := []scene.RoleDefinition{
		{PracticeObjectives: []scene.PracticeObjectiveDefinition{{
			ID: "evidence", Description: "Support claims with concrete evidence.",
		}}},
		{PracticeObjectives: []scene.PracticeObjectiveDefinition{{
			ID: "evidence", Description: "Use evidence persuasively.",
		}}},
	}

	if _, err := practiceObjectives(roles); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("practiceObjectives() error = %v", err)
	}
}

func TestPracticeObjectivesDeduplicatesIdenticalDefinitionsInFirstOrder(
	t *testing.T,
) {
	roles := []scene.RoleDefinition{
		{PracticeObjectives: []scene.PracticeObjectiveDefinition{
			{ID: "clarity", Description: "Explain the answer clearly."},
			{ID: "evidence", Description: "Support claims with concrete evidence."},
		}},
		{PracticeObjectives: []scene.PracticeObjectiveDefinition{
			{ID: "evidence", Description: "Support claims with concrete evidence."},
			{ID: "reflection", Description: "Explain what would change next time."},
		}},
	}

	objectives, err := practiceObjectives(roles)
	if err != nil {
		t.Fatalf("practiceObjectives() error = %v", err)
	}
	want := []string{"clarity", "evidence", "reflection"}
	if len(objectives) != len(want) {
		t.Fatalf("practiceObjectives() = %#v", objectives)
	}
	for index, objectiveID := range want {
		if objectives[index].ID != objectiveID {
			t.Fatalf("practiceObjectives() = %#v", objectives)
		}
	}
}

func TestPlanServiceReviseUsesFrozenSceneAndAppendsRevision(t *testing.T) {
	t.Parallel()

	current := completePlanFixture()
	var revised RevisePlanCommand
	repository := &planRepositoryStub{
		readCurrent: func(string) (PracticePlan, error) {
			return current, nil
		},
		revise: func(
			actor requestcontext.Actor,
			command RevisePlanCommand,
		) (PracticePlan, bool, error) {
			revised = command
			result := current
			result.SceneSelection = command.SceneSelection
			result.SessionPolicy = command.SessionPolicy
			result.PracticeObjectives = command.PracticeObjectives
			result.Revision++
			result.UpdatedAt = result.UpdatedAt.Add(time.Minute)
			return result, false, nil
		},
	}
	catalogReads := 0
	service := newPlanTestService(t, repository, planServiceDependencies{
		resolveSelection: func(
			string,
			int,
			[]string,
			string,
		) (scene.SelectionSnapshot, error) {
			catalogReads++
			return scene.SelectionSnapshot{}, errors.New("unexpected live Catalog read")
		},
	})

	plan, replayed, err := service.RevisePlan(
		context.Background(),
		planActor(),
		current.ID,
		"plan-revise-0001",
		RevisePlanRequest{
			ExpectedPlanRevision: 1,
			SelectedRoleIDs:      []string{"role-1"},
			PracticeOptionID:     "option-focus",
			MaxEffectiveTurns:    2,
		},
	)
	if err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	if replayed || catalogReads != 0 || plan.Revision != 2 {
		t.Fatalf(
			"RevisePlan = (replayed=%t, catalog=%d, revision=%d)",
			replayed,
			catalogReads,
			plan.Revision,
		)
	}
	if revised.ExpectedPlanRevision != 1 ||
		revised.SceneSelection.Scene.ID != current.SceneSelection.Scene.ID ||
		revised.SceneSelection.Scene.Version != current.SceneSelection.Scene.Version ||
		revised.SceneSelection.PracticeOptionID != "option-focus" ||
		revised.SessionPolicy.MaxEffectiveTurns != 2 {
		t.Fatalf("RevisePlan command = %#v", revised)
	}
}

func TestPlanServiceReviseRejectsStaleRevision(t *testing.T) {
	t.Parallel()

	current := completePlanFixture()
	current.Revision = 2
	service := newPlanTestService(t, &planRepositoryStub{
		readCurrent: func(string) (PracticePlan, error) {
			return current, nil
		},
	}, planServiceDependencies{})
	_, _, err := service.RevisePlan(
		context.Background(),
		planActor(),
		current.ID,
		"plan-revise-0002",
		RevisePlanRequest{
			ExpectedPlanRevision: 1,
			SelectedRoleIDs:      []string{"role-1"},
			PracticeOptionID:     "option-focus",
			MaxEffectiveTurns:    2,
		},
	)
	if !errors.Is(err, ErrPlanConflict) {
		t.Fatalf("RevisePlan stale revision error = %v", err)
	}
}

func TestPlanServiceReadExecutablePlanRequiresExactCurrentReadyRevision(
	t *testing.T,
) {
	t.Parallel()

	want := completePlanFixture()
	var readRevision int
	repository := &planRepositoryStub{
		readExecutable: func(
			planID string,
			revision int,
		) (PracticePlan, error) {
			readRevision = revision
			if planID != want.ID || revision != want.Revision {
				return PracticePlan{}, ErrPlanConflict
			}
			return want, nil
		},
	}
	service := newPlanTestService(t, repository, planServiceDependencies{})
	got, err := service.ReadExecutablePlan(
		context.Background(),
		planActor(),
		want.ID,
		want.Revision,
	)
	if err != nil {
		t.Fatalf("ReadExecutablePlan: %v", err)
	}
	if readRevision != want.Revision || got.ID != want.ID {
		t.Fatalf("ReadExecutablePlan = (%#v, revision=%d)", got, readRevision)
	}

	_, err = service.ReadExecutablePlan(
		context.Background(),
		planActor(),
		want.ID,
		want.Revision+1,
	)
	if !errors.Is(err, ErrPlanConflict) {
		t.Fatalf("ReadExecutablePlan stale error = %v", err)
	}
}

type planRepositoryStub struct {
	replay         func(IdempotencyIntent) (PracticePlan, bool, error)
	create         func(requestcontext.Actor, CreatePlanCommand) (PracticePlan, bool, error)
	readCurrent    func(string) (PracticePlan, error)
	revise         func(requestcontext.Actor, RevisePlanCommand) (PracticePlan, bool, error)
	readExecutable func(string, int) (PracticePlan, error)
}

func (s *planRepositoryStub) ReplayPlan(
	_ context.Context,
	_ requestcontext.Actor,
	intent IdempotencyIntent,
) (PracticePlan, bool, error) {
	if s.replay == nil {
		return PracticePlan{}, false, nil
	}
	return s.replay(intent)
}

func (s *planRepositoryStub) CreatePlan(
	_ context.Context,
	actor requestcontext.Actor,
	command CreatePlanCommand,
) (PracticePlan, bool, error) {
	if s.create == nil {
		return PracticePlan{}, false, errors.New("unexpected CreatePlan")
	}
	return s.create(actor, command)
}

func (s *planRepositoryStub) ReadCurrentPlan(
	_ context.Context,
	_ requestcontext.Actor,
	planID string,
) (PracticePlan, error) {
	if s.readCurrent == nil {
		return PracticePlan{}, errors.New("unexpected ReadCurrentPlan")
	}
	return s.readCurrent(planID)
}

func (s *planRepositoryStub) RevisePlan(
	_ context.Context,
	actor requestcontext.Actor,
	command RevisePlanCommand,
) (PracticePlan, bool, error) {
	if s.revise == nil {
		return PracticePlan{}, false, errors.New("unexpected RevisePlan")
	}
	return s.revise(actor, command)
}

func (s *planRepositoryStub) ReadExecutablePlan(
	_ context.Context,
	_ requestcontext.Actor,
	planID string,
	revision int,
) (PracticePlan, error) {
	if s.readExecutable == nil {
		return PracticePlan{}, errors.New("unexpected ReadExecutablePlan")
	}
	return s.readExecutable(planID, revision)
}

type planIDGeneratorStub struct{}

func (planIDGeneratorStub) NewID() (string, error) { return "plan-1", nil }

type planProfileReaderStub struct {
	read func(string) (Snapshot, error)
}

func (planProfileReaderStub) ReadProfile(
	context.Context,
	requestcontext.Actor,
	string,
) (Profile, error) {
	return Profile{}, errors.New("unexpected ReadProfile")
}

func (s planProfileReaderStub) ReadSnapshot(
	_ context.Context,
	_ requestcontext.Actor,
	id string,
) (Snapshot, error) {
	return s.read(id)
}

type planGoalReaderStub struct {
	read func(string) (goal.Goal, error)
}

func (s planGoalReaderStub) ReadOwned(
	_ context.Context,
	_ requestcontext.Actor,
	id string,
) (goal.Goal, error) {
	return s.read(id)
}

type planThreadReaderStub struct {
	read func(string) (SourceThread, error)
}

func (s planThreadReaderStub) ReadOwnedThread(
	_ context.Context,
	_ requestcontext.Actor,
	id string,
) (SourceThread, error) {
	return s.read(id)
}

type planCatalogStub struct {
	resolve           func(string, int, []string, string) (scene.SelectionSnapshot, error)
	resolveAccessible func(string, string, int, []string, string) (scene.SelectionSnapshot, error)
}

func (planCatalogStub) ListActiveScenes(
	context.Context,
) ([]scene.SceneDefinition, error) {
	return nil, errors.New("unexpected ListActiveScenes")
}

func (planCatalogStub) GetScene(
	context.Context,
	string,
) (scene.SceneDefinition, error) {
	return scene.SceneDefinition{}, errors.New("unexpected GetScene")
}

func (planCatalogStub) ListRoles(
	context.Context,
	string,
) ([]scene.RoleDefinition, error) {
	return nil, errors.New("unexpected ListRoles")
}

func (s planCatalogStub) ResolveSelection(
	_ context.Context,
	sceneID string,
	sceneVersion int,
	roleIDs []string,
	optionID string,
) (scene.SelectionSnapshot, error) {
	return s.resolve(sceneID, sceneVersion, roleIDs, optionID)
}

func (s planCatalogStub) ResolveAccessibleSelection(
	_ context.Context,
	ownerUserID string,
	sceneID string,
	sceneVersion int,
	roleIDs []string,
	optionID string,
) (scene.SelectionSnapshot, error) {
	if s.resolveAccessible != nil {
		return s.resolveAccessible(
			ownerUserID,
			sceneID,
			sceneVersion,
			roleIDs,
			optionID,
		)
	}
	return s.resolve(sceneID, sceneVersion, roleIDs, optionID)
}

type planIELTSResolverStub struct {
	resolve func(ielts.QuestionSetSelection) (ielts.ResolvedQuestionSet, error)
}

func (s planIELTSResolverStub) ResolveQuestionSet(
	selection ielts.QuestionSetSelection,
) (ielts.ResolvedQuestionSet, error) {
	if s.resolve == nil {
		return ielts.ResolvedQuestionSet{}, errors.New(
			"unexpected IELTS question-set read",
		)
	}
	return s.resolve(selection)
}

type planServiceDependencies struct {
	snapshotRead               func(string) (Snapshot, error)
	goal                       goal.Goal
	goalRead                   func(string) (goal.Goal, error)
	thread                     SourceThread
	threadRead                 func(string) (SourceThread, error)
	resolveSelection           func(string, int, []string, string) (scene.SelectionSnapshot, error)
	resolveAccessibleSelection func(
		string,
		string,
		int,
		[]string,
		string,
	) (scene.SelectionSnapshot, error)
	resolveIELTS func(ielts.QuestionSetSelection) (ielts.ResolvedQuestionSet, error)
}

func newPlanTestService(
	t *testing.T,
	repository PlanRepository,
	dependencies planServiceDependencies,
) *PlanService {
	t.Helper()
	if dependencies.snapshotRead == nil {
		dependencies.snapshotRead = func(string) (Snapshot, error) {
			return planSnapshotFixture(), nil
		}
	}
	if dependencies.goalRead == nil {
		dependencies.goalRead = func(string) (goal.Goal, error) {
			return dependencies.goal, nil
		}
	}
	if dependencies.threadRead == nil {
		dependencies.threadRead = func(string) (SourceThread, error) {
			return dependencies.thread, nil
		}
	}
	if dependencies.resolveSelection == nil {
		dependencies.resolveSelection = func(
			string,
			int,
			[]string,
			string,
		) (scene.SelectionSnapshot, error) {
			return planSelectionFixture(), nil
		}
	}
	service, err := NewPlanService(
		repository,
		planIDGeneratorStub{},
		planProfileReaderStub{read: dependencies.snapshotRead},
		planGoalReaderStub{read: dependencies.goalRead},
		planThreadReaderStub{read: dependencies.threadRead},
		planCatalogStub{
			resolve:           dependencies.resolveSelection,
			resolveAccessible: dependencies.resolveAccessibleSelection,
		},
		planIELTSResolverStub{resolve: dependencies.resolveIELTS},
		planPolicyResolverStub{},
	)
	if err != nil {
		t.Fatalf("NewPlanService: %v", err)
	}
	return service
}

func validPlanCreateRequest() CreatePlanRequest {
	return CreatePlanRequest{
		PreparationSnapshotID: "snapshot-1",
		SceneID:               "scene-1",
		SceneVersion:          2,
		SelectedRoleIDs:       []string{"role-1"},
		PracticeOptionID:      "option-full",
	}
}

func planActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "auth-session-1"}
}

func planSnapshotFixture() Snapshot {
	return Snapshot{
		ID:              "snapshot-1",
		SourceProfileID: "profile-1",
		SourceVersion:   2,
		ResumeSnapshot: &ResumeRevisionSnapshot{
			ResumeID: "50000000-0000-4000-8000-000000000001",
			Revision: 1,
			Material: ResumeMaterial{
				WorkExperiences:      []ResumeWorkExperience{},
				ProjectExperiences:   []ResumeProjectExperience{},
				EducationExperiences: []ResumeEducationExperience{},
				Skills:               []string{"Go"},
				Awards:               []string{},
			},
		},
		BackgroundSnapshot: "backend engineer",
		CreatedAt:          time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC),
	}
}

func planSelectionFixture() scene.SelectionSnapshot {
	definition := scene.SceneDefinition{
		ID:         "scene-1",
		Experience: scene.PracticeExperienceInterview,
		Category:   scene.SceneCategoryInterviewProfessional,
		Name:       "Interview",
		Version:    2,
		Status:     scene.SceneStatusActive,
		Prompt: scene.ScenePrompt{
			PublicSceneBrief: "Interview practice",
			PracticeGoal:     "Give clear answers",
			UserRole:         "Candidate",
			AIRole:           "Interviewer",
			PersonaSummary:   "A structured interviewer.",
			FocusAreas:       []string{"clarity", "evidence"},
			TurnBlueprints:   []string{"one", "two", "three", "four"},
		},
		Roles: []scene.RoleDefinition{
			{
				ID:          "role-1",
				SceneID:     "scene-1",
				Type:        "INTERVIEWER",
				DisplayName: "Interviewer",
				PracticeObjectives: []scene.PracticeObjectiveDefinition{
					{ID: "clarity", Description: "Explain the answer clearly."},
					{ID: "evidence", Description: "Support claims with concrete evidence."},
				},
			},
		},
		PracticeOptions: []scene.PracticeOption{
			{
				ID:                       "option-full",
				SceneID:                  "scene-1",
				Mode:                     scene.PracticeModeFullSimulation,
				SuggestedDurationSeconds: 600,
				TurnPolicyRef:            "interview.project_deep_dive.turn.v1",
				SessionPolicyRef:         "generic.practice.session.v1",
				EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
			},
			{
				ID:                       "option-focus",
				SceneID:                  "scene-1",
				RoleDefinitionID:         "role-1",
				Mode:                     scene.PracticeModeFocus,
				SuggestedDurationSeconds: 300,
				TurnPolicyRef:            "interview.project_deep_dive.turn.v1",
				SessionPolicyRef:         "generic.practice.session.v1",
				EvaluationPolicyRef:      "interview.shadow.evaluation.v1",
			},
		},
	}
	return scene.SelectionSnapshot{
		Scene:            definition,
		SelectedRoleIDs:  []string{"role-1"},
		PracticeOptionID: "option-full",
	}
}

type planPolicyResolverStub struct{}

func (planPolicyResolverStub) ResolveSessionPolicy(
	definition scene.SceneDefinition,
	option scene.PracticeOption,
	requestedMaxEffectiveTurns int,
) (SessionPolicy, error) {
	policy := SessionPolicy{
		SuggestedDurationSeconds: option.SuggestedDurationSeconds,
		MinEffectiveTurns:        4,
		MaxEffectiveTurns:        6,
		CoverageCheckpointTurn:   4,
		MaxFollowUpsPerQuestion:  1,
		EarlyCompletionRule:      EarlyCompletionCoverageSatisfiedAfterCheckpoint,
	}
	if option.Mode == scene.PracticeModeFocus {
		policy.MinEffectiveTurns = 1
		policy.MaxEffectiveTurns = 3
		policy.CoverageCheckpointTurn = 1
	}
	if requestedMaxEffectiveTurns > 0 {
		if requestedMaxEffectiveTurns < policy.MinEffectiveTurns ||
			requestedMaxEffectiveTurns > policy.MaxEffectiveTurns {
			return SessionPolicy{}, ErrPlanInvalid
		}
		policy.MaxEffectiveTurns = requestedMaxEffectiveTurns
	}
	return policy, nil
}

func completePlanFixture() PracticePlan {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	return PracticePlan{
		ID:                  "plan-1",
		UserID:              "user-1",
		PreparationSnapshot: planSnapshotFixture(),
		SceneSelection:      planSelectionFixture(),
		SessionPolicy: SessionPolicy{
			SuggestedDurationSeconds: 600,
			MinEffectiveTurns:        4,
			MaxEffectiveTurns:        6,
			CoverageCheckpointTurn:   4,
			MaxFollowUpsPerQuestion:  1,
			EarlyCompletionRule:      EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		},
		PracticeObjectives: []PracticeObjective{
			{ID: "clarity", Description: "clarity"},
			{ID: "evidence", Description: "evidence"},
		},
		Revision:  1,
		Status:    PlanStatusReady,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func planIELTSSelectionFixture() scene.SelectionSnapshot {
	selection := planSelectionFixture()
	selection.Scene.Experience = scene.PracticeExperienceIELTSSpeaking
	selection.Scene.Category = scene.SceneCategoryIELTSSpeaking
	selection.Scene.Name = "IELTS Speaking Part 2"
	selection.Scene.PracticeOptions[0].Mode = scene.PracticeModePart2
	selection.Scene.PracticeOptions[0].TurnPolicyRef = "ielts.speaking_part2.turn.v1"
	selection.Scene.PracticeOptions[0].SessionPolicyRef = "ielts.speaking_part2.session.v1"
	selection.Scene.PracticeOptions[0].EvaluationPolicyRef = "ielts.speaking_practice.evaluation.v1"
	return selection
}

func planIELTSResolvedFixture() ielts.ResolvedQuestionSet {
	return ielts.ResolvedQuestionSet{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   ielts.PracticeModePart2,
		Parts: []ielts.ResolvedPart{
			{
				Part:           ielts.PracticeModePart2,
				SourceID:       "topic-1",
				TopicTitle:     "科技与学习",
				CueCard:        "Describe a useful technology.",
				TurnBlueprints: []string{"Part 2 cue card"},
			},
			{
				Part:           ielts.PracticeModePart3,
				SourceID:       "topic-1",
				TopicTitle:     "科技与学习",
				TurnBlueprints: []string{"Part 3 question"},
			},
		},
	}
}

func completeIELTSPlanFixture() PracticePlan {
	plan := completePlanFixture()
	plan.SceneSelection = planIELTSSelectionFixture()
	resolved := planIELTSResolvedFixture()
	plan.IELTSAssignment = &IELTSAssignmentSnapshot{
		BankID: resolved.BankID,
		Season: resolved.Season,
		Mode:   scene.PracticeMode(resolved.Mode),
		Parts: []IELTSAssignmentPartSnapshot{
			{
				Part:           scene.PracticeMode(resolved.Parts[0].Part),
				SourceID:       resolved.Parts[0].SourceID,
				TopicTitle:     resolved.Parts[0].TopicTitle,
				CueCard:        resolved.Parts[0].CueCard,
				TurnBlueprints: clonePlanStrings(resolved.Parts[0].TurnBlueprints),
			},
			{
				Part:           scene.PracticeMode(resolved.Parts[1].Part),
				SourceID:       resolved.Parts[1].SourceID,
				TopicTitle:     resolved.Parts[1].TopicTitle,
				TurnBlueprints: clonePlanStrings(resolved.Parts[1].TurnBlueprints),
			},
		},
	}
	plan.SceneSelection.Scene.Prompt.TurnBlueprints = clonePlanStrings(
		ieltsAssignmentTurnBlueprints(*plan.IELTSAssignment),
	)
	plan.SceneSelection.Scene.Prompt.PublicSceneBrief =
		ieltsAssignmentSceneBrief(*plan.IELTSAssignment)
	return plan
}

func planFromCreateCommand(
	actor requestcontext.Actor,
	command CreatePlanCommand,
) PracticePlan {
	createdAt := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	return PracticePlan{
		ID:                  command.PlanID,
		UserID:              actor.UserID,
		SourceThreadID:      command.SourceThreadID,
		GoalSnapshot:        command.GoalSnapshot,
		PreparationSnapshot: command.PreparationSnapshot,
		SceneSelection:      command.SceneSelection,
		SessionPolicy:       command.SessionPolicy,
		PracticeObjectives:  command.PracticeObjectives,
		IELTSAssignment:     command.IELTSAssignment,
		Revision:            1,
		Status:              PlanStatusReady,
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
}
