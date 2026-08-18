package agentcapability

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type ServicePort struct {
	plans          PlanApplication
	catalog        scene.PreviewCatalog
	messages       TrustedMessageReader
	pendingActions preparation.PendingActionRepository
	turnIntents    *PracticeTurnIntentResolver
	manifest       PreviewCatalogManifest
}

type TrustedMessageReader interface {
	FindMessage(context.Context, string, string, string) (agentconversation.Message, error)
}

type PlanApplication interface {
	PreviewPlan(
		context.Context,
		requestcontext.Actor,
		string,
		preparation.CreatePlanRequest,
	) (preparation.PracticePlan, bool, error)
	PreviewCustomPlan(
		context.Context,
		requestcontext.Actor,
		string,
		preparation.CreateCustomPlanRequest,
	) (preparation.PracticePlan, bool, error)
}

func NewServicePort(
	ctx context.Context,
	plans PlanApplication,
	catalog scene.PreviewCatalog,
	messages TrustedMessageReader,
	pendingActions preparation.PendingActionRepository,
	turnIntentGenerator PracticeTurnIntentGenerator,
) (*ServicePort, error) {
	if ctx == nil || plans == nil || catalog == nil || messages == nil ||
		pendingActions == nil || turnIntentGenerator == nil {
		return nil, errors.New(
			"preparation agent capability: preview dependencies are required",
		)
	}
	source, err := catalog.PreviewCatalogManifest(ctx)
	if err != nil {
		return nil, err
	}
	manifest, err := NewPreviewCatalogManifest(source)
	if err != nil {
		return nil, err
	}
	turnIntents, err := NewPracticeTurnIntentResolver(turnIntentGenerator)
	if err != nil {
		return nil, err
	}
	return &ServicePort{
		plans: plans, catalog: catalog, messages: messages,
		pendingActions: pendingActions, turnIntents: turnIntents,
		manifest: manifest,
	}, nil
}

func (port *ServicePort) AuthorizePracticeTurn(
	ctx context.Context,
	request capability.ExposureRequest,
) (PracticeTurnIntent, error) {
	if port == nil || port.messages == nil || port.pendingActions == nil ||
		port.turnIntents == nil || ctx == nil || !request.Actor.Valid() ||
		request.ThreadID == "" || request.RunID == "" ||
		request.InputMessageID == "" {
		return "", capability.ErrExecutionRejected
	}
	message, err := port.messages.FindMessage(
		ctx,
		request.Actor.UserID,
		request.ThreadID,
		request.InputMessageID,
	)
	if err != nil || message.Role != agentconversation.MessageRoleUser ||
		message.ID != request.InputMessageID ||
		message.ThreadID != request.ThreadID ||
		message.OwnerID != request.Actor.UserID || message.Sequence < 1 {
		return "", capability.ErrExecutionRejected
	}
	pendingAvailable := false
	if message.Sequence >= 3 {
		pendingAvailable, err = port.pendingActions.HasOpenForReply(
			ctx, request.Actor, request.ThreadID, message.ID, message.Sequence,
		)
		if err != nil {
			return "", mapPreparationToolError(err)
		}
	}
	intent, err := port.turnIntents.Resolve(
		ctx, message.Content, pendingAvailable,
	)
	if err != nil {
		return "", err
	}
	return intent, nil
}

// NewPreviewCatalogManifest validates and freezes the trusted Scene manifest
// into the exact model-facing subset used by production and test composition.
func NewPreviewCatalogManifest(
	source scene.CatalogManifest,
) (PreviewCatalogManifest, error) {
	if !source.Valid() {
		return PreviewCatalogManifest{}, errors.New(
			"preparation agent capability: preview catalog manifest is invalid",
		)
	}
	experiences := make(
		[]PreviewCatalogManifestExperience,
		len(source.Experiences),
	)
	for index, item := range source.Experiences {
		experiences[index] = PreviewCatalogManifestExperience{
			PracticeExperience:      string(item.Experience),
			Aliases:                 append([]string(nil), item.Aliases...),
			DefaultSceneID:          item.DefaultSceneID,
			DefaultPracticeOptionID: item.DefaultPracticeOptionID,
		}
	}
	sort.Slice(experiences, func(left, right int) bool {
		return experiences[left].PracticeExperience <
			experiences[right].PracticeExperience
	})
	items := make([]PreviewCatalogManifestScene, len(source.Scenes))
	for index, item := range source.Scenes {
		items[index] = PreviewCatalogManifestScene{
			SceneID:            item.SceneID,
			Name:               item.Name,
			PracticeExperience: string(item.PracticeExperience),
			Aliases:            append([]string(nil), item.Aliases...),
			PublicSceneBrief:   item.PublicSceneBrief,
			PracticeGoal:       item.PracticeGoal,
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].SceneID < items[right].SceneID
	})
	manifest := PreviewCatalogManifest{
		Experiences: experiences,
		Scenes:      items,
	}
	if !validPreviewCatalogManifest(manifest) {
		return PreviewCatalogManifest{}, errors.New(
			"preparation agent capability: preview catalog manifest is invalid",
		)
	}
	return manifest, nil
}

func (port *ServicePort) PreviewCatalogManifest() PreviewCatalogManifest {
	if port == nil {
		return PreviewCatalogManifest{}
	}
	return clonePreviewCatalogManifest(port.manifest)
}

func (port *ServicePort) PreviewPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	if port == nil || port.plans == nil || port.catalog == nil ||
		port.messages == nil || port.pendingActions == nil || port.turnIntents == nil ||
		ctx == nil || !call.Actor.Valid() ||
		call.ThreadID == "" || call.RunID == "" || call.InputMessageID == "" ||
		call.RequestID == "" {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	if !validPreviewInputShape(input) {
		return PreviewResult{}, capability.ErrInvalidInput
	}
	message, err := port.messages.FindMessage(
		ctx, call.Actor.UserID, call.ThreadID, call.InputMessageID,
	)
	if err != nil || message.Role != agentconversation.MessageRoleUser ||
		message.ID != call.InputMessageID || message.ThreadID != call.ThreadID ||
		message.OwnerID != call.Actor.UserID || message.Sequence < 1 {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	input.SceneQuery = message.Content
	input.InputSequence = message.Sequence
	switch input.ActionIntent {
	case PracticeTurnIntentProposeCreate:
		return port.previewPendingAction(ctx, call, input)
	case PracticeTurnIntentConfirmPending:
		return port.resolvePendingAction(ctx, call, input, true)
	case PracticeTurnIntentRejectPending:
		return port.resolvePendingAction(ctx, call, input, false)
	case PracticeTurnIntentRequestCreate:
		return port.previewRequestedPractice(ctx, call, input)
	default:
		return PreviewResult{}, capability.ErrInvalidInput
	}
}

func (port *ServicePort) previewRequestedPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	switch input.SceneResolution.Kind {
	case SceneResolutionKindCatalog:
		return port.previewCatalogPractice(ctx, call, input)
	case SceneResolutionKindCustom:
		return port.previewCustomPractice(ctx, call, input)
	case SceneResolutionKindNeedsClarification:
		return port.previewClarification(ctx, input.SceneResolution.CandidateSceneIDs)
	default:
		return PreviewResult{}, capability.ErrInvalidInput
	}
}

type pendingPracticeProposal struct {
	SceneQuery        string               `json:"scene_query"`
	SceneResolution   SceneResolutionInput `json:"scene_resolution"`
	SceneIntent       *SceneIntent         `json:"scene_intent,omitempty"`
	BackgroundSummary string               `json:"background_summary,omitempty"`
	IELTSPracticeMode string               `json:"ielts_practice_mode,omitempty"`
	IELTSTopicChoice  string               `json:"ielts_topic_choice,omitempty"`
}

func (port *ServicePort) previewPendingAction(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	name, blocking, err := port.validatePendingProposal(ctx, input)
	if err != nil || blocking != nil {
		if blocking != nil {
			return *blocking, nil
		}
		return PreviewResult{}, err
	}
	proposal := pendingPracticeProposal{
		SceneQuery: input.SceneQuery, SceneResolution: input.SceneResolution,
		SceneIntent: input.SceneIntent, BackgroundSummary: input.BackgroundSummary,
		IELTSPracticeMode: input.IELTSPracticeMode, IELTSTopicChoice: input.IELTSTopicChoice,
	}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	_, replayed, err := port.pendingActions.CreateOrReplay(
		ctx, call.Actor, preparation.CreatePendingActionCommand{
			ThreadID: call.ThreadID, SourceRunID: call.RunID,
			SourceInputMessageID: call.InputMessageID,
			SourceInputSequence:  input.InputSequence, Proposal: encoded,
			ProposalFingerprint: sha256.Sum256(encoded),
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	return PreviewResult{
		Status: PreviewOutcomeActionPending, SceneResolution: SceneResolutionNotRequested,
		Replayed: replayed, AssistantText: "你是想现在创建“" + name + "”练习吗？",
	}, nil
}

func (port *ServicePort) resolvePendingAction(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
	confirm bool,
) (PreviewResult, error) {
	pending, replayed, err := port.pendingActions.ClaimForReply(
		ctx, call.Actor, preparation.ResolvePendingActionCommand{
			ThreadID: call.ThreadID, ResolutionInputMessageID: call.InputMessageID,
			ResolutionInputSequence: input.InputSequence, Confirm: confirm,
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	if !confirm {
		return PreviewResult{
			Status: PreviewOutcomeActionDeclined, SceneResolution: SceneResolutionNotRequested,
			Replayed: replayed, AssistantText: "好的，这次先不创建练习。",
		}, nil
	}
	proposal, err := decodePendingPracticeProposal(pending.Proposal)
	if err != nil {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	request := PreviewInput{
		ActionIntent: PracticeTurnIntentRequestCreate,
		SceneQuery:   proposal.SceneQuery, InputSequence: pending.SourceInputSequence,
		SceneResolution: proposal.SceneResolution, SceneIntent: proposal.SceneIntent,
		BackgroundSummary: proposal.BackgroundSummary,
		IELTSPracticeMode: proposal.IELTSPracticeMode, IELTSTopicChoice: proposal.IELTSTopicChoice,
	}
	resolvedCall := call
	resolvedCall.RequestID = "pending:" + pending.ID
	result, err := port.previewRequestedPractice(ctx, resolvedCall, request)
	if err != nil {
		return PreviewResult{}, err
	}
	if result.Status != PreviewOutcomeReady {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	if _, err = port.pendingActions.CompleteConfirmation(
		ctx, call.Actor, pending.ID, call.InputMessageID, result.PlanID,
	); err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	result.Replayed = result.Replayed || replayed
	return result, nil
}

func decodePendingPracticeProposal(raw []byte) (pendingPracticeProposal, error) {
	var proposal pendingPracticeProposal
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return pendingPracticeProposal{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return pendingPracticeProposal{}, capability.ErrInvalidInput
	}
	input := PreviewInput{
		ActionIntent: PracticeTurnIntentRequestCreate,
		SceneQuery:   proposal.SceneQuery, SceneResolution: proposal.SceneResolution,
		SceneIntent: proposal.SceneIntent, BackgroundSummary: proposal.BackgroundSummary,
		IELTSPracticeMode: proposal.IELTSPracticeMode, IELTSTopicChoice: proposal.IELTSTopicChoice,
	}
	if !agentconversation.ValidMessageContent(proposal.SceneQuery) ||
		!validPreviewInputShape(input) {
		return pendingPracticeProposal{}, capability.ErrInvalidInput
	}
	return proposal, nil
}

func (port *ServicePort) validatePendingProposal(
	ctx context.Context,
	input PreviewInput,
) (string, *PreviewResult, error) {
	switch input.SceneResolution.Kind {
	case SceneResolutionKindCatalog:
		selection, err := port.catalog.ResolvePreviewCatalogSelection(
			ctx, input.SceneResolution.CatalogSceneID,
		)
		if err != nil {
			return "", nil, mapPreparationToolError(err)
		}
		if !selection.Valid() || selection.Scene.ID != input.SceneResolution.CatalogSceneID {
			return "", nil, capability.ErrExecutionRejected
		}
		candidate := previewCatalogCandidate(selection)
		_, details := catalogPreviewOption(input, candidate)
		if len(details) > 0 {
			result := PreviewResult{
				Status: PreviewOutcomeNeedsDetails, SceneResolution: SceneResolutionNeedsDetails,
				CatalogCandidateCount: 1, RequiredMissingFields: details,
				Candidates: []CatalogCandidate{candidate}, AssistantText: catalogDetailsQuestion(details),
			}
			return "", &result, nil
		}
		return candidate.Name, nil, nil
	case SceneResolutionKindCustom:
		result, err := validateCustomProposal(input)
		if err != nil || result != nil {
			return "", result, err
		}
		return strings.TrimSpace(input.SceneIntent.Scenario), nil, nil
	default:
		return "", nil, capability.ErrInvalidInput
	}
}

func (port *ServicePort) previewCatalogPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	selection, err := port.catalog.ResolvePreviewCatalogSelection(
		ctx,
		input.SceneResolution.CatalogSceneID,
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	if !selection.Valid() ||
		selection.Scene.ID != input.SceneResolution.CatalogSceneID {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	candidate := previewCatalogCandidate(selection)
	optionID, details := catalogPreviewOption(input, candidate)
	if len(details) > 0 {
		return PreviewResult{
			Status:                PreviewOutcomeNeedsDetails,
			SceneResolution:       SceneResolutionNeedsDetails,
			CatalogCandidateCount: 1,
			RequiredMissingFields: details,
			Candidates:            []CatalogCandidate{candidate},
			AssistantText:         catalogDetailsQuestion(details),
		}, nil
	}
	plan, replayed, err := port.plans.PreviewPlan(
		ctx,
		call.Actor,
		call.RequestID,
		preparation.CreatePlanRequest{
			SourceThreadID:    call.ThreadID,
			BackgroundSummary: previewBackgroundSummary(input),
			SceneID:           selection.Scene.ID,
			SceneVersion:      selection.Scene.Version,
			SelectedRoleIDs:   append([]string(nil), selection.DefaultRoleIDs...),
			PracticeOptionID:  optionID,
			// The referenced Scene session policy owns completion and turn limits.
			MaxEffectiveTurns: 0,
			IELTSSelection:    previewIELTSQuestionSelection(input),
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	result, err := readyPreviewResult(plan, replayed, PreviewPlanSourceCatalog)
	result.CatalogCandidateCount = 1
	return result, err
}

func catalogPreviewOption(
	input PreviewInput,
	candidate CatalogCandidate,
) (string, []string) {
	if candidate.PracticeExperience !=
		string(scene.PracticeExperienceIELTSSpeaking) {
		if input.IELTSPracticeMode != "" || input.IELTSTopicChoice != "" {
			return "", []string{"ielts_practice_mode"}
		}
		return candidate.DefaultPracticeOptionID, nil
	}
	mode := scene.PracticeMode(input.IELTSPracticeMode)
	if mode == "" {
		mode = scene.PracticeModeFullMock
	}
	optionID, found := previewOptionForMode(candidate, mode)
	if !found {
		return "", []string{"ielts_practice_mode"}
	}
	if !validPreviewIELTSTopicChoice(mode, input.IELTSTopicChoice) {
		return "", []string{"ielts_topic_choice"}
	}
	return optionID, nil
}

func catalogDetailsQuestion(missing []string) string {
	if containsMissingField(missing, "ielts_practice_mode") {
		return "你想练雅思口语完整模考，还是 Part 1、Part 2 或 Part 3 专项？"
	}
	if containsMissingField(missing, "ielts_topic_choice") {
		return "请选择一个话题类型：随机、人物、地点、事物或经历。"
	}
	return "请补充这个练习所需的具体信息。"
}

func (port *ServicePort) previewCustomPractice(
	ctx context.Context,
	call capability.CallContext,
	input PreviewInput,
) (PreviewResult, error) {
	blocking, err := validateCustomProposal(input)
	if err != nil {
		return PreviewResult{}, err
	}
	if blocking != nil {
		return *blocking, nil
	}
	experience := scene.PracticeExperience(input.SceneIntent.ExperienceHint)
	intent := input.SceneIntent
	spec, err := (scene.CustomSceneCompiler{}).Compile(scene.CustomSceneDraft{
		Scenario:       strings.TrimSpace(intent.Scenario),
		UserRole:       strings.TrimSpace(intent.UserRole),
		AIRole:         strings.TrimSpace(intent.AIRole),
		PracticeGoal:   strings.TrimSpace(intent.PracticeGoal),
		ExperienceHint: experience,
	})
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	plan, replayed, err := port.plans.PreviewCustomPlan(
		ctx,
		call.Actor,
		call.RequestID,
		preparation.CreateCustomPlanRequest{
			SourceThreadID:    call.ThreadID,
			BackgroundSummary: previewBackgroundSummary(input),
			SceneSpec:         spec,
		},
	)
	if err != nil {
		return PreviewResult{}, mapPreparationToolError(err)
	}
	return readyPreviewResult(plan, replayed, PreviewPlanSourceCustom)
}

func validateCustomProposal(input PreviewInput) (*PreviewResult, error) {
	experience := scene.PracticeExperience(input.SceneIntent.ExperienceHint)
	if isRestrictedPracticeExperience(experience) {
		return &PreviewResult{
			Status:           PreviewOutcomeRequiresSpecializedFlow,
			SceneResolution:  SceneResolutionRejected,
			ResolutionReason: ResolutionReasonSpecializedFlowRequired,
			AssistantText: "面试和雅思练习使用各自的正式准备流程。" +
				"请选择目录中的面试或雅思场景。",
		}, nil
	}
	if experience == "" {
		return &PreviewResult{
			Status: PreviewOutcomeNeedsDetails, SceneResolution: SceneResolutionNeedsDetails,
			RequiredMissingFields: []string{"experience_hint"},
			AssistantText:         "这个目录外场景属于职场还是生活旅行英语？",
		}, nil
	}
	if experience != scene.PracticeExperienceWorkplace &&
		experience != scene.PracticeExperienceLifeAndTravel {
		return nil, capability.ErrInvalidInput
	}
	intent := input.SceneIntent
	if _, err := (scene.CustomSceneCompiler{}).Compile(scene.CustomSceneDraft{
		Scenario: strings.TrimSpace(intent.Scenario), UserRole: strings.TrimSpace(intent.UserRole),
		AIRole: strings.TrimSpace(intent.AIRole), PracticeGoal: strings.TrimSpace(intent.PracticeGoal),
		ExperienceHint: experience,
	}); err != nil {
		return nil, mapPreparationToolError(err)
	}
	return nil, nil
}

func (port *ServicePort) previewClarification(
	ctx context.Context,
	ids []string,
) (PreviewResult, error) {
	candidates := make([]CatalogCandidate, len(ids))
	for index, id := range ids {
		selection, err := port.catalog.ResolvePreviewCatalogSelection(ctx, id)
		if err != nil {
			return PreviewResult{}, mapPreparationToolError(err)
		}
		if !selection.Valid() || selection.Scene.ID != id {
			return PreviewResult{}, capability.ErrExecutionRejected
		}
		candidates[index] = previewCatalogCandidate(selection)
	}
	if len(candidates) == 1 {
		return PreviewResult{
			Status:                PreviewOutcomeNeedsDetails,
			SceneResolution:       SceneResolutionNeedsDetails,
			CatalogCandidateCount: 1,
			Candidates:            candidates,
			AssistantText: "你指的是“" + candidates[0].Name +
				"”吗？请确认，或补充具体情境。",
		}, nil
	}
	return PreviewResult{
		Status:                PreviewOutcomeAmbiguous,
		SceneResolution:       SceneResolutionAmbiguous,
		CatalogCandidateCount: len(candidates),
		Candidates:            candidates,
		AssistantText:         ambiguousSceneQuestion(candidates),
	}, nil
}

func previewCatalogCandidate(
	selection scene.PreviewCatalogSelection,
) CatalogCandidate {
	options := make([]CatalogPracticeOption, len(selection.Scene.PracticeOptions))
	for index, option := range selection.Scene.PracticeOptions {
		options[index] = CatalogPracticeOption{
			ID:          option.ID,
			DisplayName: option.DisplayName,
			Mode:        string(option.Mode),
		}
	}
	return CatalogCandidate{
		SceneID:                 selection.Scene.ID,
		SceneVersion:            selection.Scene.Version,
		Name:                    selection.Scene.Name,
		PracticeExperience:      string(selection.Scene.Experience),
		SceneCategory:           string(selection.Scene.Category),
		DefaultRoleIDs:          append([]string(nil), selection.DefaultRoleIDs...),
		DefaultPracticeOptionID: selection.DefaultOption.ID,
		PracticeOptions:         options,
	}
}

func previewBackgroundSummary(input PreviewInput) string {
	if summary := strings.TrimSpace(input.BackgroundSummary); summary != "" {
		return summary
	}
	return "User requested practice for: " + strings.TrimSpace(input.SceneQuery)
}

func ambiguousSceneQuestion(candidates []CatalogCandidate) string {
	names := make([]string, len(candidates))
	for index, candidate := range candidates {
		names[index] = "“" + candidate.Name + "”"
	}
	return "我找到了多个可能的场景：" + strings.Join(names, "、") + "。你想练习哪一个？"
}

func readyPreviewResult(
	plan preparation.PracticePlan,
	replayed bool,
	source PreviewPlanSource,
) (PreviewResult, error) {
	clientAction, err := practicePlanClientAction(plan)
	if err != nil {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	resolution := SceneResolutionCustomResolved
	if source == PreviewPlanSourceCatalog {
		resolution = SceneResolutionCatalogResolved
	} else if source != PreviewPlanSourceCustom {
		return PreviewResult{}, capability.ErrExecutionRejected
	}
	return PreviewResult{
		Status:          PreviewOutcomeReady,
		SceneResolution: resolution,
		PlanID:          plan.ID,
		PlanSource:      source,
		Replayed:        replayed,
		ClientAction:    clientAction,
		AssistantText: "已为您准备好“" + plan.SceneSelection.Scene.Name +
			"”练习，请确认开始。",
		SourceRefs: []capability.SourceRef{{
			Type: "practice_plan",
			ID:   plan.ID,
		}},
	}, nil
}

func containsMissingField(fields []string, expected string) bool {
	for _, field := range fields {
		if field == expected {
			return true
		}
	}
	return false
}

func isRestrictedPracticeExperience(experience scene.PracticeExperience) bool {
	return experience == scene.PracticeExperienceInterview ||
		experience == scene.PracticeExperienceIELTSSpeaking
}

type confirmPracticePlanActionPayload struct {
	Label                    string   `json:"label"`
	PracticePlanID           string   `json:"practice_plan_id"`
	PlanVersion              int      `json:"plan_version"`
	SceneID                  string   `json:"scene_id"`
	SceneName                string   `json:"scene_name"`
	UserRole                 string   `json:"user_role"`
	AIRoles                  []string `json:"ai_roles"`
	PracticeGoal             string   `json:"practice_goal"`
	PracticeExperience       string   `json:"practice_experience"`
	SceneCategory            string   `json:"scene_category"`
	PracticeMode             string   `json:"practice_mode"`
	PracticeScope            string   `json:"practice_scope"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
	MinEffectiveTurns        int      `json:"min_effective_turns"`
	MaxEffectiveTurns        int      `json:"max_effective_turns"`
	ConfirmationPrompt       string   `json:"confirmation_prompt"`
}

var confirmPracticePlanActionFields = map[string]struct{}{
	"label": {}, "practice_plan_id": {}, "plan_version": {},
	"scene_id": {}, "scene_name": {}, "user_role": {}, "ai_roles": {},
	"practice_goal": {}, "practice_experience": {},
	"scene_category": {}, "practice_mode": {}, "practice_scope": {},
	"suggested_duration_seconds": {}, "min_effective_turns": {},
	"max_effective_turns": {}, "confirmation_prompt": {},
}

var practicePlanUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func practicePlanClientAction(
	plan preparation.PracticePlan,
) (agentclientaction.Action, error) {
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	roleNames := make([]string, len(roles))
	for index, role := range roles {
		roleNames[index] = role.DisplayName
	}
	sceneID := strings.TrimSpace(plan.SceneSelection.Source.SceneID)
	if sceneID == "" {
		sceneID = strings.TrimSpace(plan.SceneSelection.Scene.Key)
	}
	prompt := plan.SceneSelection.Scene.Prompt
	payload := confirmPracticePlanActionPayload{
		Label:                    "确认并开始练习",
		PracticePlanID:           plan.ID,
		PlanVersion:              plan.Version,
		SceneID:                  sceneID,
		SceneName:                plan.SceneSelection.Scene.Name,
		UserRole:                 strings.TrimSpace(prompt.UserRole),
		AIRoles:                  roleNames,
		PracticeGoal:             strings.TrimSpace(prompt.PracticeGoal),
		PracticeExperience:       string(plan.SceneSelection.Scene.Experience),
		SceneCategory:            string(plan.SceneSelection.Scene.Category),
		PracticeMode:             string(option.Mode),
		PracticeScope:            option.DisplayName,
		SuggestedDurationSeconds: plan.SessionPolicy.SuggestedDurationSeconds,
		MinEffectiveTurns:        plan.SessionPolicy.MinEffectiveTurns,
		MaxEffectiveTurns:        plan.SessionPolicy.MaxEffectiveTurns,
		ConfirmationPrompt:       "确认后将创建练习会话；确认前不会开始练习。",
	}
	if !validConfirmPracticePlanActionPayload(payload) {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	action, err := agentclientaction.New(ConfirmPracticePlanActionType, raw)
	if err != nil {
		return agentclientaction.Action{}, capability.ErrExecutionRejected
	}
	return action, nil
}

func validConfirmPracticePlanActionPayload(
	payload confirmPracticePlanActionPayload,
) bool {
	if !validActionText(payload.Label, 100) ||
		!practicePlanUUIDPattern.MatchString(payload.PracticePlanID) ||
		payload.PlanVersion < 1 ||
		!validActionText(payload.SceneID, 200) ||
		!validActionText(payload.SceneName, 200) ||
		!validActionText(payload.UserRole, 200) ||
		!validActionText(payload.PracticeGoal, 500) ||
		!validActionText(payload.PracticeExperience, 100) ||
		!validActionText(payload.SceneCategory, 200) ||
		!validActionText(payload.PracticeMode, 100) ||
		!validActionText(payload.PracticeScope, 200) ||
		payload.SuggestedDurationSeconds < 1 ||
		payload.MinEffectiveTurns < 1 ||
		(payload.MaxEffectiveTurns != 0 &&
			payload.MaxEffectiveTurns < payload.MinEffectiveTurns) ||
		payload.MaxEffectiveTurns > 100 ||
		!validActionText(payload.ConfirmationPrompt, 300) ||
		len(payload.AIRoles) < 1 || len(payload.AIRoles) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(payload.AIRoles))
	for _, role := range payload.AIRoles {
		if !validActionText(role, 200) {
			return false
		}
		if _, duplicate := seen[role]; duplicate {
			return false
		}
		seen[role] = struct{}{}
	}
	return true
}

func decodeConfirmPracticePlanClientAction(
	action agentclientaction.Action,
) (confirmPracticePlanActionPayload, bool) {
	if action.Type != ConfirmPracticePlanActionType ||
		agentclientaction.Validate(action) != nil {
		return confirmPracticePlanActionPayload{}, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(action.Payload, &fields) != nil ||
		len(fields) != len(confirmPracticePlanActionFields) {
		return confirmPracticePlanActionPayload{}, false
	}
	for field := range confirmPracticePlanActionFields {
		if _, found := fields[field]; !found {
			return confirmPracticePlanActionPayload{}, false
		}
	}
	var payload confirmPracticePlanActionPayload
	if json.Unmarshal(action.Payload, &payload) != nil ||
		!validConfirmPracticePlanActionPayload(payload) {
		return confirmPracticePlanActionPayload{}, false
	}
	return payload, true
}

func validActionText(value string, maxRunes int) bool {
	return value == strings.TrimSpace(value) && value != "" &&
		utf8.RuneCountInString(value) <= maxRunes
}

func previewOptionForMode(
	candidate CatalogCandidate,
	mode scene.PracticeMode,
) (string, bool) {
	for _, option := range candidate.PracticeOptions {
		if scene.PracticeMode(option.Mode) == mode {
			return option.ID, true
		}
	}
	return "", false
}

func validPreviewIELTSTopicChoice(
	mode scene.PracticeMode,
	choice string,
) bool {
	if mode == scene.PracticeModeFullMock {
		return choice == ""
	}
	if mode != scene.PracticeModePart1 &&
		mode != scene.PracticeModePart2 &&
		mode != scene.PracticeModePart3 {
		return false
	}
	switch choice {
	case "random", "person", "place", "thing", "experience":
		return true
	default:
		return false
	}
}

func previewIELTSQuestionSelection(
	input PreviewInput,
) *preparation.IELTSQuestionSelection {
	if input.IELTSTopicChoice == "" || input.IELTSTopicChoice == "random" {
		return nil
	}
	return &preparation.IELTSQuestionSelection{
		CueCardType: input.IELTSTopicChoice,
	}
}

func mapPreparationToolError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, scene.ErrSceneNotFound),
		errors.Is(err, scene.ErrCatalogSelectionInvalid),
		errors.Is(err, scene.ErrCustomSceneInvalid),
		errors.Is(err, preparation.ErrPlanInvalid),
		errors.Is(err, preparation.ErrPendingActionInvalid):
		return capability.ErrInvalidInput
	case errors.Is(err, preparation.ErrPlanNotFound),
		errors.Is(err, preparation.ErrPlanConflict),
		errors.Is(err, preparation.ErrPlanIdempotencyConflict),
		errors.Is(err, preparation.ErrPendingActionNotFound),
		errors.Is(err, preparation.ErrPendingActionConflict):
		return capability.ErrExecutionRejected
	default:
		return err
	}
}
