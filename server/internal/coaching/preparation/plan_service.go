package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var planObjectiveIDPattern = regexp.MustCompile(
	`^[a-z][a-z0-9_]{0,127}$`,
)

type PlanApplication interface {
	PlanReader
	CreatePlan(
		context.Context,
		requestcontext.Actor,
		string,
		CreatePlanRequest,
	) (PracticePlan, bool, error)
	ReadPlan(
		context.Context,
		requestcontext.Actor,
		string,
	) (PracticePlan, error)
	RevisePlan(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		RevisePlanRequest,
	) (PracticePlan, bool, error)
}

type PlanService struct {
	repository PlanRepository
	ids        ResourceIDGenerator
	snapshots  ProfileSnapshotReader
	goals      goal.Reader
	threads    SourceThreadReader
	selections scene.AccessibleSelectionReader
	ielts      scene.IELTSQuestionBankReader
	policies   PolicyResolver
}

func NewPlanService(
	repository PlanRepository,
	ids ResourceIDGenerator,
	snapshots ProfileSnapshotReader,
	goals goal.Reader,
	threads SourceThreadReader,
	catalog scene.CatalogReader,
	policies PolicyResolver,
) (*PlanService, error) {
	if repository == nil || ids == nil || snapshots == nil || goals == nil ||
		threads == nil || catalog == nil || policies == nil {
		return nil, errors.New("preparation: plan dependency is required")
	}
	ielts, ok := catalog.(scene.IELTSQuestionBankReader)
	if !ok {
		return nil, errors.New("preparation: IELTS question catalog is required")
	}
	selections, ok := catalog.(scene.AccessibleSelectionReader)
	if !ok {
		return nil, errors.New("preparation: accessible Scene selection is required")
	}
	return &PlanService{
		repository: repository,
		ids:        ids,
		snapshots:  snapshots,
		goals:      goals,
		threads:    threads,
		selections: selections,
		ielts:      ielts,
		policies:   policies,
	}, nil
}

func (s *PlanService) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request CreatePlanRequest,
) (PracticePlan, bool, error) {
	if ctx == nil || !actor.Valid() || !validCreatePlanRequest(request) {
		return PracticePlan{}, false, ErrPlanInvalid
	}
	intent, err := newPlanIntent(
		"POST",
		"/v1/practice-plans",
		idempotencyKey,
		request,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	plan, found, err := s.repository.ReplayPlan(ctx, actor, intent)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if found {
		if !validReturnedPlan(plan, actor, plan.ID) {
			return PracticePlan{}, false, ErrPlanRepository
		}
		return clonePracticePlan(plan), true, nil
	}

	preparationSnapshot, err := s.snapshots.ReadSnapshot(
		ctx,
		actor,
		request.PreparationSnapshotID,
	)
	if err != nil {
		if errors.Is(err, ErrProfileNotFound) {
			return PracticePlan{}, false, ErrPlanNotFound
		}
		return PracticePlan{}, false, err
	}
	if !validPlanPreparationSnapshot(
		preparationSnapshot,
		request.PreparationSnapshotID,
	) {
		return PracticePlan{}, false, ErrPlanConflict
	}

	selection, err := s.selections.ResolveAccessibleSelection(
		ctx,
		actor.UserID,
		request.SceneID,
		request.SceneVersion,
		clonePlanStrings(request.SelectedRoleIDs),
		request.PracticeOptionID,
	)
	if err != nil {
		return PracticePlan{}, false, planSelectionError(err)
	}
	if !selectionMatchesCreateRequest(selection, request) {
		return PracticePlan{}, false, ErrPlanConflict
	}
	selection, ieltsAssignment, err := freezeIELTSAssignment(
		s.ielts,
		selection,
		request.IELTSSelection,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}

	goalSnapshot, err := s.resolveGoalSnapshot(ctx, actor, request.GoalID)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if err := s.validateSourceThread(
		ctx,
		actor,
		request.SourceThreadID,
	); err != nil {
		return PracticePlan{}, false, err
	}

	policy, objectives, err := buildPlanExecution(
		s.policies,
		selection,
		request.MaxEffectiveTurns,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	planID, err := s.ids.NewID()
	if err != nil || !validPlanResourceID(planID) {
		return PracticePlan{}, false, ErrPlanRepository
	}
	created, replayed, err := s.repository.CreatePlan(ctx, actor, CreatePlanCommand{
		PlanID:              planID,
		SourceThreadID:      request.SourceThreadID,
		GoalSnapshot:        cloneGoalSnapshot(goalSnapshot),
		PreparationSnapshot: clonePlanPreparationSnapshot(preparationSnapshot),
		SceneSelection:      clonePlanSceneSelection(selection),
		SessionPolicy:       policy,
		PracticeObjectives:  clonePlanObjectives(objectives),
		IELTSAssignment:     cloneIELTSAssignment(ieltsAssignment),
		Intent:              intent,
	})
	if err != nil {
		return PracticePlan{}, false, err
	}
	expectedID := planID
	if replayed {
		expectedID = created.ID
	}
	if !validReturnedPlan(created, actor, expectedID) {
		return PracticePlan{}, false, ErrPlanRepository
	}
	return clonePracticePlan(created), replayed, nil
}

func (s *PlanService) ReadPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (PracticePlan, error) {
	if ctx == nil || !actor.Valid() || !validPlanResourceID(planID) {
		return PracticePlan{}, ErrPlanNotFound
	}
	plan, err := s.repository.ReadCurrentPlan(ctx, actor, planID)
	if err != nil {
		return PracticePlan{}, err
	}
	if !validReturnedPlan(plan, actor, planID) {
		return PracticePlan{}, ErrPlanRepository
	}
	return clonePracticePlan(plan), nil
}

func (s *PlanService) RevisePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	idempotencyKey string,
	request RevisePlanRequest,
) (PracticePlan, bool, error) {
	if ctx == nil || !actor.Valid() || !validPlanResourceID(planID) ||
		!validRevisePlanRequest(request) {
		return PracticePlan{}, false, ErrPlanInvalid
	}
	intent, err := newPlanIntent(
		"PUT",
		"/v1/practice-plans/"+planID,
		idempotencyKey,
		request,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	plan, found, err := s.repository.ReplayPlan(ctx, actor, intent)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if found {
		if !validReturnedPlan(plan, actor, plan.ID) {
			return PracticePlan{}, false, ErrPlanRepository
		}
		return clonePracticePlan(plan), true, nil
	}

	current, err := s.repository.ReadCurrentPlan(ctx, actor, planID)
	if err != nil {
		return PracticePlan{}, false, err
	}
	if !validReturnedPlan(current, actor, planID) {
		return PracticePlan{}, false, ErrPlanRepository
	}
	if current.Status != PlanStatusReady ||
		current.Revision != request.ExpectedPlanRevision {
		return PracticePlan{}, false, ErrPlanConflict
	}
	selection, err := reviseFrozenSelection(
		current.SceneSelection,
		request.SelectedRoleIDs,
		request.PracticeOptionID,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	policy, objectives, err := buildPlanExecution(
		s.policies,
		selection,
		request.MaxEffectiveTurns,
	)
	if err != nil {
		return PracticePlan{}, false, err
	}
	revised, replayed, err := s.repository.RevisePlan(ctx, actor, RevisePlanCommand{
		PlanID:               planID,
		ExpectedPlanRevision: request.ExpectedPlanRevision,
		SceneSelection:       clonePlanSceneSelection(selection),
		SessionPolicy:        policy,
		PracticeObjectives:   clonePlanObjectives(objectives),
		IELTSAssignment:      cloneIELTSAssignment(current.IELTSAssignment),
		Intent:               intent,
	})
	if err != nil {
		return PracticePlan{}, false, err
	}
	if !validReturnedPlan(revised, actor, planID) ||
		revised.Status != PlanStatusReady ||
		revised.Revision != request.ExpectedPlanRevision+1 {
		return PracticePlan{}, false, ErrPlanRepository
	}
	return clonePracticePlan(revised), replayed, nil
}

func (s *PlanService) ReadExecutablePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	exactRevision int,
) (PracticePlan, error) {
	if ctx == nil || !actor.Valid() || !validPlanResourceID(planID) ||
		exactRevision < 1 {
		return PracticePlan{}, ErrPlanNotFound
	}
	plan, err := s.repository.ReadExecutablePlan(
		ctx,
		actor,
		planID,
		exactRevision,
	)
	if err != nil {
		return PracticePlan{}, err
	}
	if !validReturnedPlan(plan, actor, planID) ||
		plan.Status != PlanStatusReady || plan.Revision != exactRevision {
		return PracticePlan{}, ErrPlanRepository
	}
	return clonePracticePlan(plan), nil
}

func (s *PlanService) resolveGoalSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	goalID string,
) (*GoalSnapshot, error) {
	if goalID == "" {
		return nil, nil
	}
	value, err := s.goals.ReadOwned(ctx, actor, goalID)
	if err != nil {
		if errors.Is(err, goal.ErrNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}
	if value.ID != goalID || value.Version < 1 ||
		!validPlanText(value.Title) {
		return nil, ErrPlanConflict
	}
	return &GoalSnapshot{
		ID:      value.ID,
		Title:   value.Title,
		Version: value.Version,
	}, nil
}

func (s *PlanService) validateSourceThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) error {
	if threadID == "" {
		return nil
	}
	thread, err := s.threads.ReadOwnedThread(ctx, actor, threadID)
	if err != nil {
		return err
	}
	if thread.ID != threadID {
		return ErrPlanConflict
	}
	return nil
}

func newPlanIntent(
	method string,
	path string,
	key string,
	payload any,
) (IdempotencyIntent, error) {
	if (method != "POST" && method != "PUT") ||
		!validCanonicalPath(path) || !validIdempotencyKey(key) {
		return IdempotencyIntent{}, ErrPlanInvalid
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return IdempotencyIntent{}, ErrPlanInvalid
	}
	return IdempotencyIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(canonical),
	}, nil
}

func validCreatePlanRequest(request CreatePlanRequest) bool {
	return (request.SourceThreadID == "" ||
		validPlanResourceID(request.SourceThreadID)) &&
		(request.GoalID == "" || validPlanResourceID(request.GoalID)) &&
		validPlanResourceID(request.PreparationSnapshotID) &&
		validPlanResourceID(request.SceneID) && request.SceneVersion > 0 &&
		validUniquePlanIDs(request.SelectedRoleIDs) &&
		validPlanResourceID(request.PracticeOptionID) &&
		request.MaxEffectiveTurns >= 0 &&
		(request.IELTSSelection == nil ||
			validIELTSQuestionSelection(*request.IELTSSelection))
}

func validRevisePlanRequest(request RevisePlanRequest) bool {
	return request.ExpectedPlanRevision > 0 &&
		validUniquePlanIDs(request.SelectedRoleIDs) &&
		validPlanResourceID(request.PracticeOptionID) &&
		request.MaxEffectiveTurns > 0
}

func validPlanPreparationSnapshot(snapshot Snapshot, expectedID string) bool {
	if snapshot.ID != expectedID ||
		!validPlanResourceID(snapshot.SourceProfileID) ||
		snapshot.SourceVersion < 1 || snapshot.CreatedAt.IsZero() ||
		!validPlanText(snapshot.BackgroundSnapshot) {
		return false
	}
	withoutTarget := snapshot.SourceJobTargetID == "" &&
		snapshot.SourceJobTargetConfirmationVersion == 0 &&
		snapshot.JobTargetInputSnapshot == nil &&
		snapshot.JobTargetCandidateSnapshot == nil
	return withoutTarget || targetedPreparationSnapshot(snapshot)
}

func selectionMatchesCreateRequest(
	selection scene.SelectionSnapshot,
	request CreatePlanRequest,
) bool {
	if selection.Scene.ID != request.SceneID ||
		selection.Scene.Version != request.SceneVersion ||
		selection.Scene.Status != scene.SceneStatusActive ||
		selection.PracticeOptionID != request.PracticeOptionID ||
		!equalPlanStrings(selection.SelectedRoleIDs, request.SelectedRoleIDs) {
		return false
	}
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) != len(request.SelectedRoleIDs) {
		return false
	}
	option, err := selection.PracticeOption()
	return err == nil && validSelectedPlanOption(selection, roles, option)
}

func freezeIELTSAssignment(
	catalog scene.IELTSQuestionBankReader,
	selection scene.SelectionSnapshot,
	request *IELTSQuestionSelection,
) (scene.SelectionSnapshot, *IELTSAssignmentSnapshot, error) {
	expectedMode, isIELTS := expectedIELTSMode(selection.Scene)
	if !isIELTS {
		if request != nil {
			return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
		}
		return clonePlanSceneSelection(selection), nil, nil
	}
	if request == nil || request.Mode != expectedMode ||
		!validIELTSQuestionSelection(*request) {
		return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
	}
	resolved, err := catalog.ResolveIELTSQuestionSet(
		scene.IELTSQuestionSetSelection{
			Mode:         request.Mode,
			Part1SetID:   request.Part1SetID,
			TopicGroupID: request.TopicGroupID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, scene.ErrIELTSQuestionSetNotFound):
			return scene.SelectionSnapshot{}, nil, ErrPlanNotFound
		case errors.Is(err, scene.ErrIELTSPracticeModeInvalid):
			return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
		default:
			return scene.SelectionSnapshot{}, nil, err
		}
	}
	if !validResolvedIELTSQuestionSet(*request, resolved) {
		return scene.SelectionSnapshot{}, nil, ErrPlanConflict
	}

	assignment := &IELTSAssignmentSnapshot{
		BankID:         resolved.BankID,
		Season:         resolved.Season,
		Mode:           resolved.Mode,
		Part1SetID:     resolved.Part1SetID,
		TopicGroupID:   resolved.TopicGroupID,
		TopicTitle:     resolved.TopicTitle,
		Part1Questions: resolved.Part1Questions,
		Part2Questions: resolved.Part2Questions,
		Part3Questions: resolved.Part3Questions,
		TurnBlueprints: clonePlanStrings(resolved.TurnBlueprints),
	}
	if resolved.Mode == scene.IELTSPracticeModeFullMock ||
		resolved.Mode == scene.IELTSPracticeModePart2 {
		assignment.Part2CueCard = resolved.Part2CueCard
	}
	selection = clonePlanSceneSelection(selection)
	selection.Scene.Prompt.TurnBlueprints = clonePlanStrings(
		assignment.TurnBlueprints,
	)
	selection.Scene.Prompt.PublicSceneBrief = ieltsAssignmentSceneBrief(
		*assignment,
	)
	if !validPlanIELTSAssignment(selection, assignment) {
		return scene.SelectionSnapshot{}, nil, ErrPlanConflict
	}
	return selection, assignment, nil
}

func expectedIELTSMode(
	definition scene.SceneDefinition,
) (scene.IELTSPracticeMode, bool) {
	if definition.Family != scene.SceneFamilyExam {
		return "", false
	}
	switch definition.Model {
	case scene.SceneModelIELTSSpeakingFullMock:
		return scene.IELTSPracticeModeFullMock, true
	case scene.SceneModelIELTSSpeakingPart1:
		return scene.IELTSPracticeModePart1, true
	case scene.SceneModelIELTSSpeakingPart2:
		return scene.IELTSPracticeModePart2, true
	case scene.SceneModelIELTSSpeakingPart3:
		return scene.IELTSPracticeModePart3, true
	default:
		return "", false
	}
}

func validIELTSQuestionSelection(selection IELTSQuestionSelection) bool {
	switch selection.Mode {
	case scene.IELTSPracticeModeFullMock:
		return validPlanResourceID(selection.Part1SetID) &&
			validPlanResourceID(selection.TopicGroupID)
	case scene.IELTSPracticeModePart1:
		return validPlanResourceID(selection.Part1SetID) &&
			selection.TopicGroupID == ""
	case scene.IELTSPracticeModePart2, scene.IELTSPracticeModePart3:
		return selection.Part1SetID == "" &&
			validPlanResourceID(selection.TopicGroupID)
	default:
		return false
	}
}

func validResolvedIELTSQuestionSet(
	request IELTSQuestionSelection,
	resolved scene.IELTSResolvedQuestionSet,
) bool {
	if resolved.Mode != request.Mode ||
		resolved.Part1SetID != request.Part1SetID ||
		resolved.TopicGroupID != request.TopicGroupID ||
		!validPlanResourceID(resolved.BankID) ||
		!validPlanText(resolved.Season) ||
		!validIELTSTurnBlueprints(resolved.TurnBlueprints) {
		return false
	}
	switch resolved.Mode {
	case scene.IELTSPracticeModeFullMock:
		return validPlanText(resolved.TopicTitle) &&
			validPlanText(resolved.Part2CueCard) &&
			resolved.Part1Questions == 8 &&
			resolved.Part2Questions == 1 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 5 &&
			len(resolved.TurnBlueprints) == 9+resolved.Part3Questions
	case scene.IELTSPracticeModePart1:
		return resolved.TopicTitle == "" &&
			resolved.Part2CueCard == "" &&
			resolved.Part1Questions >= 2 &&
			resolved.Part1Questions <= 24 &&
			resolved.Part2Questions == 0 &&
			resolved.Part3Questions == 0 &&
			len(resolved.TurnBlueprints) == resolved.Part1Questions
	case scene.IELTSPracticeModePart2:
		return validPlanText(resolved.TopicTitle) &&
			validPlanText(resolved.Part2CueCard) &&
			resolved.Part1Questions == 0 &&
			resolved.Part2Questions == 1 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 6 &&
			len(resolved.TurnBlueprints) == 1+resolved.Part3Questions
	case scene.IELTSPracticeModePart3:
		return validPlanText(resolved.TopicTitle) &&
			resolved.Part1Questions == 0 &&
			resolved.Part2Questions == 0 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 6 &&
			len(resolved.TurnBlueprints) == resolved.Part3Questions
	default:
		return false
	}
}

func validPlanIELTSAssignment(
	selection scene.SelectionSnapshot,
	assignment *IELTSAssignmentSnapshot,
) bool {
	expectedMode, isIELTS := expectedIELTSMode(selection.Scene)
	if !isIELTS {
		return assignment == nil
	}
	if assignment == nil || assignment.Mode != expectedMode ||
		!validPlanResourceID(assignment.BankID) ||
		!validPlanText(assignment.Season) ||
		!validIELTSTurnBlueprints(assignment.TurnBlueprints) ||
		!equalPlanStrings(
			selection.Scene.Prompt.TurnBlueprints,
			assignment.TurnBlueprints,
		) || !validPlanText(selection.Scene.Prompt.PublicSceneBrief) {
		return false
	}
	switch assignment.Mode {
	case scene.IELTSPracticeModeFullMock:
		return validPlanResourceID(assignment.Part1SetID) &&
			validPlanResourceID(assignment.TopicGroupID) &&
			validPlanText(assignment.TopicTitle) &&
			validPlanText(assignment.Part2CueCard) &&
			assignment.Part1Questions == 8 &&
			assignment.Part2Questions == 1 &&
			assignment.Part3Questions >= 1 &&
			assignment.Part3Questions <= 5 &&
			len(assignment.TurnBlueprints) == 9+assignment.Part3Questions
	case scene.IELTSPracticeModePart1:
		return validPlanResourceID(assignment.Part1SetID) &&
			assignment.TopicGroupID == "" &&
			assignment.TopicTitle == "" &&
			assignment.Part2CueCard == "" &&
			assignment.Part1Questions >= 2 &&
			assignment.Part1Questions <= 24 &&
			assignment.Part2Questions == 0 &&
			assignment.Part3Questions == 0 &&
			len(assignment.TurnBlueprints) == assignment.Part1Questions
	case scene.IELTSPracticeModePart2:
		return assignment.Part1SetID == "" &&
			validPlanResourceID(assignment.TopicGroupID) &&
			validPlanText(assignment.TopicTitle) &&
			validPlanText(assignment.Part2CueCard) &&
			assignment.Part1Questions == 0 &&
			assignment.Part2Questions == 1 &&
			assignment.Part3Questions >= 1 &&
			assignment.Part3Questions <= 6 &&
			len(assignment.TurnBlueprints) == 1+assignment.Part3Questions
	case scene.IELTSPracticeModePart3:
		return assignment.Part1SetID == "" &&
			validPlanResourceID(assignment.TopicGroupID) &&
			validPlanText(assignment.TopicTitle) &&
			assignment.Part2CueCard == "" &&
			assignment.Part1Questions == 0 &&
			assignment.Part2Questions == 0 &&
			assignment.Part3Questions >= 1 &&
			assignment.Part3Questions <= 6 &&
			len(assignment.TurnBlueprints) == assignment.Part3Questions
	default:
		return false
	}
}

func validIELTSTurnBlueprints(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !validPlanText(value) {
			return false
		}
	}
	return true
}

func ieltsAssignmentSceneBrief(assignment IELTSAssignmentSnapshot) string {
	switch assignment.Mode {
	case scene.IELTSPracticeModeFullMock:
		return "完成冻结的 Part 1 套题，并继续同主题 Part 2 与 Part 3。"
	case scene.IELTSPracticeModePart1:
		return "完成冻结的三个熟悉话题和八道 Part 1 问题。"
	case scene.IELTSPracticeModePart2:
		return "完成“" + assignment.TopicTitle + "”题卡，并可继续同主题 Part 3。"
	case scene.IELTSPracticeModePart3:
		return "围绕“" + assignment.TopicTitle + "”完成同主题 Part 3 讨论。"
	default:
		return ""
	}
}

func reviseFrozenSelection(
	current scene.SelectionSnapshot,
	selectedRoleIDs []string,
	practiceOptionID string,
) (scene.SelectionSnapshot, error) {
	if current.Scene.ID == "" || current.Scene.Version < 1 ||
		!validUniquePlanIDs(selectedRoleIDs) ||
		!validPlanResourceID(practiceOptionID) {
		return scene.SelectionSnapshot{}, ErrPlanConflict
	}
	selection := scene.SelectionSnapshot{
		Scene:            clonePlanSceneDefinition(current.Scene),
		SelectedRoleIDs:  clonePlanStrings(selectedRoleIDs),
		PracticeOptionID: practiceOptionID,
	}
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) != len(selectedRoleIDs) {
		return scene.SelectionSnapshot{}, ErrPlanInvalid
	}
	option, err := selection.PracticeOption()
	if err != nil || !validSelectedPlanOption(selection, roles, option) {
		return scene.SelectionSnapshot{}, ErrPlanInvalid
	}
	return selection, nil
}

func validSelectedPlanOption(
	selection scene.SelectionSnapshot,
	roles []scene.RoleDefinition,
	option scene.PracticeOption,
) bool {
	if option.ID != selection.PracticeOptionID ||
		option.SceneID != selection.Scene.ID {
		return false
	}
	for index, role := range roles {
		if role.ID != selection.SelectedRoleIDs[index] ||
			role.SceneID != selection.Scene.ID {
			return false
		}
	}
	switch option.Type {
	case scene.PracticeOptionFullSimulation:
		return option.RoleDefinitionID == ""
	case scene.PracticeOptionFocus:
		return len(roles) == 1 && option.RoleDefinitionID == roles[0].ID
	default:
		return false
	}
}

func buildPlanExecution(
	policies PolicyResolver,
	selection scene.SelectionSnapshot,
	maxEffectiveTurns int,
) (SessionPolicy, []PracticeObjective, error) {
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return SessionPolicy{}, nil, ErrPlanInvalid
	}
	option, err := selection.PracticeOption()
	if err != nil || !validSelectedPlanOption(selection, roles, option) {
		return SessionPolicy{}, nil, ErrPlanInvalid
	}
	policy, err := policies.ResolveSessionPolicy(
		selection.Scene,
		option,
		maxEffectiveTurns,
	)
	if err != nil {
		return SessionPolicy{}, nil, err
	}
	objectives, err := practiceObjectives(roles)
	if err != nil {
		return SessionPolicy{}, nil, err
	}
	if !validPracticeObjectives(objectives) {
		return SessionPolicy{}, nil, ErrPlanInvalid
	}
	return policy, objectives, nil
}

func practiceObjectives(
	roles []scene.RoleDefinition,
) ([]PracticeObjective, error) {
	seen := make(map[string]string)
	objectives := make([]PracticeObjective, 0)
	for _, role := range roles {
		for _, definition := range role.PracticeObjectives {
			if description, exists := seen[definition.ID]; exists {
				if description != definition.Description {
					return nil, ErrPlanInvalid
				}
				continue
			}
			seen[definition.ID] = definition.Description
			objectives = append(objectives, PracticeObjective{
				ID:          definition.ID,
				Description: definition.Description,
			})
		}
	}
	return objectives, nil
}

func planSelectionError(err error) error {
	switch {
	case errors.Is(err, scene.ErrSceneNotFound):
		return ErrPlanNotFound
	case errors.Is(err, scene.ErrRoleDefinitionNotFound),
		errors.Is(err, scene.ErrPracticeOptionNotFound),
		errors.Is(err, scene.ErrCatalogSelectionInvalid),
		errors.Is(err, scene.ErrCatalogDefinitionInvalid):
		return ErrPlanInvalid
	default:
		return err
	}
}

func validReturnedPlan(
	plan PracticePlan,
	actor requestcontext.Actor,
	expectedID string,
) bool {
	if plan.ID != expectedID || !validPlanResourceID(plan.ID) ||
		plan.UserID != actor.UserID {
		return false
	}
	if plan.Revision < 1 ||
		(plan.Status != PlanStatusReady && plan.Status != PlanStatusArchived) ||
		(plan.SourceThreadID != "" &&
			!validPlanResourceID(plan.SourceThreadID)) ||
		plan.CreatedAt.IsZero() || plan.UpdatedAt.Before(plan.CreatedAt) ||
		!validPlanPreparationSnapshot(
			plan.PreparationSnapshot,
			plan.PreparationSnapshot.ID,
		) {
		return false
	}
	if plan.GoalSnapshot != nil &&
		(!validPlanResourceID(plan.GoalSnapshot.ID) ||
			!validPlanText(plan.GoalSnapshot.Title) ||
			plan.GoalSnapshot.Version < 1) {
		return false
	}
	if !validPlanResourceID(plan.SceneSelection.Scene.ID) ||
		plan.SceneSelection.Scene.Version < 1 ||
		plan.SceneSelection.Scene.Status != scene.SceneStatusActive ||
		!validUniquePlanIDs(plan.SceneSelection.SelectedRoleIDs) {
		return false
	}
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return false
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil || !validSelectedPlanOption(
		plan.SceneSelection,
		roles,
		option,
	) {
		return false
	}
	if !validStoredSessionPolicy(plan.SessionPolicy) {
		return false
	}
	return validPracticeObjectives(plan.PracticeObjectives) &&
		validPlanIELTSAssignment(
			plan.SceneSelection,
			plan.IELTSAssignment,
		)
}

func validPracticeObjectives(objectives []PracticeObjective) bool {
	if len(objectives) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(objectives))
	for _, objective := range objectives {
		if !planObjectiveIDPattern.MatchString(objective.ID) ||
			!validPlanText(objective.Description) {
			return false
		}
		if _, duplicate := seen[objective.ID]; duplicate {
			return false
		}
		seen[objective.ID] = struct{}{}
	}
	return true
}

func validStoredSessionPolicy(policy SessionPolicy) bool {
	return policy.SuggestedDurationSeconds > 0 &&
		policy.MinEffectiveTurns > 0 &&
		policy.MaxEffectiveTurns >= policy.MinEffectiveTurns &&
		policy.CoverageCheckpointTurn > 0 &&
		policy.CoverageCheckpointTurn <= policy.MaxEffectiveTurns &&
		policy.MaxFollowUpsPerQuestion >= 0 &&
		policy.EarlyCompletionRule ==
			EarlyCompletionCoverageSatisfiedAfterCheckpoint
}

func validPlanResourceID(value string) bool {
	return utf8.ValidString(value) && value != "" && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validUniquePlanIDs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validPlanResourceID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validPlanText(value string) bool {
	return utf8.ValidString(value) && value != "" &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func equalPlanStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneGoalSnapshot(source *GoalSnapshot) *GoalSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	return &result
}

func clonePlanPreparationSnapshot(source Snapshot) Snapshot {
	result := source
	result.JobTargetInputSnapshot = cloneSnapshotJobTargetInput(
		source.JobTargetInputSnapshot,
	)
	result.JobTargetCandidateSnapshot = cloneSnapshotJobTargetCandidate(
		source.JobTargetCandidateSnapshot,
	)
	return result
}

func clonePlanSceneSelection(
	source scene.SelectionSnapshot,
) scene.SelectionSnapshot {
	return scene.SelectionSnapshot{
		Scene:            clonePlanSceneDefinition(source.Scene),
		SelectedRoleIDs:  clonePlanStrings(source.SelectedRoleIDs),
		PracticeOptionID: source.PracticeOptionID,
	}
}

func clonePlanSceneDefinition(
	source scene.SceneDefinition,
) scene.SceneDefinition {
	result := source
	result.Prompt.FocusAreas = clonePlanStrings(source.Prompt.FocusAreas)
	result.Prompt.TurnBlueprints = clonePlanStrings(
		source.Prompt.TurnBlueprints,
	)
	result.Roles = make([]scene.RoleDefinition, len(source.Roles))
	for index, role := range source.Roles {
		result.Roles[index] = role
		result.Roles[index].PracticeObjectives = append(
			[]scene.PracticeObjectiveDefinition(nil),
			role.PracticeObjectives...,
		)
	}
	result.PracticeOptions = append(
		[]scene.PracticeOption(nil),
		source.PracticeOptions...,
	)
	return result
}

func clonePlanObjectives(source []PracticeObjective) []PracticeObjective {
	return append([]PracticeObjective(nil), source...)
}

func cloneIELTSAssignment(
	source *IELTSAssignmentSnapshot,
) *IELTSAssignmentSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	result.TurnBlueprints = clonePlanStrings(source.TurnBlueprints)
	return &result
}

func clonePracticePlan(source PracticePlan) PracticePlan {
	result := source
	result.GoalSnapshot = cloneGoalSnapshot(source.GoalSnapshot)
	result.PreparationSnapshot = clonePlanPreparationSnapshot(
		source.PreparationSnapshot,
	)
	result.SceneSelection = clonePlanSceneSelection(source.SceneSelection)
	result.PracticeObjectives = clonePlanObjectives(source.PracticeObjectives)
	result.IELTSAssignment = cloneIELTSAssignment(source.IELTSAssignment)
	return result
}

func clonePlanStrings(values []string) []string {
	return append([]string(nil), values...)
}

var _ PlanApplication = (*PlanService)(nil)
