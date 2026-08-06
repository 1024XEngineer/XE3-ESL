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
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var planObjectiveIDPattern = regexp.MustCompile(
	`^[a-z][a-z0-9_]{0,127}$`,
)

// maxPlanEffectiveTurns is a transport/runtime safety bound, not an IELTS
// question-count rule.
const maxPlanEffectiveTurns = 64

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
	ListPlans(
		context.Context,
		requestcontext.Actor,
		scene.PracticeExperience,
	) ([]PracticePlanSummary, error)
	RevisePlan(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		RevisePlanRequest,
	) (PracticePlan, bool, error)
	ArchivePlan(context.Context, requestcontext.Actor, string) error
}

func (s *PlanService) ListPlans(
	ctx context.Context,
	actor requestcontext.Actor,
	experience scene.PracticeExperience,
) ([]PracticePlanSummary, error) {
	if ctx == nil || !actor.Valid() ||
		(experience != scene.PracticeExperienceInterview &&
			experience != scene.PracticeExperienceIELTSSpeaking &&
			experience != scene.PracticeExperienceRoleplay) {
		return nil, ErrPlanInvalid
	}
	plans, err := s.repository.ListCurrentPlans(ctx, actor, experience)
	if err != nil {
		return nil, err
	}
	summaries := make([]PracticePlanSummary, 0, len(plans))
	for _, plan := range plans {
		if !validReturnedPlan(plan, actor, plan.ID) ||
			plan.SceneSelection.Scene.Experience != experience {
			return nil, ErrPlanRepository
		}
		option, err := plan.SceneSelection.PracticeOption()
		if err != nil {
			return nil, ErrPlanRepository
		}
		objectives := make([]string, 0, len(plan.PracticeObjectives))
		for _, objective := range plan.PracticeObjectives {
			objectives = append(objectives, objective.Description)
		}
		jobTitle := ""
		if candidate := plan.PreparationSnapshot.JobTargetCandidateSnapshot; candidate != nil {
			jobTitle = candidate.JobTitle
		}
		summaries = append(summaries, PracticePlanSummary{
			ID:                       plan.ID,
			Revision:                 plan.Revision,
			Status:                   plan.Status,
			PracticeExperience:       experience,
			SceneName:                plan.SceneSelection.Scene.Name,
			PracticeScope:            option.DisplayName,
			JobTitle:                 jobTitle,
			PracticeObjectives:       objectives,
			ResumeUsed:               plan.PreparationSnapshot.ResumeSnapshot != nil,
			SuggestedDurationSeconds: plan.SessionPolicy.SuggestedDurationSeconds,
			MinEffectiveTurns:        plan.SessionPolicy.MinEffectiveTurns,
			MaxEffectiveTurns:        plan.SessionPolicy.MaxEffectiveTurns,
			CreatedAt:                plan.CreatedAt,
			UpdatedAt:                plan.UpdatedAt,
		})
	}
	return summaries, nil
}

type PlanService struct {
	repository PlanRepository
	ids        ResourceIDGenerator
	snapshots  ProfileSnapshotReader
	goals      goal.Reader
	threads    SourceThreadReader
	selections scene.AccessibleSelectionReader
	ielts      ielts.QuestionSetResolver
	policies   PolicyResolver
}

func NewPlanService(
	repository PlanRepository,
	ids ResourceIDGenerator,
	snapshots ProfileSnapshotReader,
	goals goal.Reader,
	threads SourceThreadReader,
	catalog scene.CatalogReader,
	ieltsQuestions ielts.QuestionSetResolver,
	policies PolicyResolver,
) (*PlanService, error) {
	if repository == nil || ids == nil || snapshots == nil || goals == nil ||
		threads == nil || catalog == nil || ieltsQuestions == nil ||
		policies == nil {
		return nil, errors.New("preparation: plan dependency is required")
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
		ielts:      ieltsQuestions,
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

func (s *PlanService) ArchivePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) error {
	if ctx == nil || !actor.Valid() || !validPlanResourceID(planID) {
		return ErrPlanNotFound
	}
	return s.repository.ArchivePlan(ctx, actor, planID)
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
	selection, ieltsAssignment, err := freezeIELTSAssignment(
		s.ielts,
		selection,
		request.IELTSSelection,
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
		IELTSAssignment:      cloneIELTSAssignment(ieltsAssignment),
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
			validIELTSSelectionShape(*request.IELTSSelection))
}

func validRevisePlanRequest(request RevisePlanRequest) bool {
	return request.ExpectedPlanRevision > 0 &&
		validUniquePlanIDs(request.SelectedRoleIDs) &&
		validPlanResourceID(request.PracticeOptionID) &&
		request.MaxEffectiveTurns > 0 &&
		(request.IELTSSelection == nil ||
			validIELTSSelectionShape(*request.IELTSSelection))
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
	questions ielts.QuestionSetResolver,
	selection scene.SelectionSnapshot,
	request *IELTSQuestionSelection,
) (scene.SelectionSnapshot, *IELTSAssignmentSnapshot, error) {
	option, err := selection.PracticeOption()
	if err != nil {
		return scene.SelectionSnapshot{}, nil, ErrPlanConflict
	}
	isIELTS := selection.Scene.Experience ==
		scene.PracticeExperienceIELTSSpeaking
	if !isIELTS {
		if request != nil {
			return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
		}
		return clonePlanSceneSelection(selection), nil, nil
	}
	mode, validMode := ieltsPracticeMode(option.Mode)
	if selection.Scene.Category != scene.SceneCategoryIELTSSpeaking ||
		!validMode || request == nil ||
		!validIELTSQuestionSelection(option.Mode, *request) {
		return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
	}
	resolved, err := questions.ResolveQuestionSet(
		ielts.QuestionSetSelection{
			Mode:         mode,
			Part1SetID:   request.Part1SetID,
			TopicGroupID: request.TopicGroupID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ielts.ErrQuestionSetNotFound):
			return scene.SelectionSnapshot{}, nil, ErrPlanNotFound
		case errors.Is(err, ielts.ErrPracticeModeInvalid):
			return scene.SelectionSnapshot{}, nil, ErrPlanInvalid
		default:
			return scene.SelectionSnapshot{}, nil, err
		}
	}
	if !validResolvedIELTSQuestionSet(option.Mode, *request, resolved) {
		return scene.SelectionSnapshot{}, nil, ErrPlanConflict
	}

	assignment := &IELTSAssignmentSnapshot{
		BankID: resolved.BankID,
		Season: resolved.Season,
		Mode:   option.Mode,
		Parts:  make([]IELTSAssignmentPartSnapshot, len(resolved.Parts)),
	}
	for index, part := range resolved.Parts {
		assignment.Parts[index] = IELTSAssignmentPartSnapshot{
			Part:           scene.PracticeMode(part.Part),
			SourceID:       part.SourceID,
			TopicTitle:     part.TopicTitle,
			CueCard:        part.CueCard,
			TurnBlueprints: clonePlanStrings(part.TurnBlueprints),
		}
	}
	selection = clonePlanSceneSelection(selection)
	selection.Scene.Prompt.TurnBlueprints = clonePlanStrings(
		ieltsAssignmentTurnBlueprints(*assignment),
	)
	selection.Scene.Prompt.PublicSceneBrief = ieltsAssignmentSceneBrief(
		*assignment,
	)
	if !validPlanIELTSAssignment(selection, assignment) {
		return scene.SelectionSnapshot{}, nil, ErrPlanConflict
	}
	return selection, assignment, nil
}

func ieltsPracticeMode(mode scene.PracticeMode) (ielts.PracticeMode, bool) {
	switch mode {
	case scene.PracticeModeFullMock,
		scene.PracticeModePart1,
		scene.PracticeModePart2,
		scene.PracticeModePart3:
		return ielts.PracticeMode(mode), true
	default:
		return "", false
	}
}

func validIELTSQuestionSelection(
	mode scene.PracticeMode,
	selection IELTSQuestionSelection,
) bool {
	switch mode {
	case scene.PracticeModeFullMock:
		return validPlanResourceID(selection.Part1SetID) &&
			validPlanResourceID(selection.TopicGroupID)
	case scene.PracticeModePart1:
		return validPlanResourceID(selection.Part1SetID) &&
			selection.TopicGroupID == ""
	case scene.PracticeModePart2, scene.PracticeModePart3:
		return selection.Part1SetID == "" &&
			validPlanResourceID(selection.TopicGroupID)
	default:
		return false
	}
}

func validIELTSSelectionShape(selection IELTSQuestionSelection) bool {
	part1Valid := selection.Part1SetID == "" ||
		validPlanResourceID(selection.Part1SetID)
	topicValid := selection.TopicGroupID == "" ||
		validPlanResourceID(selection.TopicGroupID)
	return part1Valid && topicValid &&
		(selection.Part1SetID != "" || selection.TopicGroupID != "")
}

func validResolvedIELTSQuestionSet(
	mode scene.PracticeMode,
	request IELTSQuestionSelection,
	resolved ielts.ResolvedQuestionSet,
) bool {
	if scene.PracticeMode(resolved.Mode) != mode ||
		!validPlanResourceID(resolved.BankID) ||
		!validPlanText(resolved.Season) {
		return false
	}
	parts := make([]IELTSAssignmentPartSnapshot, len(resolved.Parts))
	for index, part := range resolved.Parts {
		parts[index] = IELTSAssignmentPartSnapshot{
			Part:           scene.PracticeMode(part.Part),
			SourceID:       part.SourceID,
			TopicTitle:     part.TopicTitle,
			CueCard:        part.CueCard,
			TurnBlueprints: part.TurnBlueprints,
		}
	}
	if !validIELTSAssignmentParts(mode, parts) {
		return false
	}
	for _, part := range parts {
		switch part.Part {
		case scene.PracticeModePart1:
			if part.SourceID != request.Part1SetID {
				return false
			}
		case scene.PracticeModePart2, scene.PracticeModePart3:
			if part.SourceID != request.TopicGroupID {
				return false
			}
		}
	}
	return true
}

func validPlanIELTSAssignment(
	selection scene.SelectionSnapshot,
	assignment *IELTSAssignmentSnapshot,
) bool {
	option, err := selection.PracticeOption()
	if err != nil {
		return false
	}
	isIELTS := selection.Scene.Experience ==
		scene.PracticeExperienceIELTSSpeaking
	if !isIELTS {
		return assignment == nil
	}
	if selection.Scene.Category != scene.SceneCategoryIELTSSpeaking ||
		assignment == nil || assignment.Mode != option.Mode ||
		!validPlanResourceID(assignment.BankID) ||
		!validPlanText(assignment.Season) ||
		!validIELTSAssignmentParts(assignment.Mode, assignment.Parts) ||
		!validPlanText(selection.Scene.Prompt.PublicSceneBrief) {
		return false
	}
	blueprints := ieltsAssignmentTurnBlueprints(*assignment)
	return len(blueprints) <= maxPlanEffectiveTurns &&
		equalPlanStrings(selection.Scene.Prompt.TurnBlueprints, blueprints)
}

func validIELTSAssignmentParts(
	mode scene.PracticeMode,
	parts []IELTSAssignmentPartSnapshot,
) bool {
	expected := []scene.PracticeMode(nil)
	switch mode {
	case scene.PracticeModeFullMock:
		expected = []scene.PracticeMode{
			scene.PracticeModePart1,
			scene.PracticeModePart2,
			scene.PracticeModePart3,
		}
	case scene.PracticeModePart1:
		expected = []scene.PracticeMode{scene.PracticeModePart1}
	case scene.PracticeModePart2:
		expected = []scene.PracticeMode{
			scene.PracticeModePart2,
			scene.PracticeModePart3,
		}
	case scene.PracticeModePart3:
		expected = []scene.PracticeMode{scene.PracticeModePart3}
	default:
		return false
	}
	if len(parts) != len(expected) {
		return false
	}
	for index, part := range parts {
		if part.Part != expected[index] ||
			!validPlanResourceID(part.SourceID) ||
			!validIELTSTurnBlueprints(part.TurnBlueprints) {
			return false
		}
		switch part.Part {
		case scene.PracticeModePart1:
			if part.TopicTitle != "" || part.CueCard != "" {
				return false
			}
		case scene.PracticeModePart2:
			if !validPlanText(part.TopicTitle) ||
				!validPlanText(part.CueCard) ||
				len(part.TurnBlueprints) != 1 {
				return false
			}
		case scene.PracticeModePart3:
			if !validPlanText(part.TopicTitle) || part.CueCard != "" {
				return false
			}
		}
	}
	if len(parts) >= 2 &&
		parts[len(parts)-2].Part == scene.PracticeModePart2 &&
		(parts[len(parts)-2].SourceID != parts[len(parts)-1].SourceID ||
			parts[len(parts)-2].TopicTitle != parts[len(parts)-1].TopicTitle) {
		return false
	}
	return true
}

func ieltsAssignmentTurnBlueprints(
	assignment IELTSAssignmentSnapshot,
) []string {
	var result []string
	for _, part := range assignment.Parts {
		result = append(result, part.TurnBlueprints...)
	}
	return result
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
	topicTitle := ""
	for _, part := range assignment.Parts {
		if part.TopicTitle != "" {
			topicTitle = part.TopicTitle
			break
		}
	}
	switch assignment.Mode {
	case scene.PracticeModeFullMock:
		return "完成冻结的 Part 1 套题，并继续同主题 Part 2 与 Part 3。"
	case scene.PracticeModePart1:
		return "完成冻结的三个熟悉话题和八道 Part 1 问题。"
	case scene.PracticeModePart2:
		return "完成“" + topicTitle + "”题卡，并可继续同主题 Part 3。"
	case scene.PracticeModePart3:
		return "围绕“" + topicTitle + "”完成同主题 Part 3 讨论。"
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
	switch option.Mode {
	case scene.PracticeModeFullSimulation,
		scene.PracticeModeFullMock,
		scene.PracticeModePart1,
		scene.PracticeModePart2,
		scene.PracticeModePart3:
		return option.RoleDefinitionID == ""
	case scene.PracticeModeFocus:
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
		policy.MaxEffectiveTurns <= maxPlanEffectiveTurns &&
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
	result.Parts = make([]IELTSAssignmentPartSnapshot, len(source.Parts))
	for index, part := range source.Parts {
		result.Parts[index] = part
		result.Parts[index].TurnBlueprints = clonePlanStrings(
			part.TurnBlueprints,
		)
	}
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
