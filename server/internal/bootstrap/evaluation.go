package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
	evaluationtransport "github.com/1024XEngineer/XE3-ESL/server/internal/evaluation/transport"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	interviewShadowAggregationVersion = "interview-shadow-aggregation/v1"
	interviewShadowCalibrationVersion = "NOT_CONFIGURED"
	interviewShadowGateVersion        = "interview-shadow-gate/v1"
)

type EvaluationConfiguration struct {
	Provider          string
	Model             string
	MaxOutputTokens   int
	GenerationTimeout time.Duration
	LeaseDuration     time.Duration
	MaxAttempts       int
}

type EvaluationComposition struct {
	coordinator *evaluation.InterviewShadowCoordinator
	handler     *evaluationtransport.HTTPHandler
	worker      *evaluation.InterviewShadowWorker
}

func NewEvaluationComposition(
	database *pgxpool.Pool,
	textGenerator ai.TextGenerator,
	configuration EvaluationConfiguration,
) (*EvaluationComposition, error) {
	if database == nil || textGenerator == nil ||
		configuration.Provider != "qianwen" ||
		configuration.Model == "" ||
		configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 ||
		configuration.GenerationTimeout <= 0 ||
		configuration.GenerationTimeout >
			interviewShadowGenerationTimeout ||
		configuration.LeaseDuration < time.Second ||
		configuration.LeaseDuration > 10*time.Minute ||
		configuration.MaxAttempts < 1 ||
		configuration.MaxAttempts > 10 {
		return nil, errors.New(
			"bootstrap: Evaluation dependencies are required",
		)
	}
	practiceRepository := practicepostgres.New(database)
	conversationRepository, err := postgres.New(database)
	if err != nil {
		return nil, err
	}
	audioRepository, err := postgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	evidenceSource, err := evaluation.NewEvidenceSourceReader(
		practiceRepository,
		conversationRepository,
		audioRepository,
	)
	if err != nil {
		return nil, err
	}
	repository := evaluation.NewPostgresRepository(database)
	evidenceService := evaluation.NewEvidenceSnapshotService(
		evidenceSource,
		repository,
	)
	evaluationService := evaluation.NewService(repository, repository)
	coordinator, err := evaluation.NewInterviewShadowCoordinator(
		evidenceService,
		evaluationService,
	)
	if err != nil {
		return nil, err
	}
	provider, err := newInterviewShadowTextProvider(
		textGenerator,
		configuration.GenerationTimeout,
	)
	if err != nil {
		return nil, err
	}
	runtimeConfiguration, err := interviewShadowRuntimeConfiguration(
		configuration,
	)
	if err != nil {
		return nil, err
	}
	worker, err := evaluation.NewInterviewShadowWorker(
		repository,
		evaluation.NewInterviewShadowEngine(provider),
		runtimeConfiguration,
	)
	if err != nil {
		return nil, err
	}
	application := &evaluationHTTPApplication{
		evaluations:   evaluationService,
		runtime:       repository,
		configuration: runtimeConfiguration,
	}
	handler, err := evaluationtransport.NewHTTPHandler(application)
	if err != nil {
		return nil, err
	}
	return &EvaluationComposition{
		coordinator: coordinator,
		handler:     handler,
		worker:      worker,
	}, nil
}

func (composition *EvaluationComposition) InterviewShadowCoordinator() *evaluation.InterviewShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.coordinator
}

func (composition *EvaluationComposition) HTTPHandler() *evaluationtransport.HTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.handler
}

func (composition *EvaluationComposition) Worker() *evaluation.InterviewShadowWorker {
	if composition == nil {
		return nil
	}
	return composition.worker
}

func interviewShadowRuntimeConfiguration(
	configuration EvaluationConfiguration,
) (evaluation.InterviewShadowRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return evaluation.InterviewShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(interviewShadowSystemContract),
	)
	manifest := struct {
		SchemaVersion         string `json:"schema_version"`
		StrategyRef           string `json:"strategy_ref"`
		PipelineVersion       string `json:"pipeline_version"`
		PromptVersion         string `json:"prompt_version"`
		PromptContractHash    string `json:"prompt_contract_hash"`
		ProviderSchemaVersion string `json:"provider_schema_version"`
		GateVersion           string `json:"gate_version"`
		AggregationVersion    string `json:"aggregation_version"`
		CalibrationVersion    string `json:"calibration_version"`
		Provider              string `json:"provider"`
		Model                 string `json:"model"`
		MaxOutputTokens       int    `json:"max_output_tokens"`
	}{
		SchemaVersion:   evaluation.SchemaVersion,
		StrategyRef:     evaluation.InterviewShadowStrategyRef,
		PipelineVersion: evaluation.InterviewShadowPipelineVersion,
		PromptVersion:   evaluation.InterviewShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: evaluation.InterviewShadowProviderSchemaVersion,
		GateVersion:           interviewShadowGateVersion,
		AggregationVersion:    interviewShadowAggregationVersion,
		CalibrationVersion:    interviewShadowCalibrationVersion,
		Provider:              configuration.Provider,
		Model:                 configuration.Model,
		MaxOutputTokens:       configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return evaluation.InterviewShadowRuntimeConfiguration{}, err
	}
	result := evaluation.InterviewShadowRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     evaluation.InterviewShadowStrategyRef,
		PipelineVersion: evaluation.InterviewShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   evaluation.InterviewShadowPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return evaluation.InterviewShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	return result, nil
}

type evaluationService interface {
	Create(
		context.Context,
		requestcontext.Actor,
		evaluation.CreateRequest,
	) (evaluation.Evaluation, bool, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (evaluation.Evaluation, error)
	Reevaluate(
		context.Context,
		requestcontext.Actor,
		string,
		evaluation.ReevaluateRequest,
	) (evaluation.Evaluation, bool, error)
}

type evaluationRuntimeReader interface {
	GetInterviewShadowState(
		context.Context,
		string,
		string,
		string,
	) (evaluation.InterviewShadowReadState, error)
}

type evaluationHTTPApplication struct {
	evaluations   evaluationService
	runtime       evaluationRuntimeReader
	configuration evaluation.InterviewShadowRuntimeConfiguration
}

func (application *evaluationHTTPApplication) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	request evaluation.CreateRequest,
) (evaluationtransport.EvaluationAccepted, error) {
	if application == nil || application.evaluations == nil ||
		!application.configuration.Valid() {
		return evaluationtransport.EvaluationAccepted{},
			evaluation.ErrInvalidRequest
	}
	if err := evaluation.ValidateInterviewShadowCreateRequest(
		request,
	); err != nil {
		return evaluationtransport.EvaluationAccepted{},
			interviewShadowStrategyError()
	}
	value, replayed, err := application.evaluations.Create(ctx, actor, request)
	if err != nil {
		return evaluationtransport.EvaluationAccepted{}, err
	}
	return interviewShadowAccepted(value, replayed)
}

func (application *evaluationHTTPApplication) Reevaluate(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
	request evaluation.ReevaluateRequest,
) (evaluationtransport.EvaluationAccepted, error) {
	if application == nil || application.evaluations == nil ||
		!application.configuration.Valid() {
		return evaluationtransport.EvaluationAccepted{},
			evaluation.ErrInvalidRequest
	}
	if err := evaluation.ValidateInterviewShadowReevaluateRequest(
		request,
	); err != nil {
		return evaluationtransport.EvaluationAccepted{},
			interviewShadowStrategyError()
	}
	current, err := application.evaluations.Get(
		ctx,
		actor,
		evaluationID,
	)
	if err != nil {
		return evaluationtransport.EvaluationAccepted{}, err
	}
	if !interviewShadowEvaluation(current) {
		return evaluationtransport.EvaluationAccepted{},
			interviewShadowStrategyError()
	}
	value, replayed, err := application.evaluations.Reevaluate(
		ctx,
		actor,
		evaluationID,
		request,
	)
	if err != nil {
		return evaluationtransport.EvaluationAccepted{}, err
	}
	return interviewShadowAccepted(value, replayed)
}

func (application *evaluationHTTPApplication) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	evaluationID string,
) (evaluationtransport.EvaluationResource, error) {
	if application == nil || application.evaluations == nil ||
		application.runtime == nil ||
		!application.configuration.Valid() {
		return evaluationtransport.EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	value, err := application.evaluations.Get(
		ctx,
		actor,
		evaluationID,
	)
	if err != nil {
		return evaluationtransport.EvaluationResource{}, err
	}
	if !interviewShadowEvaluation(value) {
		return evaluationtransport.EvaluationResource{},
			evaluation.ErrNotFound
	}
	state, err := application.runtime.GetInterviewShadowState(
		ctx,
		actor.UserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		return evaluationtransport.EvaluationResource{}, err
	}
	return interviewShadowResource(
		value,
		state,
		application.configuration,
	)
}

func interviewShadowAccepted(
	value evaluation.Evaluation,
	replayed bool,
) (evaluationtransport.EvaluationAccepted, error) {
	if !interviewShadowEvaluation(value) {
		return evaluationtransport.EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	if replayed {
		switch value.Revision.Status {
		case evaluation.StatusQueued,
			evaluation.StatusRunning,
			evaluation.StatusReady,
			evaluation.StatusFailed:
		default:
			return evaluationtransport.EvaluationAccepted{},
				interviewShadowVersionConflictError()
		}
	} else if value.Revision.Status != evaluation.StatusQueued {
		return evaluationtransport.EvaluationAccepted{},
			interviewShadowVersionConflictError()
	}
	return evaluationtransport.EvaluationAccepted{
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		SupersedesRevisionID: value.Revision.SupersedesRevisionID,
		EvaluationStatus:     value.Revision.Status,
		Replayed:             replayed,
	}, nil
}

func interviewShadowEvaluation(value evaluation.Evaluation) bool {
	return value.Valid() &&
		value.Scope == evaluation.ScopeSession &&
		value.SceneType == evaluation.SceneInterview &&
		len(value.Revision.Channels) == 1 &&
		value.Revision.Channels[0] == evaluation.ChannelScene &&
		value.Revision.SceneStrategyRef ==
			evaluation.InterviewShadowStrategyRef &&
		value.Revision.Core4DStrategyRef == "" &&
		value.Revision.PipelineVersion ==
			evaluation.InterviewShadowPipelineVersion
}

func interviewShadowResource(
	value evaluation.Evaluation,
	state evaluation.InterviewShadowReadState,
	configuration evaluation.InterviewShadowRuntimeConfiguration,
) (evaluationtransport.EvaluationResource, error) {
	if !interviewShadowEvaluation(value) || !configuration.Valid() {
		return evaluationtransport.EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	resource := evaluationtransport.EvaluationResource{
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		SupersedesRevisionID: value.Revision.SupersedesRevisionID,
		PracticeSessionID:    value.PracticeSessionID,
		InputSnapshotID:      value.InputSnapshotID,
		InputRevision:        value.InputRevision,
		Scope:                value.Scope,
		SceneType:            value.SceneType,
		Channels: append(
			[]evaluation.Channel(nil),
			value.Revision.Channels...,
		),
		SceneStrategyRef: value.Revision.SceneStrategyRef,
		PipelineVersion:  value.Revision.PipelineVersion,
		SchemaVersion:    value.Revision.SchemaVersion,
		EvaluationStatus: value.Revision.Status,
		ModuleStatuses: map[string]evaluationtransport.ModuleStatus{
			"scene": interviewShadowModuleStatus(state.ModuleStatus),
		},
		IsFinal:     value.Revision.IsFinal,
		CreatedAt:   value.Revision.CreatedAt,
		UpdatedAt:   value.Revision.UpdatedAt,
		CompletedAt: value.Revision.CompletedAt,
	}
	switch value.Revision.Status {
	case evaluation.StatusQueued:
		if state.ModuleStatus != evaluation.InterviewShadowRuntimePending {
			return evaluationtransport.EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if state.ModuleStatus != evaluation.InterviewShadowRuntimePending &&
			state.ModuleStatus != evaluation.InterviewShadowRuntimeRunning {
			return evaluationtransport.EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.ModuleStatus != evaluation.InterviewShadowRuntimeReady ||
			state.Result == nil || state.Failure != nil ||
			value.Revision.IsFinal {
			return evaluationtransport.EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
		projected, err := projectInterviewShadowSceneResult(
			*state.Result,
			configuration,
			state.FullConfigHash,
		)
		if err != nil {
			return evaluationtransport.EvaluationResource{}, err
		}
		resource.ScoreabilityStatus =
			evaluationtransport.ScoreabilityStatus(
				state.Result.Scoreability,
			)
		resource.GateStatus = evaluationtransport.GateStatus(
			state.Result.Gate,
		)
		resource.SceneResult = projected
	case evaluation.StatusFailed:
		if state.ModuleStatus != evaluation.InterviewShadowRuntimeFailed ||
			state.Result != nil || state.Failure == nil {
			return evaluationtransport.EvaluationResource{},
				evaluation.ErrInvalidRequest
		}
		resource.StableFailure = &evaluationtransport.EvaluationFailure{
			ReasonCode: interviewShadowFailureReason(
				state.Failure.Code,
			),
			Retryable: state.Failure.Retryable,
		}
	default:
		return evaluationtransport.EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func interviewShadowModuleStatus(
	status evaluation.InterviewShadowRuntimeStatus,
) evaluationtransport.ModuleStatus {
	switch status {
	case evaluation.InterviewShadowRuntimePending:
		return evaluationtransport.ModulePending
	case evaluation.InterviewShadowRuntimeRunning:
		return evaluationtransport.ModuleRunning
	case evaluation.InterviewShadowRuntimeReady:
		return evaluationtransport.ModuleReady
	case evaluation.InterviewShadowRuntimeFailed:
		return evaluationtransport.ModuleFailed
	default:
		return ""
	}
}

type interviewSceneResultProjection struct {
	SceneType            evaluation.SceneType                   `json:"scene_type"`
	Dimensions           []interviewDimensionProjection         `json:"dimensions"`
	TaskResults          []any                                  `json:"task_results"`
	QuestionResults      []any                                  `json:"question_results"`
	Core4ObservationRefs []string                               `json:"core4_observation_refs"`
	EvaluationStatus     evaluation.Status                      `json:"evaluation_status"`
	ScoreabilityStatus   evaluation.InterviewScoreabilityStatus `json:"scoreability_status"`
	GateStatus           evaluation.InterviewGateStatus         `json:"gate_status"`
	ModuleStatuses       map[string]string                      `json:"module_statuses"`
	StrategyRef          string                                 `json:"strategy_ref"`
	AggregationVersion   string                                 `json:"aggregation_version"`
	CalibrationVersion   string                                 `json:"calibration_version"`
	FullConfigHash       string                                 `json:"full_config_hash"`
	IsFinal              bool                                   `json:"is_final"`
	ReadinessLevel       evaluation.InterviewReadinessLevel     `json:"readiness_level"`
}

type interviewDimensionProjection struct {
	DimensionID        evaluation.InterviewDimension          `json:"dimension_id"`
	RoundingRule       string                                 `json:"rounding_rule"`
	ScoreabilityStatus evaluation.InterviewScoreabilityStatus `json:"scoreability_status"`
	GateStatus         evaluation.InterviewGateStatus         `json:"gate_status"`
	Confidence         float64                                `json:"confidence"`
	Coverage           float64                                `json:"coverage"`
	ReasonCodes        []evaluationtransport.ReasonCode       `json:"reason_codes"`
	EvidenceRefs       []any                                  `json:"evidence_refs"`
}

func projectInterviewShadowSceneResult(
	result evaluation.InterviewShadowResult,
	configuration evaluation.InterviewShadowRuntimeConfiguration,
	fullConfigHash [sha256.Size]byte,
) (json.RawMessage, error) {
	if !configuration.Valid() ||
		fullConfigHash == [sha256.Size]byte{} ||
		result.SceneType != evaluation.SceneInterview ||
		result.Scope != evaluation.ScopeSession ||
		result.Channel != evaluation.ChannelScene ||
		result.Readiness != evaluation.InterviewReadinessNotAssessed ||
		len(result.Dimensions) != 5 ||
		result.QuestionResults == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	dimensions := make(
		[]interviewDimensionProjection,
		len(result.Dimensions),
	)
	expectedDimensions := evaluation.InterviewDimensions()
	for index, dimension := range result.Dimensions {
		if dimension.DimensionID != expectedDimensions[index] ||
			!validInterviewShadowDimensionGate(
				result.Scoreability,
				result.Gate,
				dimension.Scoreability,
				dimension.Gate,
			) ||
			!unitInterval(dimension.Confidence) ||
			!unitInterval(dimension.Coverage) ||
			len(dimension.ReasonCodes) == 0 {
			return nil, evaluation.ErrInvalidRequest
		}
		reasons := make(
			[]evaluationtransport.ReasonCode,
			len(dimension.ReasonCodes),
		)
		for reasonIndex, reason := range dimension.ReasonCodes {
			mapped, ok := interviewShadowReason(reason)
			if !ok {
				return nil, evaluation.ErrInvalidRequest
			}
			reasons[reasonIndex] = mapped
		}
		dimensions[index] = interviewDimensionProjection{
			DimensionID:        dimension.DimensionID,
			RoundingRule:       "HALF_UP_TO_INTEGER",
			ScoreabilityStatus: dimension.Scoreability,
			GateStatus:         dimension.Gate,
			Confidence:         dimension.Confidence,
			Coverage:           dimension.Coverage,
			ReasonCodes:        reasons,
			EvidenceRefs:       []any{},
		}
	}
	projection := interviewSceneResultProjection{
		SceneType:            result.SceneType,
		Dimensions:           dimensions,
		TaskResults:          []any{},
		QuestionResults:      []any{},
		Core4ObservationRefs: []string{},
		EvaluationStatus:     evaluation.StatusReady,
		ScoreabilityStatus:   result.Scoreability,
		GateStatus:           result.Gate,
		ModuleStatuses:       map[string]string{"scene": "READY"},
		StrategyRef:          configuration.StrategyRef,
		AggregationVersion:   interviewShadowAggregationVersion,
		CalibrationVersion:   interviewShadowCalibrationVersion,
		FullConfigHash: "sha256:" +
			hex.EncodeToString(fullConfigHash[:]),
		IsFinal:        false,
		ReadinessLevel: result.Readiness,
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func interviewShadowReason(
	reason evaluation.InterviewReasonCode,
) (evaluationtransport.ReasonCode, bool) {
	switch reason {
	case evaluation.InterviewReasonOpportunityNotProvided:
		return evaluationtransport.ReasonOpportunityNotProvided, true
	case evaluation.InterviewReasonASRConfidenceUnavailable,
		evaluation.InterviewReasonInsufficientEvidence:
		return evaluationtransport.ReasonInsufficientEvidence, true
	default:
		return "", false
	}
}

func unitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= 0 && value <= 1
}

func validInterviewShadowDimensionGate(
	resultScoreability evaluation.InterviewScoreabilityStatus,
	resultGate evaluation.InterviewGateStatus,
	dimensionScoreability evaluation.InterviewScoreabilityStatus,
	dimensionGate evaluation.InterviewGateStatus,
) bool {
	switch {
	case resultScoreability == evaluation.InterviewScoreabilityInsufficient &&
		resultGate == evaluation.InterviewGateBlocked:
		return dimensionScoreability ==
			evaluation.InterviewScoreabilityInsufficient &&
			dimensionGate == evaluation.InterviewGateBlocked
	case resultScoreability == evaluation.InterviewScoreabilityProvisional &&
		resultGate == evaluation.InterviewGateFeedbackOnly:
		return (dimensionScoreability ==
			evaluation.InterviewScoreabilityProvisional &&
			dimensionGate == evaluation.InterviewGateFeedbackOnly) ||
			(dimensionScoreability ==
				evaluation.InterviewScoreabilityInsufficient &&
				dimensionGate == evaluation.InterviewGateBlocked)
	default:
		return false
	}
}

func interviewShadowFailureReason(
	code string,
) evaluationtransport.ReasonCode {
	switch code {
	case "provider_invalid_response":
		return evaluationtransport.ReasonPolicyViolation
	case "evidence_ref_invalid":
		return evaluationtransport.ReasonEvidenceRefInvalid
	case "version_conflict":
		return evaluationtransport.ReasonVersionConflict
	default:
		return evaluationtransport.ReasonInternalRetryable
	}
}

func interviewShadowStrategyError() error {
	return apperror.New(
		apperror.UnprocessableEntity,
		"evaluation_strategy_not_available",
		"The requested Evaluation strategy is not available.",
	)
}

func interviewShadowVersionConflictError() error {
	return apperror.New(
		apperror.Conflict,
		"evaluation_version_conflict",
		"Evaluation state changed before the operation completed.",
	)
}

var _ evaluationtransport.Application = (*evaluationHTTPApplication)(nil)
