package practice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type PracticeResourceIDGenerator interface {
	NewID() (string, error)
}

// SessionApplication owns Practice Session orchestration from one immutable
// Plan projection through the authoritative frozen Session snapshot.
type SessionApplication struct {
	repository SessionRepository
	ids        PracticeResourceIDGenerator
	plans      PlanProjectionReader
}

func NewSessionApplication(
	repository SessionRepository,
	ids PracticeResourceIDGenerator,
	plans PlanProjectionReader,
) (*SessionApplication, error) {
	if repository == nil || ids == nil || plans == nil {
		return nil, errors.New("practice: session dependencies are required")
	}
	return &SessionApplication{
		repository: repository,
		ids:        ids,
		plans:      plans,
	}, nil
}

func (a *SessionApplication) CreateSession(
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
	intent, err := newContextIntent(
		"POST",
		"/v1/practice-plans/"+planID+"/practice-sessions",
		idempotencyKey,
		request,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	plan, err := a.plans.ReadExecutablePlan(
		ctx,
		actor,
		planID,
		request.ExpectedPlanVersion,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	if !validExecutablePlan(plan, actor, planID, request.ExpectedPlanVersion) {
		return SessionBootstrap{}, false,
			ErrConflict
	}

	selection := cloneSceneSelection(plan.SceneSelection)
	roles, err := selection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return SessionBootstrap{}, false,
			ErrConflict
	}
	option, err := selection.PracticeOption()
	if err != nil {
		return SessionBootstrap{}, false, ErrConflict
	}
	sessionID, participants, err := a.newSessionIdentities(
		actor,
		roles,
	)
	if err != nil {
		return SessionBootstrap{}, false, err
	}
	snapshot := SessionSnapshot{
		SessionID:      sessionID,
		PlanVersion:    plan.Version,
		Experience:     selection.Scene.Experience,
		Category:       selection.Scene.Category,
		PracticeMode:   option.Mode,
		SceneSelection: selection,
		Preparation:    plan.Preparation,
		Participants:   participants,
		SessionPolicy:  plan.SessionPolicy,
		PracticeObjectives: append(
			[]PracticeObjective(nil),
			plan.PracticeObjectives...,
		),
		IELTSAssignment: cloneIELTSAssignment(plan.IELTSAssignment),
	}
	return a.repository.CreateSession(
		ctx,
		contextActor(actor),
		CreateSessionCommand{
			SessionID:          sessionID,
			PlanID:             plan.ID,
			PlanVersion:        plan.Version,
			Snapshot:           snapshot,
			ClientRequestID:    intent.Key,
			RequestFingerprint: intent.PayloadFingerprint,
		},
	)
}

func (a *SessionApplication) GetSession(
	ctx context.Context,
	actor requestcontext.Actor,
	sessionID string,
) (Session, error) {
	if ctx == nil || !actor.Valid() || !validContextResourceID(sessionID) {
		return Session{}, ErrNotFound
	}
	return a.repository.GetSession(ctx, contextActor(actor), sessionID)
}

func (a *SessionApplication) GetSessionSnapshot(
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

func (a *SessionApplication) TransitionSession(
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
			ClientRequestID:        intent.Key,
			RequestFingerprint:     intent.PayloadFingerprint,
		},
	)
}

func (a *SessionApplication) newSessionIdentities(
	actor requestcontext.Actor,
	roles []RoleDefinition,
) (string, []Participant, error) {
	sessionID, err := a.ids.NewID()
	if err != nil || !validContextResourceID(sessionID) {
		return "", nil, ErrConflict
	}
	participants := make([]Participant, 0, len(roles)+1)
	for _, role := range roles {
		participantID, err := a.ids.NewID()
		if err != nil || !validContextResourceID(participantID) {
			return "", nil, ErrConflict
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
		return "", nil, ErrConflict
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
	return sessionID, participants, nil
}

func validExecutablePlan(
	plan PlanProjection,
	actor requestcontext.Actor,
	planID string,
	version int,
) bool {
	if plan.ID != planID || plan.OwnerUserID != actor.UserID ||
		plan.Version != version ||
		plan.SceneSelection.Scene.ID == "" ||
		plan.SceneSelection.Scene.Version < 1 ||
		plan.SceneSelection.Scene.Status != SceneStatusActive ||
		len(plan.PracticeObjectives) == 0 {
		return false
	}
	roles, err := plan.SceneSelection.SelectedRoles()
	if err != nil || len(roles) == 0 {
		return false
	}
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil || option.SceneID != plan.SceneSelection.Scene.ID ||
		!validEvaluationPolicyRef(option.EvaluationPolicyRef) {
		return false
	}
	if _, err := ResolveTurnPolicy(
		option.TurnPolicyRef,
	); err != nil || !ValidSessionPolicy(
		option.SessionPolicyRef,
		option.Mode,
		len(plan.SceneSelection.Scene.Prompt.TurnBlueprints),
		option.SuggestedDurationSeconds,
		plan.SessionPolicy,
	) {
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
	return request.ExpectedPlanVersion > 0
}

func validFrozenIELTSPlan(plan PlanProjection) bool {
	assignment := plan.IELTSAssignment
	option, err := plan.SceneSelection.PracticeOption()
	if err != nil {
		return false
	}
	turnPolicy, err := ResolveTurnPolicy(
		option.TurnPolicyRef,
	)
	if err != nil {
		return false
	}
	if turnPolicy.Kind != TurnPolicyFrozenIELTS {
		return assignment == nil
	}
	return ValidIELTSAssignment(
		assignment,
		turnPolicy.Mode,
		plan.SceneSelection.Scene.Prompt.TurnBlueprints,
	)
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
	source *IELTSAssignment,
) *IELTSAssignment {
	if source == nil {
		return nil
	}
	result := *source
	result.Parts = make([]IELTSPart, len(source.Parts))
	for index, part := range source.Parts {
		result.Parts[index] = part
		result.Parts[index].TurnBlueprints = cloneStrings(
			part.TurnBlueprints,
		)
		result.Parts[index].PreparedAnswers = append(
			[]IELTSPreparedAnswer(nil),
			part.PreparedAnswers...,
		)
	}
	return &result
}

func cloneSceneSelection(source SceneSelection) SceneSelection {
	result := source
	result.SelectedRoleIDs = cloneStrings(source.SelectedRoleIDs)
	result.Scene.Prompt.FocusAreas = cloneStrings(source.Scene.Prompt.FocusAreas)
	result.Scene.Prompt.TurnBlueprints = cloneStrings(
		source.Scene.Prompt.TurnBlueprints,
	)
	result.Scene.Roles = append(
		[]RoleDefinition(nil),
		source.Scene.Roles...,
	)
	for index := range result.Scene.Roles {
		result.Scene.Roles[index].PracticeObjectives = append(
			[]PracticeObjectiveDefinition(nil),
			source.Scene.Roles[index].PracticeObjectives...,
		)
	}
	result.Scene.PracticeOptions = append(
		[]PracticeOption(nil),
		source.Scene.PracticeOptions...,
	)
	return result
}

func cloneRoleDefinition(source RoleDefinition) RoleDefinition {
	result := source
	result.PracticeObjectives = append(
		[]PracticeObjectiveDefinition(nil),
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
	return ValidAggregateID(value)
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
	case SessionComplete:
		return "complete", true
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
