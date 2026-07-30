package practice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

type PracticeAnchor struct {
	ThreadID string
	MatterID string
}

// AgentContextReader is caller-defined so Practice never imports Agent or
// Matter Repository types.
type AgentContextReader interface {
	ValidatePracticeAnchor(
		context.Context,
		requestcontext.Actor,
		string,
		string,
	) (PracticeAnchor, error)
}

type PreparationProfileRef struct {
	ID      string
	Version int
}

// PreparationContextReader is the minimum actor-scoped Preparation view
// required by Practice.
type PreparationContextReader interface {
	ReadPreparationProfile(
		context.Context,
		requestcontext.Actor,
		string,
	) (PreparationProfileRef, error)
	ReadPreparationSnapshot(
		context.Context,
		requestcontext.Actor,
		string,
	) (persistence.PreparationSnapshot, error)
}

type PlanCatalogRequest struct {
	ScenarioDefinitionID      string
	ScenarioDefinitionVersion int
	ScenarioConfigID          string
	ScenarioConfigVersion     int
	SelectedRoleIDs           []string
	PracticeOptionID          string
	PracticeOptionVersion     int
}

type PlanCatalogSelection struct {
	ScenarioDefinition persistence.ScenarioDefinitionSnapshot
	ScenarioConfig     persistence.ScenarioConfigSnapshot
	SelectedRoles      []persistence.RoleSnapshot
	PracticeOption     persistence.PracticeOptionSnapshot
}

type SessionCatalogRequest struct {
	Plan              persistence.Plan
	PracticeOptionID  string
	RoleDefinitionIDs []string
}

type SessionCatalogSelection struct {
	PlanCatalogSelection
	PracticeOption persistence.PracticeOptionSnapshot
}

// CatalogContextReader prevents Practice from depending on Preparation's
// catalog implementation while still requiring exact IDs and versions.
type CatalogContextReader interface {
	ReadPlanCatalog(PlanCatalogRequest) (PlanCatalogSelection, error)
	ReadSessionCatalog(SessionCatalogRequest) (SessionCatalogSelection, error)
}

type IELTSQuestionSetRequest struct {
	Mode         string
	Part1SetID   string
	TopicGroupID string
}

type IELTSQuestionSetSelection struct {
	BankID         string
	Season         string
	Mode           string
	Part1SetID     string
	TopicGroupID   string
	TopicTitle     string
	Part2CueCard   string
	TurnBlueprints []string
	Part1Questions int
	Part2Questions int
	Part3Questions int
}

type IELTSCatalogContextReader interface {
	ResolveIELTSQuestionSet(
		IELTSQuestionSetRequest,
	) (IELTSQuestionSetSelection, error)
}

type PracticeResourceIDGenerator interface {
	NewID() (string, error)
}

type ContextApplication struct {
	repository   persistence.ContextRepository
	ids          PracticeResourceIDGenerator
	agentContext AgentContextReader
	preparation  PreparationContextReader
	catalog      CatalogContextReader
}

func NewContextApplication(
	repository persistence.ContextRepository,
	ids PracticeResourceIDGenerator,
	agentContext AgentContextReader,
	preparationReader PreparationContextReader,
	catalog CatalogContextReader,
) (*ContextApplication, error) {
	if repository == nil || ids == nil || agentContext == nil ||
		preparationReader == nil || catalog == nil {
		return nil, errors.New("practice: context dependency is required")
	}
	return &ContextApplication{
		repository:   repository,
		ids:          ids,
		agentContext: agentContext,
		preparation:  preparationReader,
		catalog:      catalog,
	}, nil
}

func (a *ContextApplication) CreatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	request CreatePlanRequest,
) (persistence.Plan, bool, error) {
	if ctx == nil || !actor.Valid() || !validCreatePlanRequest(request) {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-plans",
		idempotencyKey,
		request,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	replayed, found, err := a.repository.ReplayPlan(
		ctx,
		contextActor(actor),
		intent,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if found {
		return replayed, true, nil
	}
	anchor, err := a.agentContext.ValidatePracticeAnchor(
		ctx,
		actor,
		request.AgentThreadID,
		request.MatterID,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if anchor.ThreadID != request.AgentThreadID ||
		anchor.MatterID != request.MatterID {
		return persistence.Plan{}, false, persistence.ErrConflict
	}

	profileID := request.PreparationProfileID
	catalogRequest := PlanCatalogRequest{
		ScenarioDefinitionID:      request.ScenarioDefinitionID,
		ScenarioDefinitionVersion: request.ScenarioDefinitionVersion,
		ScenarioConfigID:          request.ScenarioConfigID,
		ScenarioConfigVersion:     request.ScenarioConfigVersion,
		SelectedRoleIDs:           cloneStrings(request.SelectedRoleIDs),
	}
	var preparationSnapshot *persistence.PreparationSnapshot
	snapshotBacked := request.PreparationSnapshotID != ""
	targeted := false
	if snapshotBacked {
		snapshot, readErr := a.preparation.ReadPreparationSnapshot(
			ctx,
			actor,
			request.PreparationSnapshotID,
		)
		if readErr != nil {
			return persistence.Plan{}, false, readErr
		}
		profileID = snapshot.SourceProfileID
		if request.PreparationProfileID != "" &&
			request.PreparationProfileID != profileID {
			return persistence.Plan{}, false, persistence.ErrConflict
		}
		targeted = validTargetedPreparationSnapshot(snapshot)
		if targeted {
			recommendation := snapshot.JobTargetCandidateSnapshot.
				CatalogRecommendation
			catalogRequest = PlanCatalogRequest{
				ScenarioDefinitionID: recommendation.ScenarioDefinitionID,
				ScenarioDefinitionVersion: recommendation.
					ScenarioDefinitionVersion,
				SelectedRoleIDs: cloneStrings(
					recommendation.SelectedRoleIDs,
				),
				PracticeOptionID: recommendation.PracticeOptionID,
				PracticeOptionVersion: recommendation.
					PracticeOptionVersion,
			}
		}
		preparationCopy := clonePreparationSnapshot(snapshot)
		preparationSnapshot = &preparationCopy
		if !targeted &&
			(!validContextResourceID(catalogRequest.ScenarioDefinitionID) ||
				catalogRequest.ScenarioDefinitionVersion < 1 ||
				!validContextResourceID(catalogRequest.ScenarioConfigID) ||
				catalogRequest.ScenarioConfigVersion < 1 ||
				len(catalogRequest.SelectedRoleIDs) == 0 ||
				!validContextResourceID(request.PracticeOptionID) ||
				request.PracticeOptionVersion < 1 ||
				request.MaxEffectiveTurns < 1) {
			return persistence.Plan{}, false, persistence.ErrConflict
		}
	} else {
		profile, readErr := a.preparation.ReadPreparationProfile(
			ctx,
			actor,
			profileID,
		)
		if readErr != nil {
			return persistence.Plan{}, false, readErr
		}
		if profile.ID != profileID || profile.Version < 1 {
			return persistence.Plan{}, false, persistence.ErrNotFound
		}
	}

	if request.PracticeOptionID != "" {
		catalogRequest.PracticeOptionID = request.PracticeOptionID
		catalogRequest.PracticeOptionVersion = request.PracticeOptionVersion
	}
	catalog, err := a.catalog.ReadPlanCatalog(catalogRequest)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if !validPlanCatalogSelection(catalogRequest, catalog) ||
		(targeted && !compatibleTargetedPlanRequest(request, catalog)) {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	if targeted && catalog.ScenarioDefinition.Type == "INTERVIEW" &&
		len(catalog.SelectedRoles) != 1 {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	planID, err := a.ids.NewID()
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	var (
		catalogSnapshot *persistence.PlanCatalogSnapshot
		sessionPolicy   *persistence.ContextSessionPolicy
		practiceFocuses []persistence.PracticeObjective
	)
	configuredPreview := snapshotBacked ||
		(request.PracticeOptionID != "" && request.MaxEffectiveTurns > 0)
	if configuredPreview {
		catalogValue := planCatalogSnapshot(catalog)
		policyValue := defaultContextSessionPolicy(
			catalog.ScenarioConfig,
			catalog.PracticeOption,
		)
		if request.MaxEffectiveTurns > 0 {
			var adjusted bool
			policyValue, adjusted = adjustedContextSessionPolicy(
				policyValue,
				request.MaxEffectiveTurns,
			)
			if !adjusted {
				return persistence.Plan{}, false, persistence.ErrConflict
			}
		}
		catalogSnapshot = &catalogValue
		sessionPolicy = &policyValue
		practiceFocuses = contextPracticeFocuses(catalog.SelectedRoles)
	}
	return a.repository.CreatePlan(ctx, contextActor(actor), persistence.CreatePlanCommand{
		PlanID:                    planID,
		AgentThreadID:             request.AgentThreadID,
		MatterID:                  request.MatterID,
		ScenarioDefinitionID:      catalog.ScenarioDefinition.ID,
		ScenarioDefinitionVersion: catalog.ScenarioDefinition.Version,
		ScenarioType:              catalog.ScenarioDefinition.Type,
		ScenarioModel:             catalog.ScenarioDefinition.Model,
		ScenarioConfigID:          catalog.ScenarioConfig.ID,
		ScenarioConfigVersion:     catalog.ScenarioConfig.Version,
		PreparationProfileID:      profileID,
		SelectedRoleIDs:           roleSnapshotIDs(catalog.SelectedRoles),
		PreparationSnapshot:       preparationSnapshot,
		CatalogSnapshot:           catalogSnapshot,
		SessionPolicy:             sessionPolicy,
		PracticeFocuses:           practiceFocuses,
		Intent:                    intent,
	})
}

func (a *ContextApplication) GetPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (persistence.Plan, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(planID) {
		return persistence.Plan{}, persistence.ErrNotFound
	}
	return a.repository.GetPlan(ctx, contextActor(actor), planID)
}

func (a *ContextApplication) UpdatePlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	idempotencyKey string,
	request UpdatePlanRequest,
) (persistence.Plan, bool, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(planID) ||
		!validUpdatePlanRequest(request) {
		return persistence.Plan{}, false, persistence.ErrInvalidArgument
	}
	intent, err := newContextIntent(
		"PUT",
		"/v1/practice-plans/"+planID,
		idempotencyKey,
		request,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	replayed, found, err := a.repository.ReplayPlan(
		ctx,
		contextActor(actor),
		intent,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if found {
		return replayed, true, nil
	}
	plan, err := a.repository.GetPlan(ctx, contextActor(actor), planID)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if plan.Status != persistence.PlanStatusReady ||
		plan.Revision != request.ExpectedPlanRevision ||
		plan.PreparationSnapshot == nil ||
		plan.CatalogSnapshot == nil ||
		plan.SessionPolicy == nil {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	catalogRequest := PlanCatalogRequest{
		ScenarioDefinitionID:      plan.ScenarioDefinitionID,
		ScenarioDefinitionVersion: plan.ScenarioDefinitionVersion,
		ScenarioConfigID:          plan.ScenarioConfigID,
		ScenarioConfigVersion:     plan.ScenarioConfigVersion,
		SelectedRoleIDs:           cloneStrings(request.SelectedRoleIDs),
		PracticeOptionID:          request.PracticeOptionID,
		PracticeOptionVersion:     request.PracticeOptionVersion,
	}
	catalog, err := a.catalog.ReadPlanCatalog(catalogRequest)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if !validPlanCatalogSelection(catalogRequest, catalog) {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	if catalog.ScenarioDefinition.Type == "INTERVIEW" &&
		len(catalog.SelectedRoles) != 1 {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	policy, ok := adjustedContextSessionPolicy(
		defaultContextSessionPolicy(
			catalog.ScenarioConfig,
			catalog.PracticeOption,
		),
		request.MaxEffectiveTurns,
	)
	if !ok {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	return a.repository.UpdatePlan(
		ctx,
		contextActor(actor),
		persistence.UpdatePlanCommand{
			PlanID:               planID,
			ExpectedPlanRevision: request.ExpectedPlanRevision,
			SelectedRoleIDs:      roleSnapshotIDs(catalog.SelectedRoles),
			CatalogSnapshot:      planCatalogSnapshot(catalog),
			SessionPolicy:        policy,
			PracticeFocuses: contextPracticeFocuses(
				catalog.SelectedRoles,
			),
			Intent: intent,
		},
	)
}

func (a *ContextApplication) CreateSession(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	idempotencyKey string,
	request CreateSessionRequest,
) (persistence.ContextSessionBootstrap, bool, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(planID) ||
		!validCreateSessionRequest(request) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrInvalidArgument
	}
	if !request.UserConfirmed {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConfirmationRequired
	}
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-plans/"+planID+"/practice-sessions",
		idempotencyKey,
		request,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	replayed, found, err := a.repository.ReplayContextSession(
		ctx,
		contextActor(actor),
		intent,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if found {
		return replayed, true, nil
	}
	plan, err := a.repository.GetPlan(ctx, contextActor(actor), planID)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if plan.Status != persistence.PlanStatusReady ||
		plan.Revision != request.ExpectedPlanRevision {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	if completeTargetedPlanPreview(plan) {
		return a.createTargetedContextSession(
			ctx,
			actor,
			plan,
			request,
			intent,
		)
	}
	if plan.ScenarioType == "INTERVIEW" {
		roleIDs := request.RoleDefinitionIDs
		if completePlanConfiguration(plan) {
			roleIDs = plan.SelectedRoleIDs
		}
		if len(roleIDs) != 1 {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
	}
	anchor, err := a.agentContext.ValidatePracticeAnchor(
		ctx,
		actor,
		plan.AgentThreadID,
		plan.MatterID,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if anchor.ThreadID != plan.AgentThreadID ||
		anchor.MatterID != plan.MatterID {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	var preparationSnapshot persistence.PreparationSnapshot
	if plan.PreparationSnapshot != nil {
		if request.PreparationSnapshotID != "" &&
			request.PreparationSnapshotID != plan.PreparationSnapshot.ID {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
		preparationSnapshot = clonePreparationSnapshot(
			*plan.PreparationSnapshot,
		)
	} else {
		preparationSnapshot, err = a.preparation.ReadPreparationSnapshot(
			ctx,
			actor,
			request.PreparationSnapshotID,
		)
		if err != nil {
			return persistence.ContextSessionBootstrap{}, false, err
		}
		if preparationSnapshot.ID != request.PreparationSnapshotID ||
			preparationSnapshot.SourceProfileID != plan.PreparationProfileID {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
	}
	var (
		catalog       SessionCatalogSelection
		sessionPolicy persistence.ContextSessionPolicy
		focuses       []persistence.PracticeObjective
	)
	if completePlanConfiguration(plan) {
		if (request.PracticeOptionID != "" &&
			request.PracticeOptionID !=
				plan.CatalogSnapshot.PracticeOption.ID) ||
			(len(request.RoleDefinitionIDs) > 0 && !equalContextStrings(
				request.RoleDefinitionIDs,
				plan.SelectedRoleIDs,
			)) {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
		catalog = SessionCatalogSelection{
			PlanCatalogSelection: PlanCatalogSelection{
				ScenarioDefinition: plan.CatalogSnapshot.ScenarioDefinition,
				ScenarioConfig:     plan.CatalogSnapshot.ScenarioConfig,
				SelectedRoles: cloneRoleSnapshots(
					plan.CatalogSnapshot.SelectedRoles,
				),
				PracticeOption: plan.CatalogSnapshot.PracticeOption,
			},
			PracticeOption: plan.CatalogSnapshot.PracticeOption,
		}
		sessionPolicy = cloneContextSessionPolicy(*plan.SessionPolicy)
		focuses = clonePracticeObjectives(plan.PracticeFocuses)
	} else {
		catalog, err = a.catalog.ReadSessionCatalog(SessionCatalogRequest{
			Plan:              plan,
			PracticeOptionID:  request.PracticeOptionID,
			RoleDefinitionIDs: cloneStrings(request.RoleDefinitionIDs),
		})
		if err != nil {
			return persistence.ContextSessionBootstrap{}, false, err
		}
		if !validSessionCatalogSelection(plan, request, catalog) {
			return persistence.ContextSessionBootstrap{}, false,
				persistence.ErrConflict
		}
		sessionPolicy = defaultContextSessionPolicy(
			catalog.ScenarioConfig,
			catalog.PracticeOption,
		)
		focuses = contextPracticeFocuses(catalog.SelectedRoles)
	}
	catalog, assignment, err := a.applyIELTSQuestionSelection(
		plan,
		request,
		catalog,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	if assignment != nil {
		sessionPolicy = defaultContextSessionPolicy(
			catalog.ScenarioConfig,
			catalog.PracticeOption,
		)
	}
	sessionID, snapshotID, participants, err := a.newSessionIdentities(
		actor,
		catalog.SelectedRoles,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	snapshot := persistence.ContextSessionSnapshot{
		ID:                 snapshotID,
		SessionID:          sessionID,
		PlanRevision:       plan.Revision,
		ScenarioType:       plan.ScenarioType,
		ScenarioModel:      plan.ScenarioModel,
		ScenarioDefinition: catalog.ScenarioDefinition,
		ScenarioConfig:     catalog.ScenarioConfig,
		Preparation:        preparationSnapshot,
		Participants:       participants,
		PracticeOption:     catalog.PracticeOption,
		SessionPolicy:      sessionPolicy,
		PracticeFocuses:    focuses,
		IELTSAssignment:    assignment,
	}
	return a.repository.CreateContextSession(
		ctx,
		contextActor(actor),
		persistence.CreateContextSessionCommand{
			SessionID:             sessionID,
			SnapshotID:            snapshotID,
			PlanID:                plan.ID,
			ExpectedPlanRevision:  request.ExpectedPlanRevision,
			PreparationSnapshotID: preparationSnapshot.ID,
			Snapshot:              snapshot,
			Intent:                intent,
		},
	)
}

func (a *ContextApplication) ConfirmAndStartPractice(
	ctx context.Context,
	actor requestcontext.Actor,
	idempotencyKey string,
	confirmation StartConfirmation,
) (ConfirmAndStartResult, error) {
	if ctx == nil || !actor.Valid() ||
		!validContextResourceID(confirmation.AgentThreadID) ||
		!validContextResourceID(confirmation.PracticePlanID) ||
		confirmation.ExpectedPlanRevision < 1 {
		return ConfirmAndStartResult{}, persistence.ErrInvalidArgument
	}
	plan, err := a.repository.GetPlan(
		ctx,
		contextActor(actor),
		confirmation.PracticePlanID,
	)
	if err != nil {
		return ConfirmAndStartResult{}, err
	}
	if plan.AgentThreadID != confirmation.AgentThreadID {
		return ConfirmAndStartResult{}, persistence.ErrNotFound
	}
	if plan.Status != persistence.PlanStatusReady ||
		plan.Revision != confirmation.ExpectedPlanRevision {
		return ConfirmAndStartResult{}, persistence.ErrConflict
	}
	bootstrap, replayed, err := a.CreateSession(
		ctx,
		actor,
		confirmation.PracticePlanID,
		idempotencyKey,
		CreateSessionRequest{
			ExpectedPlanRevision: confirmation.ExpectedPlanRevision,
			UserConfirmed:        true,
			IELTSSelection:       confirmation.IELTSSelection,
		},
	)
	if errors.Is(err, persistence.ErrActiveSessionConflict) {
		active, resolveErr := a.repository.ResolveContextSessionByThread(
			ctx,
			contextActor(actor),
			confirmation.AgentThreadID,
		)
		if resolveErr != nil {
			return ConfirmAndStartResult{}, err
		}
		return ConfirmAndStartResult{
			Bootstrap:      active,
			ActiveConflict: true,
		}, nil
	}
	if err != nil {
		return ConfirmAndStartResult{}, err
	}
	return ConfirmAndStartResult{
		Bootstrap: bootstrap,
		Replayed:  replayed,
	}, nil
}

func (a *ContextApplication) createTargetedContextSession(
	ctx context.Context,
	actor requestcontext.Actor,
	plan persistence.Plan,
	request CreateSessionRequest,
	intent persistence.ContextIdempotencyIntent,
) (persistence.ContextSessionBootstrap, bool, error) {
	if !compatibleTargetedStartRequest(plan, request) {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	catalog := *plan.CatalogSnapshot
	if plan.ScenarioType == "INTERVIEW" &&
		len(catalog.SelectedRoles) != 1 {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
	}
	sessionCatalog := SessionCatalogSelection{
		PlanCatalogSelection: PlanCatalogSelection{
			ScenarioDefinition: catalog.ScenarioDefinition,
			ScenarioConfig:     catalog.ScenarioConfig,
			SelectedRoles:      catalog.SelectedRoles,
			PracticeOption:     catalog.PracticeOption,
		},
		PracticeOption: catalog.PracticeOption,
	}
	sessionCatalog, assignment, err := a.applyIELTSQuestionSelection(
		plan,
		request,
		sessionCatalog,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	sessionPolicy := cloneContextSessionPolicy(*plan.SessionPolicy)
	if assignment != nil {
		sessionPolicy = defaultContextSessionPolicy(
			sessionCatalog.ScenarioConfig,
			sessionCatalog.PracticeOption,
		)
	}
	sessionID, snapshotID, participants, err := a.newSessionIdentities(
		actor,
		sessionCatalog.SelectedRoles,
	)
	if err != nil {
		return persistence.ContextSessionBootstrap{}, false, err
	}
	snapshot := persistence.ContextSessionSnapshot{
		ID:                 snapshotID,
		SessionID:          sessionID,
		PlanRevision:       plan.Revision,
		ScenarioType:       plan.ScenarioType,
		ScenarioModel:      plan.ScenarioModel,
		ScenarioDefinition: sessionCatalog.ScenarioDefinition,
		ScenarioConfig: cloneScenarioConfigSnapshot(
			sessionCatalog.ScenarioConfig,
		),
		Preparation: clonePreparationSnapshot(
			*plan.PreparationSnapshot,
		),
		Participants:   participants,
		PracticeOption: sessionCatalog.PracticeOption,
		SessionPolicy:  sessionPolicy,
		PracticeFocuses: clonePracticeObjectives(
			plan.PracticeFocuses,
		),
		IELTSAssignment: assignment,
	}
	return a.repository.CreateContextSession(
		ctx,
		contextActor(actor),
		persistence.CreateContextSessionCommand{
			SessionID:             sessionID,
			SnapshotID:            snapshotID,
			PlanID:                plan.ID,
			ExpectedPlanRevision:  request.ExpectedPlanRevision,
			PreparationSnapshotID: plan.PreparationSnapshot.ID,
			Snapshot:              snapshot,
			Intent:                intent,
		},
	)
}

func (a *ContextApplication) GetSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (persistence.ContextSession, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) {
		return persistence.ContextSession{}, persistence.ErrNotFound
	}
	return a.repository.GetContextSession(ctx, contextActor(actor), sessionID)
}

func (a *ContextApplication) GetSessionSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (persistence.ContextSessionSnapshot, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) {
		return persistence.ContextSessionSnapshot{}, persistence.ErrNotFound
	}
	return a.repository.GetContextSessionSnapshot(
		ctx,
		contextActor(actor),
		sessionID,
	)
}

func (a *ContextApplication) ResolveSessionByThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (persistence.ContextSessionBootstrap, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(threadID) {
		return persistence.ContextSessionBootstrap{}, persistence.ErrNotFound
	}
	return a.repository.ResolveContextSessionByThread(
		ctx,
		contextActor(actor),
		threadID,
	)
}

func (a *ContextApplication) TransitionSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
	expectedVersion int,
	transition persistence.ContextSessionTransition,
) (persistence.ContextSession, bool, error) {
	wireAction, validTransition := contextTransitionWireAction(transition)
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) ||
		expectedVersion < 1 || !validTransition {
		return persistence.ContextSession{}, false,
			persistence.ErrInvalidArgument
	}
	payload := struct {
		ExpectedSessionVersion int `json:"expected_session_version"`
	}{ExpectedSessionVersion: expectedVersion}
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-sessions/"+sessionID+"/"+wireAction,
		idempotencyKey,
		payload,
	)
	if err != nil {
		return persistence.ContextSession{}, false, err
	}
	return a.repository.TransitionContextSession(
		ctx,
		contextActor(actor),
		persistence.TransitionContextSessionCommand{
			SessionID:              sessionID,
			ExpectedSessionVersion: expectedVersion,
			Transition:             transition,
			Intent:                 intent,
		},
	)
}

func (a *ContextApplication) newSessionIdentities(
	actor requestcontext.Actor,
	roles []persistence.RoleSnapshot,
) (string, string, []persistence.ContextParticipant, error) {
	sessionID, err := a.ids.NewID()
	if err != nil {
		return "", "", nil, persistence.ErrConflict
	}
	snapshotID, err := a.ids.NewID()
	if err != nil {
		return "", "", nil, persistence.ErrConflict
	}
	participants := make([]persistence.ContextParticipant, 0, len(roles)+1)
	for _, role := range roles {
		participantID, err := a.ids.NewID()
		if err != nil {
			return "", "", nil, persistence.ErrConflict
		}
		roleCopy := role
		participants = append(participants, persistence.ContextParticipant{
			ID:        participantID,
			SessionID: sessionID,
			Role:      "FACILITATOR",
			SubjectRef: persistence.SubjectRef{
				Namespace: "speakup.role",
				SubjectID: role.ID,
			},
			RoleDefinitionID: role.ID,
			RoleSnapshot:     &roleCopy,
			Order:            len(participants) + 1,
		})
	}
	candidateID, err := a.ids.NewID()
	if err != nil {
		return "", "", nil, persistence.ErrConflict
	}
	participants = append(participants, persistence.ContextParticipant{
		ID:        candidateID,
		SessionID: sessionID,
		Role:      "LEARNER",
		SubjectRef: persistence.SubjectRef{
			Namespace: "speakup.user",
			SubjectID: actor.UserID,
		},
		Order: len(participants) + 1,
	})
	return sessionID, snapshotID, participants, nil
}

func newContextIntent(
	method string,
	path string,
	key string,
	payload any,
) (persistence.ContextIdempotencyIntent, error) {
	if (method != "POST" && method != "PUT") ||
		!validContextResourcePath(path) ||
		!validContextIdempotencyKey(key) {
		return persistence.ContextIdempotencyIntent{},
			persistence.ErrInvalidArgument
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return persistence.ContextIdempotencyIntent{},
			persistence.ErrInvalidArgument
	}
	return persistence.ContextIdempotencyIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(canonical),
	}, nil
}

func validCreatePlanRequest(request CreatePlanRequest) bool {
	if !validContextResourceID(request.AgentThreadID) ||
		(request.MatterID != "" &&
			!validContextResourceID(request.MatterID)) {
		return false
	}
	if request.PreparationSnapshotID == "" {
		return validContextResourceID(request.ScenarioDefinitionID) &&
			request.ScenarioDefinitionVersion > 0 &&
			validContextResourceID(request.ScenarioConfigID) &&
			request.ScenarioConfigVersion > 0 &&
			validContextResourceID(request.PreparationProfileID) &&
			validUniqueContextIDs(request.SelectedRoleIDs) &&
			validOptionalPlanPreviewSelection(request)
	}
	if !validContextResourceID(request.PreparationSnapshotID) ||
		(request.PreparationProfileID != "" &&
			!validContextResourceID(request.PreparationProfileID)) ||
		(len(request.SelectedRoleIDs) > 0 &&
			!validUniqueContextIDs(request.SelectedRoleIDs)) {
		return false
	}
	scenarioEmpty := request.ScenarioDefinitionID == "" &&
		request.ScenarioDefinitionVersion == 0
	scenarioValid := validContextResourceID(request.ScenarioDefinitionID) &&
		request.ScenarioDefinitionVersion > 0
	configEmpty := request.ScenarioConfigID == "" &&
		request.ScenarioConfigVersion == 0
	configValid := validContextResourceID(request.ScenarioConfigID) &&
		request.ScenarioConfigVersion > 0
	return (scenarioEmpty || scenarioValid) &&
		(configEmpty || configValid) &&
		validOptionalPlanPreviewSelection(request)
}

func validOptionalPlanPreviewSelection(request CreatePlanRequest) bool {
	optionEmpty := request.PracticeOptionID == "" &&
		request.PracticeOptionVersion == 0
	optionValid := validContextResourceID(request.PracticeOptionID) &&
		request.PracticeOptionVersion > 0
	return (optionEmpty || optionValid) && request.MaxEffectiveTurns >= 0
}

func validUpdatePlanRequest(request UpdatePlanRequest) bool {
	return request.ExpectedPlanRevision > 0 &&
		validUniqueContextIDs(request.SelectedRoleIDs) &&
		validContextResourceID(request.PracticeOptionID) &&
		request.PracticeOptionVersion > 0 &&
		request.MaxEffectiveTurns > 0
}

func validCreateSessionRequest(request CreateSessionRequest) bool {
	return request.ExpectedPlanRevision > 0 &&
		(request.PreparationSnapshotID == "" ||
			validContextResourceID(request.PreparationSnapshotID)) &&
		(request.PracticeOptionID == "" ||
			validContextResourceID(request.PracticeOptionID)) &&
		(len(request.RoleDefinitionIDs) == 0 ||
			validUniqueContextIDs(request.RoleDefinitionIDs)) &&
		(request.IELTSSelection == nil ||
			validIELTSPracticeSelectionInput(*request.IELTSSelection))
}

func validIELTSPracticeSelectionInput(selection IELTSPracticeSelection) bool {
	switch selection.Mode {
	case "FULL_MOCK":
		return validContextResourceID(selection.Part1SetID) &&
			validContextResourceID(selection.TopicGroupID)
	case "PART_1":
		return validContextResourceID(selection.Part1SetID) &&
			selection.TopicGroupID == ""
	case "PART_2", "PART_3":
		return selection.Part1SetID == "" &&
			validContextResourceID(selection.TopicGroupID)
	default:
		return false
	}
}

func validPlanCatalogSelection(
	request PlanCatalogRequest,
	selection PlanCatalogSelection,
) bool {
	if selection.ScenarioDefinition.ID != request.ScenarioDefinitionID ||
		selection.ScenarioDefinition.Version !=
			request.ScenarioDefinitionVersion ||
		selection.ScenarioDefinition.Status != "active" ||
		selection.ScenarioConfig.ScenarioDefinitionID !=
			request.ScenarioDefinitionID ||
		selection.ScenarioConfig.Type != selection.ScenarioDefinition.Type ||
		selection.ScenarioConfig.Model != selection.ScenarioDefinition.Model ||
		!validScenarioFamilyModel(
			selection.ScenarioDefinition.Type,
			selection.ScenarioDefinition.Model,
		) ||
		len(selection.SelectedRoles) != len(request.SelectedRoleIDs) {
		return false
	}
	if request.ScenarioConfigID != "" &&
		(selection.ScenarioConfig.ID != request.ScenarioConfigID ||
			selection.ScenarioConfig.Version != request.ScenarioConfigVersion) {
		return false
	}
	for index, role := range selection.SelectedRoles {
		if role.ID != request.SelectedRoleIDs[index] ||
			role.ScenarioDefinitionID != request.ScenarioDefinitionID ||
			role.Version < 1 {
			return false
		}
	}
	if selection.PracticeOption.ScenarioDefinitionID !=
		request.ScenarioDefinitionID ||
		selection.PracticeOption.Version < 1 {
		return false
	}
	if request.PracticeOptionID != "" &&
		selection.PracticeOption.ID != request.PracticeOptionID {
		return false
	}
	if request.PracticeOptionVersion > 0 &&
		selection.PracticeOption.Version != request.PracticeOptionVersion {
		return false
	}
	if selection.PracticeOption.Type == "FOCUS" {
		return len(selection.SelectedRoles) == 1 &&
			selection.PracticeOption.RoleDefinitionID ==
				selection.SelectedRoles[0].ID
	}
	return selection.PracticeOption.Type == "FULL_SIMULATION" &&
		selection.PracticeOption.RoleDefinitionID == ""
}

func validSessionCatalogSelection(
	plan persistence.Plan,
	request CreateSessionRequest,
	selection SessionCatalogSelection,
) bool {
	if !validPlanCatalogSelection(PlanCatalogRequest{
		ScenarioDefinitionID:      plan.ScenarioDefinitionID,
		ScenarioDefinitionVersion: plan.ScenarioDefinitionVersion,
		ScenarioConfigID:          plan.ScenarioConfigID,
		ScenarioConfigVersion:     plan.ScenarioConfigVersion,
		SelectedRoleIDs:           request.RoleDefinitionIDs,
		PracticeOptionID:          request.PracticeOptionID,
	}, selection.PlanCatalogSelection) {
		return false
	}
	selectedPlanRoles := make(map[string]struct{}, len(plan.SelectedRoleIDs))
	for _, roleID := range plan.SelectedRoleIDs {
		selectedPlanRoles[roleID] = struct{}{}
	}
	for _, roleID := range request.RoleDefinitionIDs {
		if _, ok := selectedPlanRoles[roleID]; !ok {
			return false
		}
	}
	focusMatches := true
	if selection.PracticeOption.Type == "FOCUS" {
		focusMatches = len(request.RoleDefinitionIDs) == 1 &&
			selection.PracticeOption.RoleDefinitionID ==
				request.RoleDefinitionIDs[0]
	}
	return selection.PracticeOption.ID == request.PracticeOptionID &&
		selection.PracticeOption.ScenarioDefinitionID ==
			plan.ScenarioDefinitionID &&
		selection.PracticeOption.Version > 0 &&
		focusMatches
}

func compatibleTargetedPlanRequest(
	request CreatePlanRequest,
	selection PlanCatalogSelection,
) bool {
	if request.ScenarioDefinitionID != "" &&
		(request.ScenarioDefinitionID != selection.ScenarioDefinition.ID ||
			request.ScenarioDefinitionVersion !=
				selection.ScenarioDefinition.Version) {
		return false
	}
	if request.ScenarioConfigID != "" &&
		(request.ScenarioConfigID != selection.ScenarioConfig.ID ||
			request.ScenarioConfigVersion != selection.ScenarioConfig.Version) {
		return false
	}
	if request.PracticeOptionID != "" &&
		(request.PracticeOptionID != selection.PracticeOption.ID ||
			request.PracticeOptionVersion != selection.PracticeOption.Version) {
		return false
	}
	return len(request.SelectedRoleIDs) == 0 ||
		equalContextStrings(
			request.SelectedRoleIDs,
			roleSnapshotIDs(selection.SelectedRoles),
		)
}

func validTargetedPreparationSnapshot(
	snapshot persistence.PreparationSnapshot,
) bool {
	if !validContextResourceID(snapshot.ID) ||
		!validContextResourceID(snapshot.SourceProfileID) ||
		snapshot.SourceVersion < 1 ||
		!validContextResourceID(snapshot.SourceJobTargetID) ||
		snapshot.SourceJobTargetConfirmationVersion < 1 ||
		snapshot.JobTargetInputSnapshot == nil ||
		snapshot.JobTargetCandidateSnapshot == nil ||
		strings.TrimSpace(snapshot.BackgroundSnapshot) == "" ||
		snapshot.CreatedAt.IsZero() {
		return false
	}
	input := snapshot.JobTargetInputSnapshot
	candidate := snapshot.JobTargetCandidateSnapshot
	recommendation := candidate.CatalogRecommendation
	sourceShape := (candidate.Source == "quick_start" &&
		candidate.GeneralAdviceOnly &&
		strings.TrimSpace(input.JobTitle) != "" &&
		input.JobDescription == "") ||
		(candidate.Source == "job_description" &&
			!candidate.GeneralAdviceOnly &&
			strings.TrimSpace(input.JobDescription) != "")
	return input.Source == candidate.Source &&
		(candidate.Source == "job_description" ||
			candidate.Source == "quick_start") &&
		sourceShape &&
		strings.TrimSpace(candidate.JobTitle) != "" &&
		strings.TrimSpace(candidate.Seniority) != "" &&
		strings.TrimSpace(candidate.ScopeNotice) != "" &&
		validNonBlankContextTexts(candidate.Responsibilities) &&
		validNonBlankContextTexts(candidate.CoreSkills) &&
		validNonBlankContextTexts(candidate.CommunicationFocus) &&
		validNonBlankContextTexts(candidate.PracticeGoals) &&
		validContextResourceID(recommendation.ScenarioDefinitionID) &&
		recommendation.ScenarioDefinitionVersion > 0 &&
		validUniqueContextIDs(recommendation.SelectedRoleIDs) &&
		validContextResourceID(recommendation.PracticeOptionID) &&
		recommendation.PracticeOptionVersion > 0
}

func completeTargetedPlanPreview(plan persistence.Plan) bool {
	if plan.PreparationSnapshot == nil ||
		!validTargetedPreparationSnapshot(*plan.PreparationSnapshot) ||
		!completePlanConfiguration(plan) {
		return false
	}
	return plan.PreparationSnapshot.SourceProfileID ==
		plan.PreparationProfileID
}

func completePlanConfiguration(plan persistence.Plan) bool {
	if plan.CatalogSnapshot == nil || plan.SessionPolicy == nil ||
		len(plan.PracticeFocuses) == 0 {
		return false
	}
	catalog := plan.CatalogSnapshot
	return catalog.ScenarioDefinition.ID == plan.ScenarioDefinitionID &&
		catalog.ScenarioDefinition.Version ==
			plan.ScenarioDefinitionVersion &&
		catalog.ScenarioDefinition.Type == plan.ScenarioType &&
		catalog.ScenarioDefinition.Model == plan.ScenarioModel &&
		catalog.ScenarioConfig.ID == plan.ScenarioConfigID &&
		catalog.ScenarioConfig.Version == plan.ScenarioConfigVersion &&
		catalog.ScenarioConfig.Type == plan.ScenarioType &&
		catalog.ScenarioConfig.Model == plan.ScenarioModel &&
		equalContextStrings(
			roleSnapshotIDs(catalog.SelectedRoles),
			plan.SelectedRoleIDs,
		)
}

func compatibleTargetedStartRequest(
	plan persistence.Plan,
	request CreateSessionRequest,
) bool {
	if request.PreparationSnapshotID != "" &&
		request.PreparationSnapshotID != plan.PreparationSnapshot.ID {
		return false
	}
	if request.PracticeOptionID != "" &&
		request.PracticeOptionID != plan.CatalogSnapshot.PracticeOption.ID {
		return false
	}
	return len(request.RoleDefinitionIDs) == 0 ||
		equalContextStrings(
			request.RoleDefinitionIDs,
			plan.SelectedRoleIDs,
		)
}

func adjustedContextSessionPolicy(
	recommended persistence.ContextSessionPolicy,
	maxEffectiveTurns int,
) (persistence.ContextSessionPolicy, bool) {
	if maxEffectiveTurns < recommended.MinEffectiveTurns ||
		maxEffectiveTurns > recommended.MaxEffectiveTurns {
		return persistence.ContextSessionPolicy{}, false
	}
	result := cloneContextSessionPolicy(recommended)
	result.MaxEffectiveTurns = maxEffectiveTurns
	if result.CoverageCheckpointTurn > maxEffectiveTurns {
		result.CoverageCheckpointTurn = maxEffectiveTurns
	}
	return result, true
}

func planCatalogSnapshot(
	selection PlanCatalogSelection,
) persistence.PlanCatalogSnapshot {
	return persistence.PlanCatalogSnapshot{
		ScenarioDefinition: selection.ScenarioDefinition,
		ScenarioConfig: cloneScenarioConfigSnapshot(
			selection.ScenarioConfig,
		),
		SelectedRoles:  cloneRoleSnapshots(selection.SelectedRoles),
		PracticeOption: selection.PracticeOption,
	}
}

func roleSnapshotIDs(roles []persistence.RoleSnapshot) []string {
	ids := make([]string, len(roles))
	for index, role := range roles {
		ids[index] = role.ID
	}
	return ids
}

func clonePreparationSnapshot(
	source persistence.PreparationSnapshot,
) persistence.PreparationSnapshot {
	result := source
	if source.JobTargetInputSnapshot != nil {
		input := *source.JobTargetInputSnapshot
		result.JobTargetInputSnapshot = &input
	}
	if source.JobTargetCandidateSnapshot != nil {
		candidate := *source.JobTargetCandidateSnapshot
		candidate.Responsibilities = cloneStrings(source.
			JobTargetCandidateSnapshot.Responsibilities)
		candidate.CoreSkills = cloneStrings(source.
			JobTargetCandidateSnapshot.CoreSkills)
		candidate.CommunicationFocus = cloneStrings(source.
			JobTargetCandidateSnapshot.CommunicationFocus)
		candidate.PracticeGoals = cloneStrings(source.
			JobTargetCandidateSnapshot.PracticeGoals)
		candidate.CatalogRecommendation.SelectedRoleIDs = cloneStrings(
			source.JobTargetCandidateSnapshot.
				CatalogRecommendation.SelectedRoleIDs,
		)
		result.JobTargetCandidateSnapshot = &candidate
	}
	return result
}

func cloneScenarioConfigSnapshot(
	source persistence.ScenarioConfigSnapshot,
) persistence.ScenarioConfigSnapshot {
	result := source
	result.PromptModel.FocusAreas = cloneStrings(source.PromptModel.FocusAreas)
	result.PromptModel.TurnBlueprints = cloneStrings(
		source.PromptModel.TurnBlueprints,
	)
	result.FocusAreas = cloneStrings(source.FocusAreas)
	return result
}

func cloneRoleSnapshots(
	source []persistence.RoleSnapshot,
) []persistence.RoleSnapshot {
	result := make([]persistence.RoleSnapshot, len(source))
	for index, role := range source {
		result[index] = role
		result[index].FocusAreas = cloneStrings(role.FocusAreas)
	}
	return result
}

func clonePracticeObjectives(
	source []persistence.PracticeObjective,
) []persistence.PracticeObjective {
	return append([]persistence.PracticeObjective(nil), source...)
}

func cloneContextSessionPolicy(
	source persistence.ContextSessionPolicy,
) persistence.ContextSessionPolicy {
	result := source
	result.TargetObjectives = clonePracticeObjectives(source.TargetObjectives)
	return result
}

func (a *ContextApplication) applyIELTSQuestionSelection(
	plan persistence.Plan,
	request CreateSessionRequest,
	catalog SessionCatalogSelection,
) (
	SessionCatalogSelection,
	*persistence.IELTSPracticeAssignment,
	error,
) {
	if request.IELTSSelection == nil {
		if _, isIELTS := ieltsModeForScenario(plan.ScenarioDefinitionID); isIELTS {
			return SessionCatalogSelection{}, nil, persistence.ErrConflict
		}
		return catalog, nil, nil
	}
	expectedMode, isIELTS := ieltsModeForScenario(plan.ScenarioDefinitionID)
	if !isIELTS || request.IELTSSelection.Mode != expectedMode {
		return SessionCatalogSelection{}, nil, persistence.ErrConflict
	}
	reader, ok := a.catalog.(IELTSCatalogContextReader)
	if !ok {
		return SessionCatalogSelection{}, nil, persistence.ErrConflict
	}
	resolved, err := reader.ResolveIELTSQuestionSet(
		IELTSQuestionSetRequest{
			Mode:         request.IELTSSelection.Mode,
			Part1SetID:   request.IELTSSelection.Part1SetID,
			TopicGroupID: request.IELTSSelection.TopicGroupID,
		},
	)
	if err != nil || !validResolvedIELTSQuestionSet(expectedMode, resolved) {
		return SessionCatalogSelection{}, nil, persistence.ErrConflict
	}
	catalog.ScenarioConfig = cloneScenarioConfigSnapshot(
		catalog.ScenarioConfig,
	)
	catalog.ScenarioConfig.PromptModel.TurnBlueprints = cloneStrings(
		resolved.TurnBlueprints,
	)
	catalog.ScenarioConfig.PromptModel.PublicSceneBrief =
		ieltsSceneBrief(resolved)
	catalog.ScenarioConfig.PromptModel.SuggestedDurationSeconds =
		ieltsSuggestedDurationSeconds(expectedMode)
	assignment := &persistence.IELTSPracticeAssignment{
		BankID:         resolved.BankID,
		Season:         resolved.Season,
		Mode:           resolved.Mode,
		Part1SetID:     resolved.Part1SetID,
		TopicGroupID:   resolved.TopicGroupID,
		TopicTitle:     resolved.TopicTitle,
		Part2CueCard:   resolved.Part2CueCard,
		Part1Questions: resolved.Part1Questions,
		Part2Questions: resolved.Part2Questions,
		Part3Questions: resolved.Part3Questions,
		TurnBlueprints: cloneStrings(resolved.TurnBlueprints),
	}
	return catalog, assignment, nil
}

func ieltsModeForScenario(scenarioID string) (string, bool) {
	switch scenarioID {
	case "scn_ielts_speaking_full":
		return "FULL_MOCK", true
	case "scn_ielts_speaking_part_1":
		return "PART_1", true
	case "scn_ielts_speaking_part_2":
		return "PART_2", true
	case "scn_ielts_speaking_part_3":
		return "PART_3", true
	default:
		return "", false
	}
}

func validResolvedIELTSQuestionSet(
	mode string,
	resolved IELTSQuestionSetSelection,
) bool {
	if resolved.Mode != mode ||
		!validContextResourceID(resolved.BankID) ||
		strings.TrimSpace(resolved.Season) == "" ||
		len(resolved.TurnBlueprints) == 0 {
		return false
	}
	switch mode {
	case "FULL_MOCK":
		return validContextResourceID(resolved.Part1SetID) &&
			validContextResourceID(resolved.TopicGroupID) &&
			resolved.Part1Questions == 8 &&
			resolved.Part2Questions == 1 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 5 &&
			len(resolved.TurnBlueprints) ==
				9+resolved.Part3Questions
	case "PART_1":
		return validContextResourceID(resolved.Part1SetID) &&
			resolved.TopicGroupID == "" &&
			resolved.Part1Questions == 8 &&
			resolved.Part2Questions == 0 &&
			resolved.Part3Questions == 0 &&
			len(resolved.TurnBlueprints) == 8
	case "PART_2":
		return resolved.Part1SetID == "" &&
			validContextResourceID(resolved.TopicGroupID) &&
			resolved.Part1Questions == 0 &&
			resolved.Part2Questions == 1 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 5 &&
			len(resolved.TurnBlueprints) ==
				1+resolved.Part3Questions
	case "PART_3":
		return resolved.Part1SetID == "" &&
			validContextResourceID(resolved.TopicGroupID) &&
			resolved.Part1Questions == 0 &&
			resolved.Part2Questions == 0 &&
			resolved.Part3Questions >= 1 &&
			resolved.Part3Questions <= 5 &&
			len(resolved.TurnBlueprints) ==
				resolved.Part3Questions
	default:
		return false
	}
}

func ieltsSceneBrief(resolved IELTSQuestionSetSelection) string {
	switch resolved.Mode {
	case "FULL_MOCK":
		return "完成冻结的 Part 1 套题，并继续同主题 Part 2 与 Part 3。"
	case "PART_1":
		return "完成冻结的三个熟悉话题和八道 Part 1 问题。"
	case "PART_2":
		return "完成“" + resolved.TopicTitle + "”题卡，并可继续同主题 Part 3。"
	case "PART_3":
		return "围绕“" + resolved.TopicTitle + "”完成同主题 Part 3 讨论。"
	default:
		return "完成 IELTS 口语练习。"
	}
}

func ieltsSuggestedDurationSeconds(mode string) int {
	switch mode {
	case "FULL_MOCK":
		return 900
	case "PART_1", "PART_3":
		return 300
	case "PART_2":
		return 540
	default:
		return 600
	}
}

func equalContextStrings(left []string, right []string) bool {
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

func validNonBlankContextTexts(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value {
			return false
		}
	}
	return true
}

func defaultContextSessionPolicy(
	config persistence.ScenarioConfigSnapshot,
	option persistence.PracticeOptionSnapshot,
) persistence.ContextSessionPolicy {
	focusAreas := scenarioFocusAreas(config)
	objectives := make(
		[]persistence.PracticeObjective,
		0,
		len(focusAreas),
	)
	for _, focus := range focusAreas {
		objectives = append(objectives, persistence.PracticeObjective{
			ID:          focus,
			Description: objectiveDescription(focus),
		})
	}
	policy := persistence.ContextSessionPolicy{
		SuggestedDurationSeconds: config.PromptModel.SuggestedDurationSeconds,
		MinEffectiveTurns:        4,
		MaxEffectiveTurns:        6,
		CoverageCheckpointTurn:   4,
		MaxFollowUpsPerQuestion:  1,
		TargetObjectives:         objectives,
		EarlyCompletionRule:      "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
	}
	if policy.SuggestedDurationSeconds == 0 {
		policy.SuggestedDurationSeconds = 900
	}
	switch config.Model {
	case persistence.ScenarioModelIELTSSpeakingFullMock:
		turns := len(config.PromptModel.TurnBlueprints)
		if turns == 0 {
			turns = 14
		}
		policy.MinEffectiveTurns = turns
		policy.MaxEffectiveTurns = turns
		policy.CoverageCheckpointTurn = turns
		policy.MaxFollowUpsPerQuestion = 0
	case persistence.ScenarioModelIELTSSpeakingPart1:
		policy.MinEffectiveTurns = 8
		policy.MaxEffectiveTurns = 8
		policy.CoverageCheckpointTurn = 8
		policy.MaxFollowUpsPerQuestion = 0
	case persistence.ScenarioModelIELTSSpeakingPart2:
		turns := len(config.PromptModel.TurnBlueprints)
		if turns == 0 {
			turns = 6
		}
		policy.MinEffectiveTurns = turns
		policy.MaxEffectiveTurns = turns
		policy.CoverageCheckpointTurn = turns
		policy.MaxFollowUpsPerQuestion = 0
	case persistence.ScenarioModelIELTSSpeakingPart3:
		turns := len(config.PromptModel.TurnBlueprints)
		if turns == 0 {
			turns = 5
		}
		policy.MinEffectiveTurns = turns
		policy.MaxEffectiveTurns = turns
		policy.CoverageCheckpointTurn = turns
		policy.MaxFollowUpsPerQuestion = 0
	}
	if option.Type == "FOCUS" {
		policy.SuggestedDurationSeconds = 600
		policy.MinEffectiveTurns = 1
		policy.MaxEffectiveTurns = 3
		policy.CoverageCheckpointTurn = 1
	}
	return policy
}

func scenarioFocusAreas(
	config persistence.ScenarioConfigSnapshot,
) []string {
	if len(config.PromptModel.FocusAreas) > 0 {
		return config.PromptModel.FocusAreas
	}
	return config.FocusAreas
}

func validScenarioFamilyModel(
	family persistence.ScenarioFamily,
	model persistence.ScenarioModel,
) bool {
	switch family {
	case persistence.ScenarioFamilyInterview:
		return model == persistence.ScenarioModelProjectExperienceDeepDive ||
			model == persistence.ScenarioModelInterviewBasicDialogue
	case persistence.ScenarioFamilyExam:
		return model == persistence.ScenarioModelIELTSSpeakingPart1 ||
			model == persistence.ScenarioModelIELTSSpeakingPart2 ||
			model == persistence.ScenarioModelIELTSSpeakingPart3 ||
			model == persistence.ScenarioModelIELTSSpeakingFullMock ||
			model == persistence.ScenarioModelExamBasicDialogue
	case persistence.ScenarioFamilyWorkplace:
		return model == persistence.ScenarioModelProgressAndRiskUpdate ||
			model == persistence.ScenarioModelWorkplaceBasicDialogue
	case persistence.ScenarioFamilyDaily:
		return model ==
			persistence.ScenarioModelHotelCheckinAndIssueHandling ||
			model == persistence.ScenarioModelDailyBasicDialogue
	default:
		return false
	}
}

func contextPracticeFocuses(
	roles []persistence.RoleSnapshot,
) []persistence.PracticeObjective {
	seen := make(map[string]struct{})
	result := make([]persistence.PracticeObjective, 0)
	for _, role := range roles {
		for _, focus := range role.FocusAreas {
			if _, exists := seen[focus]; exists {
				continue
			}
			seen[focus] = struct{}{}
			result = append(result, persistence.PracticeObjective{
				ID:          focus,
				Description: objectiveDescription(focus),
			})
		}
	}
	return result
}

func objectiveDescription(id string) string {
	return fmt.Sprintf("Practice %s with concrete evidence.", strings.ReplaceAll(id, "_", " "))
}

func validContextResourceID(value string) bool {
	return utf8.ValidString(value) &&
		value != "" &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validUniqueContextIDs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validContextResourceID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validContextResourcePath(value string) bool {
	return strings.HasPrefix(value, "/") &&
		len(value) <= 1024 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func validContextIdempotencyKey(value string) bool {
	return len(value) >= 8 &&
		len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') &&
		strings.TrimSpace(value) == value
}

func contextTransitionWireAction(
	value persistence.ContextSessionTransition,
) (string, bool) {
	switch value {
	case persistence.ContextSessionPause:
		return "pause", true
	case persistence.ContextSessionResume:
		return "resume", true
	case persistence.ContextSessionEndEarly:
		return "end-early", true
	default:
		return "", false
	}
}

func contextActor(actor requestcontext.Actor) persistence.Actor {
	return persistence.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
