package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	preparation "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var planObjectiveIDPattern = regexp.MustCompile(
	`^[a-z][a-z0-9_]{0,127}$`,
)

// maxPlanEffectiveTurns is a transport/runtime safety bound, not an IELTS
// question-count rule.
const (
	maxPlanEffectiveTurns    = 64
	customPlanEffectiveTurns = 5
)

type PlanService struct {
	repository preparation.PlanRepository
	ids        preparation.ResourceIDGenerator
	interviews preparation.InterviewPreparationReader
	threads    preparation.SourceThreadReader
	selections scene.AccessibleSelectionReader
	ielts      ielts.QuestionSetResolver
	policies   preparation.PolicyResolver
}

func NewPlanService(repository preparation.PlanRepository, ids preparation.ResourceIDGenerator, interviews preparation.InterviewPreparationReader, threads preparation.SourceThreadReader, selections scene.AccessibleSelectionReader, ieltsQuestions ielts.QuestionSetResolver, policies preparation.PolicyResolver) (*PlanService, error) {
	if repository == nil || ids == nil || interviews == nil || threads == nil || selections == nil || ieltsQuestions == nil || policies == nil {
		return nil, errors.New("preparation: plan dependencies are required")
	}
	return &PlanService{repository: repository, ids: ids, interviews: interviews, threads: threads, selections: selections, ielts: ieltsQuestions, policies: policies}, nil
}

func (s *PlanService) CreatePlan(ctx context.Context, actor requestcontext.Actor, clientRequestID string, request preparation.CreatePlanRequest) (preparation.PracticePlan, bool, error) {
	return s.createPlan(ctx, actor, clientRequestID, request, preparation.PlanStatusReady)
}

func (s *PlanService) PreviewPlan(ctx context.Context, actor requestcontext.Actor, clientRequestID string, request preparation.CreatePlanRequest) (preparation.PracticePlan, bool, error) {
	if request.SourceThreadID == "" {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	return s.createPlan(ctx, actor, clientRequestID, request, preparation.PlanStatusDraft)
}

func (s *PlanService) PreviewCustomPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	clientRequestID string,
	request preparation.CreateCustomPlanRequest,
) (preparation.PracticePlan, bool, error) {
	if ctx == nil || !actor.Valid() ||
		!validIdempotencyKey(clientRequestID) ||
		!validPlanAggregateID(request.SourceThreadID) ||
		!validPlanPreparationSnapshot(preparation.Snapshot{
			BackgroundSummary: request.BackgroundSummary,
		}) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	if err := s.validateSourceThread(ctx, actor, request.SourceThreadID); err != nil {
		return preparation.PracticePlan{}, false, err
	}
	id, err := s.newPlanID()
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	selection, err := scene.NewCustomSelection(id, request.SceneSpec)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	return s.persistResolvedPlan(
		ctx,
		actor,
		clientRequestID,
		request,
		id,
		request.SourceThreadID,
		preparation.Snapshot{BackgroundSummary: request.BackgroundSummary},
		selection,
		customPlanEffectiveTurns,
		nil,
		preparation.PlanStatusDraft,
	)
}

func (s *PlanService) createPlan(ctx context.Context, actor requestcontext.Actor, clientRequestID string, request preparation.CreatePlanRequest, status preparation.PlanStatus) (preparation.PracticePlan, bool, error) {
	if ctx == nil || !actor.Valid() || !validIdempotencyKey(clientRequestID) || !validCreatePlanRequest(request) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	if err := s.validateSourceThread(ctx, actor, request.SourceThreadID); err != nil {
		return preparation.PracticePlan{}, false, err
	}
	id, err := s.newPlanID()
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	preparationSnapshot := preparation.Snapshot{BackgroundSummary: request.BackgroundSummary}
	if request.InterviewPreparationID != "" {
		interview, err := s.interviews.ReadConfirmed(ctx, actor, request.InterviewPreparationID, request.ExpectedInterviewVersion)
		if err != nil {
			return preparation.PracticePlan{}, false, err
		}
		preparationSnapshot.Interview = &interview
	}
	selection, err := s.selections.ResolveAccessibleSelection(ctx, actor.UserID, request.SceneID, request.SceneVersion, clonePlanStrings(request.SelectedRoleIDs), request.PracticeOptionID)
	if err != nil {
		return preparation.PracticePlan{}, false, planSelectionError(err)
	}
	if !selectionMatchesCreateRequest(selection, request) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanConflict
	}
	selection, assignment, err := freezeIELTSAssignment(ctx, s.ielts, selection, request.IELTSSelection)
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	if err := freezeIELTSPreparedAnswers(assignment, request.IELTSPreparedAnswers); err != nil {
		return preparation.PracticePlan{}, false, err
	}
	return s.persistResolvedPlan(
		ctx,
		actor,
		clientRequestID,
		request,
		id,
		request.SourceThreadID,
		preparationSnapshot,
		selection,
		request.MaxEffectiveTurns,
		assignment,
		status,
	)
}

func (s *PlanService) newPlanID() (string, error) {
	id, err := s.ids.NewID()
	if err != nil || !validPlanAggregateID(id) {
		return "", preparation.ErrPlanRepository
	}
	return id, nil
}

func (s *PlanService) persistResolvedPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	clientRequestID string,
	fingerprintInput any,
	planID string,
	sourceThreadID string,
	preparationSnapshot preparation.Snapshot,
	selection scene.SelectionSnapshot,
	maxEffectiveTurns int,
	assignment *preparation.IELTSAssignmentSnapshot,
	status preparation.PlanStatus,
) (preparation.PracticePlan, bool, error) {
	policy, objectives, err := buildPlanExecution(
		s.policies,
		selection,
		maxEffectiveTurns,
	)
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	fingerprint, err := planRequestFingerprint(fingerprintInput)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	plan, replayed, err := s.repository.CreatePlan(ctx, actor, preparation.CreatePlanCommand{PlanID: planID, SourceThreadID: sourceThreadID, PreparationSnapshot: clonePlanPreparationSnapshot(preparationSnapshot), SceneSelection: clonePlanSceneSelection(selection), SessionPolicy: policy, PracticeObjectives: clonePlanObjectives(objectives), IELTSAssignment: cloneIELTSAssignment(assignment), Status: status, ClientRequestID: clientRequestID, RequestFingerprint: fingerprint})
	if err != nil {
		return preparation.PracticePlan{}, false, err
	}
	if !validReturnedPlan(plan, actor, plan.ID) {
		return preparation.PracticePlan{}, false, preparation.ErrPlanRepository
	}
	return clonePracticePlan(plan), replayed, nil
}

func (s *PlanService) ReadPlan(ctx context.Context, actor requestcontext.Actor, id string) (preparation.PracticePlan, error) {
	if ctx == nil || !actor.Valid() || !validPlanAggregateID(id) {
		return preparation.PracticePlan{}, preparation.ErrPlanNotFound
	}
	plan, err := s.repository.ReadCurrentPlan(ctx, actor, id)
	if err != nil {
		return preparation.PracticePlan{}, err
	}
	if !validReturnedPlan(plan, actor, id) {
		return preparation.PracticePlan{}, preparation.ErrPlanRepository
	}
	return clonePracticePlan(plan), nil
}

func (s *PlanService) ListPlans(ctx context.Context, actor requestcontext.Actor, experience scene.PracticeExperience) ([]preparation.PracticePlanSummary, error) {
	if ctx == nil || !actor.Valid() {
		return nil, preparation.ErrPlanInvalid
	}
	plans, err := s.repository.ListCurrentPlans(ctx, actor, experience)
	if err != nil {
		return nil, err
	}
	result := make([]preparation.PracticePlanSummary, 0, len(plans))
	for _, plan := range plans {
		if !validReturnedPlan(plan, actor, plan.ID) || plan.SceneSelection.Scene.Experience != experience {
			return nil, preparation.ErrPlanRepository
		}
		option, err := plan.SceneSelection.PracticeOption()
		if err != nil {
			return nil, preparation.ErrPlanRepository
		}
		objectives := make([]string, len(plan.PracticeObjectives))
		for i := range plan.PracticeObjectives {
			objectives[i] = plan.PracticeObjectives[i].Description
		}
		jobTitle, resumeUsed := "", false
		if plan.PreparationSnapshot.Interview != nil {
			jobTitle = plan.PreparationSnapshot.Interview.Candidate.JobTitle
			resumeUsed = plan.PreparationSnapshot.Interview.ResumeContent != nil
		}
		result = append(result, preparation.PracticePlanSummary{ID: plan.ID, Version: plan.Version, Status: plan.Status, PracticeExperience: experience, SceneName: plan.SceneSelection.Scene.Name, PracticeScope: option.DisplayName, JobTitle: jobTitle, PracticeObjectives: objectives, ResumeUsed: resumeUsed, SuggestedDurationSeconds: plan.SessionPolicy.SuggestedDurationSeconds, MinEffectiveTurns: plan.SessionPolicy.MinEffectiveTurns, MaxEffectiveTurns: plan.SessionPolicy.MaxEffectiveTurns, CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt})
	}
	return result, nil
}

func (s *PlanService) ArchivePlan(ctx context.Context, actor requestcontext.Actor, id string) error {
	if ctx == nil || !actor.Valid() || !validPlanAggregateID(id) {
		return preparation.ErrPlanNotFound
	}
	return s.repository.ArchivePlan(ctx, actor, id)
}

func (s *PlanService) ConfirmPlan(ctx context.Context, actor requestcontext.Actor, id, clientRequestID string, request preparation.ConfirmPlanRequest) (preparation.PracticePlan, bool, error) {
	if ctx == nil || !actor.Valid() || !validPlanAggregateID(id) || !validIdempotencyKey(clientRequestID) || request.ExpectedVersion < 1 {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	fingerprint, err := planRequestFingerprint(request)
	if err != nil {
		return preparation.PracticePlan{}, false, preparation.ErrPlanInvalid
	}
	return s.repository.ConfirmPlan(ctx, actor, preparation.ConfirmPlanCommand{PlanID: id, ExpectedVersion: request.ExpectedVersion, ClientRequestID: clientRequestID, RequestFingerprint: fingerprint})
}

func (s *PlanService) ReadExecutablePlan(ctx context.Context, actor requestcontext.Actor, id string, version int) (preparation.PracticePlan, error) {
	if ctx == nil || !actor.Valid() || !validPlanAggregateID(id) || version < 1 {
		return preparation.PracticePlan{}, preparation.ErrPlanNotFound
	}
	plan, err := s.repository.ReadExecutablePlan(ctx, actor, id, version)
	if err != nil {
		return preparation.PracticePlan{}, err
	}
	if !validReturnedPlan(plan, actor, id) || plan.Status != preparation.PlanStatusReady || plan.Version != version {
		return preparation.PracticePlan{}, preparation.ErrPlanRepository
	}
	return clonePracticePlan(plan), nil
}

func (s *PlanService) validateSourceThread(ctx context.Context, actor requestcontext.Actor, id string) error {
	if id == "" {
		return nil
	}
	thread, err := s.threads.ReadOwnedThread(ctx, actor, id)
	if err != nil {
		return err
	}
	if thread.ID != id {
		return preparation.ErrPlanConflict
	}
	return nil
}

func validCreatePlanRequest(request preparation.CreatePlanRequest) bool {
	interview := request.InterviewPreparationID != "" || request.ExpectedInterviewVersion != 0
	return (request.SourceThreadID == "" || validPlanAggregateID(request.SourceThreadID)) &&
		(!interview || (validPlanAggregateID(request.InterviewPreparationID) && request.ExpectedInterviewVersion > 0)) &&
		validPlanResourceID(request.SceneID) && request.SceneVersion > 0 && validUniquePlanIDs(request.SelectedRoleIDs) &&
		validPlanResourceID(request.PracticeOptionID) && request.MaxEffectiveTurns >= 0 && request.MaxEffectiveTurns <= maxPlanEffectiveTurns &&
		len(request.BackgroundSummary) <= 64*1024 && strings.TrimSpace(request.BackgroundSummary) == request.BackgroundSummary && !strings.ContainsRune(request.BackgroundSummary, '\x00') &&
		(request.IELTSSelection == nil || validIELTSSelectionShape(*request.IELTSSelection)) &&
		len(request.IELTSPreparedAnswers) <= maxPlanEffectiveTurns
}

func freezeIELTSPreparedAnswers(
	assignment *preparation.IELTSAssignmentSnapshot,
	answers []preparation.IELTSPreparedAnswerRequest,
) error {
	if len(answers) == 0 {
		return nil
	}
	if assignment == nil {
		return preparation.ErrPlanInvalid
	}
	seen := make(map[string]struct{}, len(answers))
	for _, answer := range answers {
		if answer.BankID != assignment.BankID ||
			!validPlanResourceID(answer.SourceID) ||
			answer.QuestionPosition < 1 ||
			!validPreparedAnswer(answer.Answer) {
			return preparation.ErrPlanInvalid
		}
		partIndex := -1
		for index := range assignment.Parts {
			part := assignment.Parts[index]
			if part.Part == answer.Part && part.SourceID == answer.SourceID {
				partIndex = index
				break
			}
		}
		if partIndex < 0 || answer.QuestionPosition > len(assignment.Parts[partIndex].TurnBlueprints) {
			return preparation.ErrPlanInvalid
		}
		key := fmt.Sprintf("%s:%s:%d", answer.Part, answer.SourceID, answer.QuestionPosition)
		if _, duplicate := seen[key]; duplicate {
			return preparation.ErrPlanInvalid
		}
		seen[key] = struct{}{}
		assignment.Parts[partIndex].PreparedAnswers = append(
			assignment.Parts[partIndex].PreparedAnswers,
			preparation.IELTSPreparedAnswerSnapshot{
				QuestionPosition: answer.QuestionPosition,
				Answer:           answer.Answer,
				Personalized:     answer.Personalized,
			},
		)
	}
	return nil
}

func validPreparedAnswer(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		utf8.ValidString(value) && utf8.RuneCountInString(value) <= 2000 &&
		!strings.ContainsRune(value, '\x00')
}

func ValidCreatePlanRequest(request preparation.CreatePlanRequest) bool {
	return validCreatePlanRequest(request)
}

func planRequestFingerprint(value any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func validPlanPreparationSnapshot(snapshot preparation.Snapshot) bool {
	if len(snapshot.BackgroundSummary) > 64*1024 ||
		strings.TrimSpace(snapshot.BackgroundSummary) != snapshot.BackgroundSummary ||
		strings.ContainsRune(snapshot.BackgroundSummary, '\x00') {
		return false
	}
	if snapshot.Interview == nil {
		return true
	}
	interview := snapshot.Interview
	if !validPlanAggregateID(interview.ID) || interview.Version < 1 ||
		!preparation.ValidJobTargetInput(interview.Input) ||
		!preparation.ValidJobTargetCandidateShape(interview.Candidate, interview.Input.Source) {
		return false
	}
	return interview.ResumeContent == nil || preparation.ValidResumeMaterial(*interview.ResumeContent)
}

func selectionMatchesCreateRequest(
	selection scene.SelectionSnapshot,
	request preparation.CreatePlanRequest,
) bool {
	if selection.Source.Type != scene.SceneSourceCatalog ||
		selection.Source.SceneID != request.SceneID ||
		selection.Source.SceneVersion != request.SceneVersion ||
		selection.Scene.Key != request.SceneID ||
		selection.Scene.Revision != request.SceneVersion ||
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
	ctx context.Context,
	questions ielts.QuestionSetResolver,
	selection scene.SelectionSnapshot,
	request *preparation.IELTSQuestionSelection,
) (scene.SelectionSnapshot, *preparation.IELTSAssignmentSnapshot, error) {
	option, err := selection.PracticeOption()
	if err != nil {
		return scene.SelectionSnapshot{}, nil, preparation.ErrPlanConflict
	}
	isIELTS := selection.Scene.Experience ==
		scene.PracticeExperienceIELTSSpeaking
	if !isIELTS {
		if request != nil {
			return scene.SelectionSnapshot{}, nil, preparation.ErrPlanInvalid
		}
		return clonePlanSceneSelection(selection), nil, nil
	}
	mode, validMode := ieltsPracticeMode(option.Mode)
	if selection.Scene.Category != scene.SceneCategoryIELTSSpeaking ||
		!validMode {
		return scene.SelectionSnapshot{}, nil, preparation.ErrPlanInvalid
	}
	var resolved ielts.ResolvedQuestionSet
	var effectiveSelection preparation.IELTSQuestionSelection
	if request == nil {
		resolved, err = questions.AssignQuestionSet(ctx, mode, "")
		if err == nil {
			effectiveSelection = ieltsSelectionFromResolved(resolved)
		}
	} else if request.CueCardType != "" {
		if option.Mode == scene.PracticeModeFullMock ||
			!validIELTSQuestionSelection(option.Mode, *request) {
			return scene.SelectionSnapshot{}, nil, preparation.ErrPlanInvalid
		}
		resolved, err = questions.AssignQuestionSet(
			ctx,
			mode,
			request.CueCardType,
		)
		if err == nil {
			effectiveSelection = ieltsSelectionFromResolved(resolved)
		}
	} else {
		if !validIELTSQuestionSelection(option.Mode, *request) {
			return scene.SelectionSnapshot{}, nil, preparation.ErrPlanInvalid
		}
		effectiveSelection = *request
		resolved, err = questions.ResolveQuestionSet(
			ctx,
			ielts.QuestionSetSelection{
				Mode:         mode,
				Part1SetID:   request.Part1SetID,
				TopicGroupID: request.TopicGroupID,
			},
		)
	}
	if err != nil {
		switch {
		case errors.Is(err, ielts.ErrQuestionSetNotFound):
			return scene.SelectionSnapshot{}, nil, preparation.ErrPlanNotFound
		case errors.Is(err, ielts.ErrPracticeModeInvalid):
			return scene.SelectionSnapshot{}, nil, preparation.ErrPlanInvalid
		default:
			return scene.SelectionSnapshot{}, nil, err
		}
	}
	if !validResolvedIELTSQuestionSet(option.Mode, effectiveSelection, resolved) {
		return scene.SelectionSnapshot{}, nil, preparation.ErrPlanConflict
	}

	assignment := &preparation.IELTSAssignmentSnapshot{
		BankID: resolved.BankID,
		Season: resolved.Season,
		Mode:   option.Mode,
		Parts:  make([]preparation.IELTSAssignmentPartSnapshot, len(resolved.Parts)),
	}
	for index, part := range resolved.Parts {
		snapshot := preparation.IELTSAssignmentPartSnapshot{
			Part:           scene.PracticeMode(part.Part),
			SourceID:       part.SourceID,
			CueCard:        part.CueCard,
			TurnBlueprints: clonePlanStrings(part.TurnBlueprints),
		}
		if part.Part != ielts.PracticeModePart1 {
			snapshot.TopicTitle = part.TopicTitle
		}
		assignment.Parts[index] = snapshot
	}
	selection = clonePlanSceneSelection(selection)
	selection.Scene.Prompt.TurnBlueprints = clonePlanStrings(
		ieltsAssignmentTurnBlueprints(*assignment),
	)
	selection.Scene.Prompt.PublicSceneBrief = ieltsAssignmentSceneBrief(
		*assignment,
	)
	if !validPlanIELTSAssignment(selection, assignment) {
		return scene.SelectionSnapshot{}, nil, preparation.ErrPlanConflict
	}
	return selection, assignment, nil
}

func ieltsSelectionFromResolved(
	resolved ielts.ResolvedQuestionSet,
) preparation.IELTSQuestionSelection {
	var selection preparation.IELTSQuestionSelection
	for _, part := range resolved.Parts {
		switch part.Part {
		case ielts.PracticeModePart1:
			selection.Part1SetID = part.SourceID
		case ielts.PracticeModePart2, ielts.PracticeModePart3:
			selection.TopicGroupID = part.SourceID
		}
	}
	return selection
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
	selection preparation.IELTSQuestionSelection,
) bool {
	if selection.CueCardType != "" {
		switch mode {
		case scene.PracticeModePart1,
			scene.PracticeModePart2,
			scene.PracticeModePart3:
			return validIELTSCueCardType(selection.CueCardType) &&
				selection.Part1SetID == "" && selection.TopicGroupID == ""
		default:
			return false
		}
	}
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

func validIELTSSelectionShape(selection preparation.IELTSQuestionSelection) bool {
	if selection.CueCardType != "" {
		return validIELTSCueCardType(selection.CueCardType) &&
			selection.Part1SetID == "" && selection.TopicGroupID == ""
	}
	part1Valid := selection.Part1SetID == "" ||
		validPlanResourceID(selection.Part1SetID)
	topicValid := selection.TopicGroupID == "" ||
		validPlanResourceID(selection.TopicGroupID)
	return part1Valid && topicValid &&
		(selection.Part1SetID != "" || selection.TopicGroupID != "")
}

func validIELTSExactSelectionShape(selection preparation.IELTSQuestionSelection) bool {
	return selection.CueCardType == "" && validIELTSSelectionShape(selection)
}

func validIELTSCueCardType(value string) bool {
	return value == "person" || value == "place" || value == "thing" ||
		value == "experience"
}

func validResolvedIELTSQuestionSet(
	mode scene.PracticeMode,
	request preparation.IELTSQuestionSelection,
	resolved ielts.ResolvedQuestionSet,
) bool {
	if scene.PracticeMode(resolved.Mode) != mode ||
		!validPlanResourceID(resolved.BankID) ||
		!validPlanText(resolved.Season) {
		return false
	}
	parts := make([]preparation.IELTSAssignmentPartSnapshot, len(resolved.Parts))
	for index, part := range resolved.Parts {
		snapshot := preparation.IELTSAssignmentPartSnapshot{
			Part:           scene.PracticeMode(part.Part),
			SourceID:       part.SourceID,
			CueCard:        part.CueCard,
			TurnBlueprints: part.TurnBlueprints,
		}
		if part.Part != ielts.PracticeModePart1 {
			snapshot.TopicTitle = part.TopicTitle
		}
		parts[index] = snapshot
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
	assignment *preparation.IELTSAssignmentSnapshot,
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

func ValidPlanIELTSAssignment(
	selection scene.SelectionSnapshot,
	assignment *preparation.IELTSAssignmentSnapshot,
) bool {
	return validPlanIELTSAssignment(selection, assignment)
}

func validIELTSAssignmentParts(
	mode scene.PracticeMode,
	parts []preparation.IELTSAssignmentPartSnapshot,
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
		seenAnswers := make(map[int]struct{}, len(part.PreparedAnswers))
		for _, answer := range part.PreparedAnswers {
			if answer.QuestionPosition < 1 ||
				answer.QuestionPosition > len(part.TurnBlueprints) ||
				!validPreparedAnswer(answer.Answer) {
				return false
			}
			if _, duplicate := seenAnswers[answer.QuestionPosition]; duplicate {
				return false
			}
			seenAnswers[answer.QuestionPosition] = struct{}{}
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
	assignment preparation.IELTSAssignmentSnapshot,
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

func ieltsAssignmentSceneBrief(assignment preparation.IELTSAssignmentSnapshot) string {
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
		return "完成冻结的 Part 1 熟悉话题问答。"
	case scene.PracticeModePart2:
		return "完成“" + topicTitle + "”题卡，并可继续同主题 Part 3。"
	case scene.PracticeModePart3:
		return "围绕“" + topicTitle + "”完成同主题 Part 3 讨论。"
	default:
		return ""
	}
}

func validSelectedPlanOption(
	selection scene.SelectionSnapshot,
	roles []scene.RoleSnapshot,
	option scene.PracticeOptionSnapshot,
) bool {
	if option.ID != selection.PracticeOptionID ||
		option.SceneKey != selection.Scene.Key {
		return false
	}
	for index, role := range roles {
		if role.ID != selection.SelectedRoleIDs[index] ||
			role.SceneKey != selection.Scene.Key {
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

func ValidSelectedPlanOption(
	selection scene.SelectionSnapshot,
	roles []scene.RoleSnapshot,
	option scene.PracticeOptionSnapshot,
) bool {
	return validSelectedPlanOption(selection, roles, option)
}

func buildPlanExecution(
	policies preparation.PolicyResolver,
	selection scene.SelectionSnapshot,
	maxEffectiveTurns int,
) (preparation.SessionPolicy, []preparation.PracticeObjective, error) {
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return preparation.SessionPolicy{}, nil, preparation.ErrPlanInvalid
	}
	option, err := selection.PracticeOption()
	if err != nil || !validSelectedPlanOption(selection, roles, option) {
		return preparation.SessionPolicy{}, nil, preparation.ErrPlanInvalid
	}
	policy, err := policies.ResolveSessionPolicy(
		selection.Scene,
		option,
		maxEffectiveTurns,
	)
	if err != nil {
		return preparation.SessionPolicy{}, nil, err
	}
	objectives, err := practiceObjectives(roles)
	if err != nil {
		return preparation.SessionPolicy{}, nil, err
	}
	if !validPracticeObjectives(objectives) {
		return preparation.SessionPolicy{}, nil, preparation.ErrPlanInvalid
	}
	return policy, objectives, nil
}

func practiceObjectives(
	roles []scene.RoleSnapshot,
) ([]preparation.PracticeObjective, error) {
	seen := make(map[string]string)
	objectives := make([]preparation.PracticeObjective, 0)
	for _, role := range roles {
		for _, definition := range role.PracticeObjectives {
			if description, exists := seen[definition.ID]; exists {
				if description != definition.Description {
					return nil, preparation.ErrPlanInvalid
				}
				continue
			}
			seen[definition.ID] = definition.Description
			objectives = append(objectives, preparation.PracticeObjective{
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
		return preparation.ErrPlanNotFound
	case errors.Is(err, scene.ErrRoleDefinitionNotFound),
		errors.Is(err, scene.ErrPracticeOptionNotFound),
		errors.Is(err, scene.ErrCatalogSelectionInvalid),
		errors.Is(err, scene.ErrCatalogDefinitionInvalid):
		return preparation.ErrPlanInvalid
	default:
		return err
	}
}

func validReturnedPlan(
	plan preparation.PracticePlan,
	actor requestcontext.Actor,
	expectedID string,
) bool {
	if plan.ID != expectedID || !validPlanAggregateID(plan.ID) ||
		plan.UserID != actor.UserID {
		return false
	}
	if plan.Version < 1 ||
		(plan.Status != preparation.PlanStatusReady && plan.Status != preparation.PlanStatusDraft && plan.Status != preparation.PlanStatusArchived) ||
		(plan.SourceThreadID != "" &&
			!validPlanAggregateID(plan.SourceThreadID)) ||
		plan.CreatedAt.IsZero() || plan.UpdatedAt.Before(plan.CreatedAt) ||
		!validPlanPreparationSnapshot(plan.PreparationSnapshot) {
		return false
	}
	if !scene.ValidSelectionSnapshot(plan.SceneSelection) ||
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

func ValidReturnedPlan(
	plan preparation.PracticePlan,
	actor requestcontext.Actor,
	expectedID string,
) bool {
	return validReturnedPlan(plan, actor, expectedID)
}

func validPracticeObjectives(objectives []preparation.PracticeObjective) bool {
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

func ValidPracticeObjectives(objectives []preparation.PracticeObjective) bool {
	return validPracticeObjectives(objectives)
}

func validStoredSessionPolicy(policy preparation.SessionPolicy) bool {
	completionMode := policy.CompletionMode
	if policy.SuggestedDurationSeconds < 1 ||
		policy.MinEffectiveTurns < 1 ||
		policy.CoverageCheckpointTurn < 1 ||
		policy.MaxFollowUpsPerQuestion < 0 ||
		policy.EarlyCompletionRule !=
			preparation.EarlyCompletionCoverageSatisfiedAfterCheckpoint {
		return false
	}
	if completionMode == preparation.CompletionModeUserControlled {
		return policy.MaxEffectiveTurns == 0 &&
			policy.CoverageCheckpointTurn == 1
	}
	return completionMode == preparation.CompletionModeTurnLimited &&
		policy.MaxEffectiveTurns >= policy.MinEffectiveTurns &&
		policy.MaxEffectiveTurns <= maxPlanEffectiveTurns &&
		policy.CoverageCheckpointTurn <= policy.MaxEffectiveTurns &&
		policy.MaxEffectiveTurns > 0
}

func ValidStoredSessionPolicy(policy preparation.SessionPolicy) bool {
	return validStoredSessionPolicy(policy)
}

func validPlanResourceID(value string) bool {
	return utf8.ValidString(value) && value != "" && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func ValidPlanResourceID(value string) bool {
	return validPlanResourceID(value)
}

func validPlanAggregateID(value string) bool {
	return preparation.ValidAggregateID(value)
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

func ValidUniquePlanIDs(values []string) bool {
	return validUniquePlanIDs(values)
}

func validPlanText(value string) bool {
	return utf8.ValidString(value) && value != "" &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func ValidPlanText(value string) bool {
	return validPlanText(value)
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

func clonePlanPreparationSnapshot(source preparation.Snapshot) preparation.Snapshot {
	result := source
	if source.Interview != nil {
		interview := *source.Interview
		interview.Candidate = preparation.CloneJobTargetCandidate(source.Interview.Candidate)
		if source.Interview.ResumeContent != nil {
			resume := preparation.CloneResumeMaterial(*source.Interview.ResumeContent)
			interview.ResumeContent = &resume
		}
		result.Interview = &interview
	}
	return result
}

func ClonePlanPreparationSnapshot(source preparation.Snapshot) preparation.Snapshot {
	return clonePlanPreparationSnapshot(source)
}

func clonePlanSceneSelection(
	source scene.SelectionSnapshot,
) scene.SelectionSnapshot {
	return scene.SelectionSnapshot{
		Source:           source.Source,
		Scene:            clonePlanSceneDefinition(source.Scene),
		SelectedRoleIDs:  clonePlanStrings(source.SelectedRoleIDs),
		PracticeOptionID: source.PracticeOptionID,
	}
}

func clonePlanSceneDefinition(
	source scene.ExecutableSceneSnapshot,
) scene.ExecutableSceneSnapshot {
	result := source
	result.Prompt.FocusAreas = clonePlanStrings(source.Prompt.FocusAreas)
	result.Prompt.TurnBlueprints = clonePlanStrings(
		source.Prompt.TurnBlueprints,
	)
	result.Roles = make([]scene.RoleSnapshot, len(source.Roles))
	for index, role := range source.Roles {
		result.Roles[index] = role
		result.Roles[index].PracticeObjectives = append(
			[]scene.PracticeObjectiveDefinition(nil),
			role.PracticeObjectives...,
		)
	}
	result.PracticeOptions = append(
		[]scene.PracticeOptionSnapshot(nil),
		source.PracticeOptions...,
	)
	return result
}

func clonePlanObjectives(source []preparation.PracticeObjective) []preparation.PracticeObjective {
	return append([]preparation.PracticeObjective(nil), source...)
}

func ClonePlanObjectives(source []preparation.PracticeObjective) []preparation.PracticeObjective {
	return clonePlanObjectives(source)
}

func cloneIELTSAssignment(
	source *preparation.IELTSAssignmentSnapshot,
) *preparation.IELTSAssignmentSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	result.Parts = make([]preparation.IELTSAssignmentPartSnapshot, len(source.Parts))
	for index, part := range source.Parts {
		result.Parts[index] = part
		result.Parts[index].TurnBlueprints = clonePlanStrings(
			part.TurnBlueprints,
		)
		result.Parts[index].PreparedAnswers = append(
			[]preparation.IELTSPreparedAnswerSnapshot(nil),
			part.PreparedAnswers...,
		)
	}
	return &result
}

func CloneIELTSAssignment(
	source *preparation.IELTSAssignmentSnapshot,
) *preparation.IELTSAssignmentSnapshot {
	return cloneIELTSAssignment(source)
}

func clonePracticePlan(source preparation.PracticePlan) preparation.PracticePlan {
	result := source
	result.PreparationSnapshot = clonePlanPreparationSnapshot(
		source.PreparationSnapshot,
	)
	result.SceneSelection = clonePlanSceneSelection(source.SceneSelection)
	result.PracticeObjectives = clonePlanObjectives(source.PracticeObjectives)
	result.IELTSAssignment = cloneIELTSAssignment(source.IELTSAssignment)
	return result
}

func ClonePracticePlan(source preparation.PracticePlan) preparation.PracticePlan {
	return clonePracticePlan(source)
}

func clonePlanStrings(values []string) []string {
	return append([]string(nil), values...)
}
