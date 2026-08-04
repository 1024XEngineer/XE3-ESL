package practice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type PracticeResourceIDGenerator interface {
	NewID() (string, error)
}

// ContextApplication owns only Practice Session orchestration. Preparation
// owns complete executable Plans and exposes them through PlanReader.
type ContextApplication struct {
	repository SessionRepository
	ids        PracticeResourceIDGenerator
	plans      preparation.PlanReader
}

func NewContextApplication(
	repository SessionRepository,
	ids PracticeResourceIDGenerator,
	plans preparation.PlanReader,
) (*ContextApplication, error) {
	if repository == nil || ids == nil || plans == nil {
		return nil, errors.New("practice: context dependency is required")
	}
	return &ContextApplication{
		repository: repository,
		ids:        ids,
		plans:      plans,
	}, nil
}

func (a *ContextApplication) CreateSession(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
	idempotencyKey string,
	request CreateSessionRequest,
) (SessionBootstrap, bool, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(planID) ||
		!validCreateSessionRequest(request) {
		return SessionBootstrap{}, false,
			ErrInvalidArgument
	}
	if !request.UserConfirmed {
		return SessionBootstrap{}, false,
			ErrConfirmationRequired
	}
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-plans/"+planID+"/practice-sessions",
		idempotencyKey,
		request,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	replayed, found, err := a.repository.ReplaySession(
		ctx,
		contextActor(actor),
		intent,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	if found {
		return replayed, true, nil
	}

	plan, err := a.plans.ReadExecutablePlan(
		ctx,
		actor,
		planID,
		request.ExpectedPlanRevision,
	)
	if err != nil {
		return SessionBootstrap{}, false,
			mapPlanReadError(err)
	}
	if !validExecutablePlan(plan, actor, planID, request.ExpectedPlanRevision) {
		return SessionBootstrap{}, false,
			ErrConflict
	}

	selection := cloneSceneSelection(plan.SceneSelection)
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return SessionBootstrap{}, false,
			ErrConflict
	}
	sessionID, snapshotID, participants, err := a.newSessionIdentities(
		actor,
		roles,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	snapshot := SessionSnapshot{
		ID:             snapshotID,
		SessionID:      sessionID,
		PlanRevision:   plan.Revision,
		SceneFamily:    selection.Scene.Family,
		SceneModel:     selection.Scene.Model,
		SceneSelection: selection,
		Preparation:    plan.PreparationSnapshot,
		Participants:   participants,
		SessionPolicy:  plan.SessionPolicy,
		PracticeObjectives: append(
			[]preparation.PracticeObjective(nil),
			plan.PracticeObjectives...,
		),
		IELTSAssignment: cloneIELTSAssignment(plan.IELTSAssignment),
	}
	return a.repository.CreateSession(
		ctx,
		contextActor(actor),
		CreateSessionCommand{
			SessionID:    sessionID,
			SnapshotID:   snapshotID,
			PlanID:       plan.ID,
			PlanRevision: plan.Revision,
			Snapshot:     snapshot,
			Intent:       intent,
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
		return ConfirmAndStartResult{}, ErrInvalidArgument
	}
	plan, err := a.plans.ReadExecutablePlan(
		ctx,
		actor,
		confirmation.PracticePlanID,
		confirmation.ExpectedPlanRevision,
	)
	if err != nil {
		return ConfirmAndStartResult{}, mapPlanReadError(err)
	}
	if !validExecutablePlan(
		plan,
		actor,
		confirmation.PracticePlanID,
		confirmation.ExpectedPlanRevision,
	) {
		return ConfirmAndStartResult{}, ErrConflict
	}
	if plan.SourceThreadID != confirmation.AgentThreadID {
		return ConfirmAndStartResult{}, ErrNotFound
	}

	bootstrap, replayed, err := a.CreateSession(
		ctx,
		actor,
		confirmation.PracticePlanID,
		idempotencyKey,
		CreateSessionRequest{
			ExpectedPlanRevision: confirmation.ExpectedPlanRevision,
			UserConfirmed:        true,
		},
	)
	if errors.Is(err, ErrActiveSessionConflict) {
		active, resolveErr := a.repository.ResolveSessionByPlan(
			ctx,
			contextActor(actor),
			confirmation.PracticePlanID,
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
	return ConfirmAndStartResult{Bootstrap: bootstrap, Replayed: replayed}, nil
}

func (a *ContextApplication) GetSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (Session, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) {
		return Session{}, ErrNotFound
	}
	return a.repository.GetSession(ctx, contextActor(actor), sessionID)
}

func (a *ContextApplication) GetSessionSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (SessionSnapshot, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) {
		return SessionSnapshot{}, ErrNotFound
	}
	return a.repository.GetSessionSnapshot(
		ctx,
		contextActor(actor),
		sessionID,
	)
}

func (a *ContextApplication) ResolveSessionByPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (SessionBootstrap, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(planID) {
		return SessionBootstrap{}, ErrNotFound
	}
	return a.repository.ResolveSessionByPlan(
		ctx,
		contextActor(actor),
		planID,
	)
}

func (a *ContextApplication) TransitionSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
	idempotencyKey string,
	expectedVersion int,
	transition SessionTransition,
) (Session, bool, error) {
	wireAction, validTransition := contextTransitionWireAction(transition)
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) ||
		expectedVersion < 1 || !validTransition {
		return Session{}, false,
			ErrInvalidArgument
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
		return Session{}, false, err
	}
	return a.repository.TransitionSession(
		ctx,
		contextActor(actor),
		TransitionSessionCommand{
			SessionID:              sessionID,
			ExpectedSessionVersion: expectedVersion,
			Transition:             transition,
			Intent:                 intent,
		},
	)
}

func (a *ContextApplication) newSessionIdentities(
	actor requestcontext.Actor,
	roles []scene.RoleDefinition,
) (string, string, []Participant, error) {
	sessionID, err := a.ids.NewID()
	if err != nil || !validContextResourceID(sessionID) {
		return "", "", nil, ErrConflict
	}
	snapshotID, err := a.ids.NewID()
	if err != nil || !validContextResourceID(snapshotID) {
		return "", "", nil, ErrConflict
	}
	participants := make([]Participant, 0, len(roles)+1)
	for _, role := range roles {
		participantID, err := a.ids.NewID()
		if err != nil || !validContextResourceID(participantID) {
			return "", "", nil, ErrConflict
		}
		roleCopy := cloneRoleDefinition(role)
		participants = append(participants, Participant{
			ID:        participantID,
			SessionID: sessionID,
			Role:      "FACILITATOR",
			SubjectRef: SubjectRef{
				Namespace: "speakup.role",
				SubjectID: role.ID,
			},
			RoleDefinitionID: role.ID,
			RoleSnapshot:     &roleCopy,
			Order:            len(participants) + 1,
		})
	}
	learnerID, err := a.ids.NewID()
	if err != nil || !validContextResourceID(learnerID) {
		return "", "", nil, ErrConflict
	}
	participants = append(participants, Participant{
		ID:        learnerID,
		SessionID: sessionID,
		Role:      "LEARNER",
		SubjectRef: SubjectRef{
			Namespace: "speakup.user",
			SubjectID: actor.UserID,
		},
		Order: len(participants) + 1,
	})
	return sessionID, snapshotID, participants, nil
}

func mapPlanReadError(err error) error {
	switch {
	case errors.Is(err, preparation.ErrPlanInvalid):
		return ErrInvalidArgument
	case errors.Is(err, preparation.ErrPlanNotFound):
		return ErrNotFound
	case errors.Is(err, preparation.ErrPlanConflict):
		return ErrConflict
	default:
		return err
	}
}

func validExecutablePlan(
	plan preparation.PracticePlan,
	actor requestcontext.Actor,
	planID string,
	revision int,
) bool {
	if plan.ID != planID || plan.UserID != actor.UserID ||
		plan.Revision != revision || plan.Status != preparation.PlanStatusReady ||
		plan.PreparationSnapshot.ID == "" ||
		plan.PreparationSnapshot.SourceProfileID == "" ||
		plan.PreparationSnapshot.SourceVersion < 1 ||
		plan.PreparationSnapshot.CreatedAt.IsZero() ||
		plan.SceneSelection.Scene.ID == "" ||
		plan.SceneSelection.Scene.Version < 1 ||
		plan.SceneSelection.Scene.Status != scene.SceneStatusActive ||
		plan.SessionPolicy.MinEffectiveTurns < 1 ||
		plan.SessionPolicy.MaxEffectiveTurns <
			plan.SessionPolicy.MinEffectiveTurns ||
		plan.SessionPolicy.CoverageCheckpointTurn < 1 ||
		plan.SessionPolicy.CoverageCheckpointTurn >
			plan.SessionPolicy.MaxEffectiveTurns ||
		plan.SessionPolicy.MaxFollowUpsPerQuestion < 0 ||
		len(plan.PracticeObjectives) == 0 {
		return false
	}
	if plan.SourceThreadID != "" &&
		!validContextResourceID(plan.SourceThreadID) {
		return false
	}
	if plan.GoalSnapshot != nil &&
		(!validContextResourceID(plan.GoalSnapshot.ID) ||
			plan.GoalSnapshot.Version < 1 ||
			strings.TrimSpace(plan.GoalSnapshot.Title) == "") {
		return false
	}
	if _, err := plan.SceneSelection.SelectedRoles(); err != nil {
		return false
	}
	if _, err := plan.SceneSelection.PracticeOption(); err != nil {
		return false
	}
	seen := make(map[string]struct{}, len(plan.PracticeObjectives))
	for _, objective := range plan.PracticeObjectives {
		if strings.TrimSpace(objective.ID) == "" ||
			strings.TrimSpace(objective.Description) == "" {
			return false
		}
		if _, duplicate := seen[objective.ID]; duplicate {
			return false
		}
		seen[objective.ID] = struct{}{}
	}
	return validFrozenIELTSPlan(plan)
}

func validCreateSessionRequest(request CreateSessionRequest) bool {
	return request.ExpectedPlanRevision > 0
}

func validFrozenIELTSPlan(plan preparation.PracticePlan) bool {
	assignment := plan.IELTSAssignment
	expectedMode, isIELTS := practiceIELTSMode(plan.SceneSelection.Scene)
	if !isIELTS {
		return assignment == nil
	}
	if assignment == nil || assignment.Mode != expectedMode ||
		!validContextResourceID(assignment.BankID) ||
		strings.TrimSpace(assignment.Season) == "" ||
		len(assignment.TurnBlueprints) == 0 ||
		!equalStrings(
			assignment.TurnBlueprints,
			plan.SceneSelection.Scene.Prompt.TurnBlueprints,
		) {
		return false
	}
	for _, blueprint := range assignment.TurnBlueprints {
		if strings.TrimSpace(blueprint) == "" {
			return false
		}
	}
	return true
}

func practiceIELTSMode(
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

func equalStrings(left, right []string) bool {
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

func cloneIELTSAssignment(
	source *preparation.IELTSAssignmentSnapshot,
) *preparation.IELTSAssignmentSnapshot {
	if source == nil {
		return nil
	}
	result := *source
	result.TurnBlueprints = cloneStrings(source.TurnBlueprints)
	return &result
}

func cloneSceneSelection(source scene.SelectionSnapshot) scene.SelectionSnapshot {
	result := source
	result.SelectedRoleIDs = cloneStrings(source.SelectedRoleIDs)
	result.Scene.Prompt.FocusAreas = cloneStrings(source.Scene.Prompt.FocusAreas)
	result.Scene.Prompt.TurnBlueprints = cloneStrings(
		source.Scene.Prompt.TurnBlueprints,
	)
	result.Scene.Roles = append(
		[]scene.RoleDefinition(nil),
		source.Scene.Roles...,
	)
	for index := range result.Scene.Roles {
		result.Scene.Roles[index].PracticeObjectives = append(
			[]scene.PracticeObjectiveDefinition(nil),
			source.Scene.Roles[index].PracticeObjectives...,
		)
	}
	result.Scene.PracticeOptions = append(
		[]scene.PracticeOption(nil),
		source.Scene.PracticeOptions...,
	)
	return result
}

func cloneRoleDefinition(source scene.RoleDefinition) scene.RoleDefinition {
	result := source
	result.PracticeObjectives = append(
		[]scene.PracticeObjectiveDefinition(nil),
		source.PracticeObjectives...,
	)
	return result
}

func newContextIntent(
	method string,
	path string,
	key string,
	payload any,
) (IdempotencyIntent, error) {
	if method != "POST" || !validContextResourcePath(path) ||
		!validContextIdempotencyKey(key) {
		return IdempotencyIntent{},
			ErrInvalidArgument
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return IdempotencyIntent{},
			ErrInvalidArgument
	}
	return IdempotencyIntent{
		Method:             method,
		CanonicalPath:      path,
		Key:                key,
		PayloadFingerprint: sha256.Sum256(canonical),
	}, nil
}

func validContextResourceID(value string) bool {
	return utf8.ValidString(value) && value != "" && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validContextResourcePath(value string) bool {
	return strings.HasPrefix(value, "/") && len(value) <= 1024 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validContextIdempotencyKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 &&
		!strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func contextTransitionWireAction(
	value SessionTransition,
) (string, bool) {
	switch value {
	case SessionPause:
		return "pause", true
	case SessionResume:
		return "resume", true
	case SessionEndEarly:
		return "end-early", true
	default:
		return "", false
	}
}

func contextActor(actor requestcontext.Actor) Actor {
	return Actor{UserID: actor.UserID, SessionID: actor.SessionID}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
