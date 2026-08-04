package evaluation

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const SchemaVersion = "evaluation-schema/1.0.0"

var (
	ErrInvalidRequest          = errors.New("evaluation: invalid request")
	ErrNotFound                = errors.New("evaluation: not found")
	ErrIdempotencyConflict     = errors.New("evaluation: idempotency conflict")
	ErrAccountUnavailable      = errors.New("evaluation: account unavailable")
	ErrDeletionGenerationStale = errors.New(
		"evaluation: deletion generation stale",
	)
)

type Channel string

const (
	ChannelScene  Channel = "SCENE"
	ChannelCore4D Channel = "CORE_4D"
)

type Scope string

const (
	ScopeTurn    Scope = "TURN"
	ScopeSession Scope = "SESSION"
)

type SceneType string

const (
	SceneIELTSSpeaking     SceneType = "IELTS_SPEAKING"
	SceneInterview         SceneType = "INTERVIEW"
	SceneOverseasDaily     SceneType = "OVERSEAS_DAILY_LIFE"
	SceneOverseasWorkplace SceneType = "OVERSEAS_WORKPLACE"
)

type Status string

const (
	StatusReceived     Status = "RECEIVED"
	StatusValidating   Status = "VALIDATING"
	StatusQueued       Status = "QUEUED"
	StatusRunning      Status = "RUNNING"
	StatusPartialReady Status = "PARTIAL_READY"
	StatusReady        Status = "READY"
	StatusFailed       Status = "FAILED"
	StatusSuperseded   Status = "SUPERSEDED"
)

type CreateRequest struct {
	PracticeSessionID string
	InputSnapshotID   string
	InputRevision     int
	Scope             Scope
	SceneType         SceneType
	Channels          []Channel
	SceneStrategyRef  string
	Core4DStrategyRef string
	PipelineVersion   string
	ClientRequestID   string
}

type ReevaluateRequest struct {
	Channels          []Channel
	SceneStrategyRef  string
	Core4DStrategyRef string
	PipelineVersion   string
	ClientRequestID   string
}

type Evaluation struct {
	ID                string
	OwnerUserID       string
	PracticeSessionID string
	InputSnapshotID   string
	InputRevision     int
	Scope             Scope
	SceneType         SceneType
	Revision          Revision
	CreatedAt         time.Time
}

type Revision struct {
	ID                   string
	EvaluationID         string
	OwnerUserID          string
	Number               int
	SupersedesRevisionID string
	Channels             []Channel
	SceneStrategyRef     string
	Core4DStrategyRef    string
	PipelineVersion      string
	SchemaVersion        string
	Status               Status
	IsFinal              bool
	ClientRequestID      string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	CompletedAt          *time.Time
}

func (e Evaluation) Valid() bool {
	return validUUID(e.ID) &&
		validUUID(e.OwnerUserID) &&
		validIdentifier(e.PracticeSessionID) &&
		validIdentifier(e.InputSnapshotID) &&
		e.InputRevision > 0 &&
		validScope(e.Scope) &&
		validSceneType(e.SceneType) &&
		e.Revision.Valid() &&
		e.Revision.EvaluationID == e.ID &&
		e.Revision.OwnerUserID == e.OwnerUserID &&
		!e.CreatedAt.IsZero()
}

func (r Revision) Valid() bool {
	return validUUID(r.ID) &&
		validUUID(r.EvaluationID) &&
		validUUID(r.OwnerUserID) &&
		r.Number > 0 &&
		((r.Number == 1 && r.SupersedesRevisionID == "") ||
			(r.Number > 1 && validUUID(r.SupersedesRevisionID))) &&
		validChannels(r.Channels) &&
		validStrategies(
			r.Channels,
			r.SceneStrategyRef,
			r.Core4DStrategyRef,
		) &&
		validVersion(r.PipelineVersion) &&
		validVersion(r.SchemaVersion) &&
		validStatus(r.Status) &&
		validCompletion(r.Status, r.CompletedAt) &&
		validClientRequestID(r.ClientRequestID) &&
		!r.CreatedAt.IsZero() &&
		!r.UpdatedAt.Before(r.CreatedAt)
}

type RevisionConfig struct {
	Channels          []Channel `json:"channels"`
	SceneStrategyRef  string    `json:"scene_strategy_ref,omitempty"`
	Core4DStrategyRef string    `json:"core_4d_strategy_ref,omitempty"`
	PipelineVersion   string    `json:"pipeline_version"`
	SchemaVersion     string    `json:"schema_version"`
	ClientRequestID   string    `json:"-"`
}

func (config RevisionConfig) Valid() bool {
	return validChannels(config.Channels) &&
		validStrategies(
			config.Channels,
			config.SceneStrategyRef,
			config.Core4DStrategyRef,
		) &&
		validVersion(config.PipelineVersion) &&
		config.SchemaVersion == SchemaVersion &&
		validClientRequestID(config.ClientRequestID)
}

type CreateInput struct {
	PracticeSessionID string         `json:"practice_session_id"`
	InputSnapshotID   string         `json:"input_snapshot_id"`
	InputRevision     int            `json:"input_revision"`
	Scope             Scope          `json:"scope"`
	SceneType         SceneType      `json:"scene_type"`
	Config            RevisionConfig `json:"config"`
}

func (input CreateInput) Valid() bool {
	return validIdentifier(input.PracticeSessionID) &&
		validIdentifier(input.InputSnapshotID) &&
		input.InputRevision > 0 &&
		validScope(input.Scope) &&
		validSceneType(input.SceneType) &&
		input.Config.Valid()
}

func normalizeCreate(request CreateRequest) (CreateInput, error) {
	config, err := normalizeConfig(
		request.Channels,
		request.SceneStrategyRef,
		request.Core4DStrategyRef,
		request.PipelineVersion,
		request.ClientRequestID,
	)
	if err != nil ||
		!validIdentifier(request.PracticeSessionID) ||
		!validIdentifier(request.InputSnapshotID) ||
		request.InputRevision < 1 ||
		!validScope(request.Scope) ||
		!validSceneType(request.SceneType) {
		return CreateInput{}, ErrInvalidRequest
	}
	return CreateInput{
		PracticeSessionID: strings.TrimSpace(request.PracticeSessionID),
		InputSnapshotID:   strings.TrimSpace(request.InputSnapshotID),
		InputRevision:     request.InputRevision,
		Scope:             request.Scope,
		SceneType:         request.SceneType,
		Config:            config,
	}, nil
}

func normalizeReevaluation(
	request ReevaluateRequest,
) (RevisionConfig, error) {
	return normalizeConfig(
		request.Channels,
		request.SceneStrategyRef,
		request.Core4DStrategyRef,
		request.PipelineVersion,
		request.ClientRequestID,
	)
}

func normalizeConfig(
	channels []Channel,
	sceneStrategyRef string,
	core4DStrategyRef string,
	pipelineVersion string,
	clientRequestID string,
) (RevisionConfig, error) {
	normalizedChannels := slices.Clone(channels)
	slices.Sort(normalizedChannels)
	sceneStrategyRef = strings.TrimSpace(sceneStrategyRef)
	core4DStrategyRef = strings.TrimSpace(core4DStrategyRef)
	pipelineVersion = strings.TrimSpace(pipelineVersion)
	clientRequestID = strings.TrimSpace(clientRequestID)
	if !validChannels(normalizedChannels) ||
		!validStrategies(
			normalizedChannels,
			sceneStrategyRef,
			core4DStrategyRef,
		) ||
		!validVersion(pipelineVersion) ||
		!validClientRequestID(clientRequestID) {
		return RevisionConfig{}, ErrInvalidRequest
	}
	return RevisionConfig{
		Channels:          normalizedChannels,
		SceneStrategyRef:  sceneStrategyRef,
		Core4DStrategyRef: core4DStrategyRef,
		PipelineVersion:   pipelineVersion,
		SchemaVersion:     SchemaVersion,
		ClientRequestID:   clientRequestID,
	}, nil
}

func validActor(actor requestcontext.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
}

func validChannels(channels []Channel) bool {
	if len(channels) < 1 || len(channels) > 2 {
		return false
	}
	seen := make(map[Channel]struct{}, len(channels))
	for _, channel := range channels {
		if channel != ChannelScene && channel != ChannelCore4D {
			return false
		}
		if _, exists := seen[channel]; exists {
			return false
		}
		seen[channel] = struct{}{}
	}
	return true
}

func validStrategies(
	channels []Channel,
	sceneStrategyRef string,
	core4DStrategyRef string,
) bool {
	hasScene := slices.Contains(channels, ChannelScene)
	hasCore4D := slices.Contains(channels, ChannelCore4D)
	return ((hasScene && validVersion(sceneStrategyRef)) ||
		(!hasScene && sceneStrategyRef == "")) &&
		((hasCore4D && validVersion(core4DStrategyRef)) ||
			(!hasCore4D && core4DStrategyRef == ""))
}

func validScope(scope Scope) bool {
	return scope == ScopeTurn || scope == ScopeSession
}

func validSceneType(sceneType SceneType) bool {
	switch sceneType {
	case SceneIELTSSpeaking,
		SceneInterview,
		SceneOverseasDaily,
		SceneOverseasWorkplace:
		return true
	default:
		return false
	}
}

func validStatus(status Status) bool {
	switch status {
	case StatusReceived,
		StatusValidating,
		StatusQueued,
		StatusRunning,
		StatusPartialReady,
		StatusReady,
		StatusFailed,
		StatusSuperseded:
		return true
	default:
		return false
	}
}

func validCompletion(status Status, completedAt *time.Time) bool {
	switch status {
	case StatusReady, StatusFailed, StatusSuperseded:
		return completedAt != nil && !completedAt.IsZero()
	default:
		return completedAt == nil
	}
}

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
	identifierPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	versionPattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9._:/-]{0,127}$`,
	)
	clientRequestPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func validIdentifier(value string) bool {
	return identifierPattern.MatchString(strings.TrimSpace(value))
}

func validVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}

func validClientRequestID(value string) bool {
	return value == "" || clientRequestPattern.MatchString(value)
}
