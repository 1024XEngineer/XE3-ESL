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
}

type PlanCatalogSelection struct {
	ScenarioDefinition persistence.ScenarioDefinitionSnapshot
	ScenarioConfig     persistence.ScenarioConfigSnapshot
	SelectedRoles      []persistence.RoleSnapshot
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
	profile, err := a.preparation.ReadPreparationProfile(
		ctx,
		actor,
		request.PreparationProfileID,
	)
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if profile.ID != request.PreparationProfileID || profile.Version < 1 {
		return persistence.Plan{}, false, persistence.ErrNotFound
	}
	catalog, err := a.catalog.ReadPlanCatalog(PlanCatalogRequest{
		ScenarioDefinitionID:      request.ScenarioDefinitionID,
		ScenarioDefinitionVersion: request.ScenarioDefinitionVersion,
		ScenarioConfigID:          request.ScenarioConfigID,
		ScenarioConfigVersion:     request.ScenarioConfigVersion,
		SelectedRoleIDs:           cloneStrings(request.SelectedRoleIDs),
	})
	if err != nil {
		return persistence.Plan{}, false, err
	}
	if !validPlanCatalogSelection(request, catalog) {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	planID, err := a.ids.NewID()
	if err != nil {
		return persistence.Plan{}, false, persistence.ErrConflict
	}
	return a.repository.CreatePlan(ctx, contextActor(actor), persistence.CreatePlanCommand{
		PlanID:                    planID,
		AgentThreadID:             request.AgentThreadID,
		MatterID:                  request.MatterID,
		ScenarioDefinitionID:      request.ScenarioDefinitionID,
		ScenarioDefinitionVersion: request.ScenarioDefinitionVersion,
		ScenarioType:              catalog.ScenarioDefinition.Type,
		ScenarioConfigID:          request.ScenarioConfigID,
		ScenarioConfigVersion:     request.ScenarioConfigVersion,
		PreparationProfileID:      request.PreparationProfileID,
		SelectedRoleIDs:           cloneStrings(request.SelectedRoleIDs),
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
	if plan.ScenarioType == "INTERVIEW" &&
		len(request.RoleDefinitionIDs) != 1 {
		return persistence.ContextSessionBootstrap{}, false,
			persistence.ErrConflict
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
	preparationSnapshot, err := a.preparation.ReadPreparationSnapshot(
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
	catalog, err := a.catalog.ReadSessionCatalog(SessionCatalogRequest{
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
		ScenarioDefinition: catalog.ScenarioDefinition,
		ScenarioConfig:     catalog.ScenarioConfig,
		Preparation:        preparationSnapshot,
		Participants:       participants,
		PracticeOption:     catalog.PracticeOption,
		SessionPolicy: defaultContextSessionPolicy(
			catalog.ScenarioConfig,
			catalog.PracticeOption,
		),
		PracticeFocuses: contextPracticeFocuses(catalog.SelectedRoles),
	}
	return a.repository.CreateContextSession(
		ctx,
		contextActor(actor),
		persistence.CreateContextSessionCommand{
			SessionID:             sessionID,
			SnapshotID:            snapshotID,
			PlanID:                plan.ID,
			ExpectedPlanRevision:  request.ExpectedPlanRevision,
			PreparationSnapshotID: request.PreparationSnapshotID,
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
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) ||
		expectedVersion < 1 || !validContextTransition(transition) {
		return persistence.ContextSession{}, false,
			persistence.ErrInvalidArgument
	}
	payload := struct {
		ExpectedSessionVersion int `json:"expected_session_version"`
	}{ExpectedSessionVersion: expectedVersion}
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-sessions/"+sessionID+"/"+string(transition),
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
			Role:      "INTERVIEWER",
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
		Role:      "CANDIDATE",
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
	if method != "POST" || !validContextResourcePath(path) ||
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
	return validContextResourceID(request.AgentThreadID) &&
		validContextResourceID(request.MatterID) &&
		validContextResourceID(request.ScenarioDefinitionID) &&
		request.ScenarioDefinitionVersion > 0 &&
		validContextResourceID(request.ScenarioConfigID) &&
		request.ScenarioConfigVersion > 0 &&
		validContextResourceID(request.PreparationProfileID) &&
		validUniqueContextIDs(request.SelectedRoleIDs)
}

func validCreateSessionRequest(request CreateSessionRequest) bool {
	return request.ExpectedPlanRevision > 0 &&
		validContextResourceID(request.PreparationSnapshotID) &&
		validContextResourceID(request.PracticeOptionID) &&
		validUniqueContextIDs(request.RoleDefinitionIDs)
}

func validPlanCatalogSelection(
	request CreatePlanRequest,
	selection PlanCatalogSelection,
) bool {
	if selection.ScenarioDefinition.ID != request.ScenarioDefinitionID ||
		selection.ScenarioDefinition.Version !=
			request.ScenarioDefinitionVersion ||
		selection.ScenarioDefinition.Status != "active" ||
		selection.ScenarioConfig.ID != request.ScenarioConfigID ||
		selection.ScenarioConfig.Version != request.ScenarioConfigVersion ||
		selection.ScenarioConfig.ScenarioDefinitionID !=
			request.ScenarioDefinitionID ||
		selection.ScenarioConfig.Type != selection.ScenarioDefinition.Type ||
		len(selection.SelectedRoles) != len(request.SelectedRoleIDs) {
		return false
	}
	for index, role := range selection.SelectedRoles {
		if role.ID != request.SelectedRoleIDs[index] ||
			role.ScenarioDefinitionID != request.ScenarioDefinitionID ||
			role.Version < 1 {
			return false
		}
	}
	return true
}

func validSessionCatalogSelection(
	plan persistence.Plan,
	request CreateSessionRequest,
	selection SessionCatalogSelection,
) bool {
	if !validPlanCatalogSelection(CreatePlanRequest{
		AgentThreadID:             plan.AgentThreadID,
		MatterID:                  plan.MatterID,
		ScenarioDefinitionID:      plan.ScenarioDefinitionID,
		ScenarioDefinitionVersion: plan.ScenarioDefinitionVersion,
		ScenarioConfigID:          plan.ScenarioConfigID,
		ScenarioConfigVersion:     plan.ScenarioConfigVersion,
		PreparationProfileID:      plan.PreparationProfileID,
		SelectedRoleIDs:           request.RoleDefinitionIDs,
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

func defaultContextSessionPolicy(
	config persistence.ScenarioConfigSnapshot,
	option persistence.PracticeOptionSnapshot,
) persistence.ContextSessionPolicy {
	objectives := make([]persistence.PracticeObjective, 0, len(config.FocusAreas))
	for _, focus := range config.FocusAreas {
		objectives = append(objectives, persistence.PracticeObjective{
			ID:          focus,
			Description: objectiveDescription(focus),
		})
	}
	policy := persistence.ContextSessionPolicy{
		SuggestedDurationSeconds: 900,
		MinEffectiveTurns:        4,
		MaxEffectiveTurns:        6,
		CoverageCheckpointTurn:   4,
		MaxFollowUpsPerQuestion:  1,
		TargetObjectives:         objectives,
		EarlyCompletionRule:      "COVERAGE_SATISFIED_AFTER_CHECKPOINT",
	}
	if option.Type == "FOCUS" {
		policy.SuggestedDurationSeconds = 600
		policy.MinEffectiveTurns = 1
		policy.MaxEffectiveTurns = 3
		policy.CoverageCheckpointTurn = 1
	}
	return policy
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

func validContextTransition(value persistence.ContextSessionTransition) bool {
	return value == persistence.ContextSessionPause ||
		value == persistence.ContextSessionResume ||
		value == persistence.ContextSessionEndEarly
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
