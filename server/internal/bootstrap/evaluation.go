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
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationtext "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textprovider"
	evaluationtransport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/transport"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	interviewShadowAggregationVersion     = "interview-shadow-aggregation/v1"
	interviewShadowCalibrationVersion     = "NOT_CONFIGURED"
	interviewShadowGateVersion            = "interview-shadow-gate/v1"
	ieltsSpeakingShadowAggregationVersion = "ielts-speaking-shadow-aggregation/v1"
	ieltsSpeakingShadowCalibrationVersion = "NOT_CONFIGURED"
	ieltsSpeakingShadowGateVersion        = "ielts-speaking-shadow-gate/v1"
	generalSceneAggregationVersion        = "general-scene-aggregation/v1"
	generalSceneCalibrationVersion        = "NOT_CONFIGURED"
	generalSceneGateVersion               = "general-scene-evidence-gate/v1"
)

type EvaluationConfiguration struct {
	Provider          string
	Model             string
	MaxOutputTokens   int
	GenerationTimeout time.Duration
	LeaseDuration     time.Duration
	RetryDelay        time.Duration
	MaxAttempts       int
}

type EvaluationComposition struct {
	interviewCoordinator *evaluation.InterviewShadowCoordinator
	ieltsCoordinator     *evaluation.IELTSSpeakingShadowCoordinator
	handler              *evaluationtransport.HTTPHandler
	worker               evaluationShadowProcessor
}

func NewEvaluationComposition(
	database *pgxpool.Pool,
	textGenerator ai.TextGenerator,
	configuration EvaluationConfiguration,
) (*EvaluationComposition, error) {
	if database == nil || textGenerator == nil ||
		configuration.Provider == "" ||
		configuration.Model == "" ||
		configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 ||
		configuration.GenerationTimeout <= 0 ||
		configuration.GenerationTimeout >
			evaluationtext.MaxGenerationTimeout ||
		configuration.LeaseDuration < time.Second ||
		configuration.LeaseDuration > 10*time.Minute ||
		configuration.RetryDelay < 0 ||
		configuration.RetryDelay > time.Hour ||
		configuration.MaxAttempts < 1 ||
		configuration.MaxAttempts > 10 {
		return nil, errors.New(
			"bootstrap: Evaluation dependencies are required",
		)
	}
	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	conversationRepository := practiceRepository
	audioRepository, err := practicepostgres.NewAudioAssetRepository(database)
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
	ieltsCoordinator, err :=
		evaluation.NewIELTSSpeakingShadowCoordinator(
			evidenceService,
			evaluationService,
		)
	if err != nil {
		return nil, err
	}
	provider, err := evaluationtext.NewInterviewShadowProvider(
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
	ieltsProvider, err := evaluationtext.NewIELTSSpeakingShadowProvider(
		textGenerator,
		configuration.GenerationTimeout,
	)
	if err != nil {
		return nil, err
	}
	ieltsRuntimeConfiguration, err :=
		ieltsSpeakingShadowRuntimeConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	ieltsWorker, err := evaluation.NewIELTSSpeakingShadowWorker(
		repository,
		evaluation.NewIELTSSpeakingShadowEngine(ieltsProvider),
		ieltsRuntimeConfiguration,
	)
	if err != nil {
		return nil, err
	}
	generalProvider, err := evaluationtext.NewGeneralSceneProvider(
		textGenerator,
		configuration.GenerationTimeout,
	)
	if err != nil {
		return nil, err
	}
	generalConfiguration, err := generalSceneRuntimeConfiguration(
		configuration,
	)
	if err != nil {
		return nil, err
	}
	generalWorker, err := evaluation.NewGeneralSceneWorker(
		repository,
		evaluation.NewGeneralSceneEngine(generalProvider),
		generalConfiguration,
	)
	if err != nil {
		return nil, err
	}
	completionIntake, err := evaluation.NewCompletionIntake(
		practiceRepository,
		evidenceService,
		evaluationService,
		evaluation.CompletionIntakeConfiguration{
			MaxAttempts:   configuration.MaxAttempts,
			LeaseDuration: configuration.LeaseDuration,
			RetryDelay:    configuration.RetryDelay,
		},
	)
	if err != nil {
		return nil, err
	}
	combinedWorker, err := newEvaluationShadowProcessor(
		completionIntake,
		worker,
		ieltsWorker,
		generalWorker,
	)
	if err != nil {
		return nil, err
	}
	application := &evaluationHTTPApplication{
		evaluations:        evaluationService,
		runtime:            repository,
		interviewReports:   repository,
		ieltsReports:       repository,
		configuration:      runtimeConfiguration,
		ieltsConfiguration: ieltsRuntimeConfiguration,
	}
	handler, err := evaluationtransport.NewHTTPHandler(application)
	if err != nil {
		return nil, err
	}
	return &EvaluationComposition{
		interviewCoordinator: coordinator,
		ieltsCoordinator:     ieltsCoordinator,
		handler:              handler,
		worker:               combinedWorker,
	}, nil
}

func generalSceneRuntimeConfiguration(
	configuration EvaluationConfiguration,
) (evaluation.GeneralSceneRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return evaluation.GeneralSceneRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(evaluation.GeneralSceneSystemContract),
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
		SchemaVersion:         evaluation.SchemaVersion,
		StrategyRef:           evaluation.GeneralSceneStrategyRef,
		PipelineVersion:       evaluation.GeneralScenePipelineVersion,
		PromptVersion:         evaluation.GeneralScenePromptVersion,
		PromptContractHash:    "sha256:" + hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: evaluation.GeneralSceneProviderSchemaVersion,
		GateVersion:           generalSceneGateVersion,
		AggregationVersion:    generalSceneAggregationVersion,
		CalibrationVersion:    generalSceneCalibrationVersion,
		Provider:              configuration.Provider,
		Model:                 configuration.Model,
		MaxOutputTokens:       configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return evaluation.GeneralSceneRuntimeConfiguration{}, err
	}
	result := evaluation.GeneralSceneRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     evaluation.GeneralSceneStrategyRef,
		PipelineVersion: evaluation.GeneralScenePipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   evaluation.GeneralScenePromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return evaluation.GeneralSceneRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	return result, nil
}

func (composition *EvaluationComposition) InterviewShadowCoordinator() *evaluation.InterviewShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.interviewCoordinator
}

func (composition *EvaluationComposition) IELTSSpeakingShadowCoordinator() *evaluation.IELTSSpeakingShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.ieltsCoordinator
}

func (composition *EvaluationComposition) HTTPHandler() *evaluationtransport.HTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.handler
}

func (composition *EvaluationComposition) Worker() evaluationShadowProcessor {
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
		[]byte(evaluation.InterviewShadowSystemContract),
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

func ieltsSpeakingShadowRuntimeConfiguration(
	configuration EvaluationConfiguration,
) (evaluation.IELTSSpeakingShadowRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return evaluation.IELTSSpeakingShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(evaluation.IELTSSpeakingShadowSystemContract),
	)
	manifest := struct {
		SchemaVersion         string `json:"schema_version"`
		StrategyRef           string `json:"strategy_ref"`
		PipelineVersion       string `json:"pipeline_version"`
		PromptVersion         string `json:"prompt_version"`
		PromptContractHash    string `json:"prompt_contract_hash"`
		ProviderSchemaVersion string `json:"provider_schema_version"`
		RubricVersion         string `json:"rubric_version"`
		GateVersion           string `json:"gate_version"`
		AggregationVersion    string `json:"aggregation_version"`
		CalibrationVersion    string `json:"calibration_version"`
		Provider              string `json:"provider"`
		Model                 string `json:"model"`
		MaxOutputTokens       int    `json:"max_output_tokens"`
	}{
		SchemaVersion:   evaluation.SchemaVersion,
		StrategyRef:     evaluation.IELTSSpeakingShadowStrategyRef,
		PipelineVersion: evaluation.IELTSSpeakingShadowPipelineVersion,
		PromptVersion:   evaluation.IELTSSpeakingShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: evaluation.
			IELTSSpeakingShadowProviderSchemaVersion,
		RubricVersion:      evaluation.IELTSSpeakingShadowRubricVersion,
		GateVersion:        ieltsSpeakingShadowGateVersion,
		AggregationVersion: ieltsSpeakingShadowAggregationVersion,
		CalibrationVersion: ieltsSpeakingShadowCalibrationVersion,
		Provider:           configuration.Provider,
		Model:              configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return evaluation.IELTSSpeakingShadowRuntimeConfiguration{}, err
	}
	result := evaluation.IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     evaluation.IELTSSpeakingShadowStrategyRef,
		PipelineVersion: evaluation.IELTSSpeakingShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   evaluation.IELTSSpeakingShadowPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return evaluation.IELTSSpeakingShadowRuntimeConfiguration{},
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

type interviewReportReader interface {
	GetCurrentInterviewReportState(
		context.Context,
		string,
		string,
	) (evaluation.InterviewReportReadState, error)
}

type ieltsSpeakingReportReader interface {
	GetCurrentIELTSSpeakingReportState(
		context.Context,
		string,
		string,
	) (evaluation.IELTSSpeakingReportReadState, error)
}

type evaluationHTTPApplication struct {
	evaluations        evaluationService
	runtime            evaluationRuntimeReader
	interviewReports   interviewReportReader
	ieltsReports       ieltsSpeakingReportReader
	configuration      evaluation.InterviewShadowRuntimeConfiguration
	ieltsConfiguration evaluation.IELTSSpeakingShadowRuntimeConfiguration
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
	if application == nil || application.evaluations == nil {
		return evaluationtransport.EvaluationAccepted{},
			evaluation.ErrInvalidRequest
	}
	current, err := application.evaluations.Get(
		ctx,
		actor,
		evaluationID,
	)
	if err != nil {
		return evaluationtransport.EvaluationAccepted{}, err
	}
	var accept func(
		evaluation.Evaluation,
		bool,
	) (evaluationtransport.EvaluationAccepted, error)
	switch {
	case interviewShadowEvaluation(current):
		if !application.configuration.Valid() ||
			evaluation.ValidateInterviewShadowReevaluateRequest(request) != nil {
			return evaluationtransport.EvaluationAccepted{},
				interviewShadowStrategyError()
		}
		accept = interviewShadowAccepted
	case ieltsSpeakingShadowEvaluation(current):
		if !application.ieltsConfiguration.Valid() ||
			evaluation.ValidateIELTSSpeakingShadowReevaluateRequest(request) != nil {
			return evaluationtransport.EvaluationAccepted{},
				interviewShadowStrategyError()
		}
		accept = ieltsSpeakingShadowAccepted
	default:
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
	return accept(value, replayed)
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

func (application *evaluationHTTPApplication) GetInterviewReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (evaluationtransport.InterviewReportResource, error) {
	if application == nil || application.interviewReports == nil ||
		ctx == nil || !actor.Valid() ||
		!application.configuration.Valid() {
		return evaluationtransport.InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return evaluationtransport.InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	state, err := application.interviewReports.GetCurrentInterviewReportState(
		ctx,
		actor.UserID,
		practiceSessionID,
	)
	if err != nil {
		if errors.Is(
			err,
			evaluation.ErrInterviewShadowConfigurationConflict,
		) {
			return evaluationtransport.InterviewReportResource{},
				interviewShadowVersionConflictError()
		}
		return evaluationtransport.InterviewReportResource{}, err
	}
	return interviewReportResource(
		practiceSessionID,
		state,
	)
}

func (application *evaluationHTTPApplication) GetIELTSSpeakingReport(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
) (evaluationtransport.IELTSSpeakingReportResource, error) {
	if application == nil ||
		application.ieltsReports == nil ||
		ctx == nil ||
		!actor.Valid() ||
		!application.ieltsConfiguration.Valid() {
		return evaluationtransport.IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if !ok || trustedActor != actor {
		return evaluationtransport.IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	state, err := application.ieltsReports.
		GetCurrentIELTSSpeakingReportState(
			ctx,
			actor.UserID,
			practiceSessionID,
		)
	if err != nil {
		if errors.Is(
			err,
			evaluation.ErrIELTSSpeakingShadowConfigurationConflict,
		) {
			return evaluationtransport.IELTSSpeakingReportResource{},
				interviewShadowVersionConflictError()
		}
		return evaluationtransport.IELTSSpeakingReportResource{}, err
	}
	return ieltsSpeakingReportResource(practiceSessionID, state)
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

func ieltsSpeakingShadowAccepted(
	value evaluation.Evaluation,
	replayed bool,
) (evaluationtransport.EvaluationAccepted, error) {
	if !ieltsSpeakingShadowEvaluation(value) {
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
		failure := interviewShadowFailure(state.Failure.Code)
		resource.StableFailure = &failure
	default:
		return evaluationtransport.EvaluationResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func interviewReportResource(
	practiceSessionID string,
	state evaluation.InterviewReportReadState,
) (evaluationtransport.InterviewReportResource, error) {
	value := state.Evaluation
	if practiceSessionID == "" ||
		value.PracticeSessionID != practiceSessionID ||
		!interviewShadowEvaluation(value) ||
		value.Revision.IsFinal {
		return evaluationtransport.InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	resource := evaluationtransport.InterviewReportResource{
		PracticeSessionID:    value.PracticeSessionID,
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		EvaluationStatus:     value.Revision.Status,
		IsFinal:              false,
	}
	switch value.Revision.Status {
	case evaluation.StatusQueued:
		if state.Runtime.ModuleStatus !=
			evaluation.InterviewShadowRuntimePending ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return evaluationtransport.InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if (state.Runtime.ModuleStatus !=
			evaluation.InterviewShadowRuntimePending &&
			state.Runtime.ModuleStatus !=
				evaluation.InterviewShadowRuntimeRunning) ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return evaluationtransport.InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.Runtime.ModuleStatus !=
			evaluation.InterviewShadowRuntimeReady ||
			state.Runtime.Result == nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot == nil ||
			state.Snapshot.ID != value.InputSnapshotID ||
			state.Snapshot.OwnerUserID != value.OwnerUserID ||
			state.Snapshot.PracticeSessionID !=
				value.PracticeSessionID {
			return evaluationtransport.InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
		report, err := evaluation.ProjectInterviewReport(
			*state.Snapshot,
			*state.Runtime.Result,
		)
		if err != nil {
			return evaluationtransport.InterviewReportResource{}, err
		}
		resource.Report = &report
	case evaluation.StatusFailed:
		if state.Runtime.ModuleStatus !=
			evaluation.InterviewShadowRuntimeFailed ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure == nil ||
			state.Snapshot != nil {
			return evaluationtransport.InterviewReportResource{},
				evaluation.ErrInvalidRequest
		}
		failure := interviewShadowFailure(state.Runtime.Failure.Code)
		resource.StableFailure = &failure
	default:
		return evaluationtransport.InterviewReportResource{},
			evaluation.ErrInvalidRequest
	}
	return resource, nil
}

func ieltsSpeakingShadowEvaluation(
	value evaluation.Evaluation,
) bool {
	return value.Valid() &&
		value.Scope == evaluation.ScopeSession &&
		value.SceneType == evaluation.SceneIELTSSpeaking &&
		len(value.Revision.Channels) == 1 &&
		value.Revision.Channels[0] == evaluation.ChannelScene &&
		value.Revision.SceneStrategyRef ==
			evaluation.IELTSSpeakingShadowStrategyRef &&
		value.Revision.Core4DStrategyRef == "" &&
		value.Revision.PipelineVersion ==
			evaluation.IELTSSpeakingShadowPipelineVersion
}

func ieltsSpeakingReportResource(
	practiceSessionID string,
	state evaluation.IELTSSpeakingReportReadState,
) (evaluationtransport.IELTSSpeakingReportResource, error) {
	value := state.Evaluation
	if practiceSessionID == "" ||
		value.PracticeSessionID != practiceSessionID ||
		!ieltsSpeakingShadowEvaluation(value) ||
		value.Revision.IsFinal {
		return evaluationtransport.IELTSSpeakingReportResource{},
			evaluation.ErrInvalidRequest
	}
	resource := evaluationtransport.IELTSSpeakingReportResource{
		PracticeSessionID:    value.PracticeSessionID,
		EvaluationID:         value.ID,
		EvaluationRevisionID: value.Revision.ID,
		Revision:             value.Revision.Number,
		EvaluationStatus:     value.Revision.Status,
		IsFinal:              false,
	}
	switch value.Revision.Status {
	case evaluation.StatusQueued:
		if state.Runtime.ModuleStatus !=
			evaluation.IELTSSpeakingShadowRuntimePending ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return evaluationtransport.IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusRunning:
		if (state.Runtime.ModuleStatus !=
			evaluation.IELTSSpeakingShadowRuntimePending &&
			state.Runtime.ModuleStatus !=
				evaluation.IELTSSpeakingShadowRuntimeRunning) ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot != nil {
			return evaluationtransport.IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
	case evaluation.StatusReady:
		if state.Runtime.ModuleStatus !=
			evaluation.IELTSSpeakingShadowRuntimeReady ||
			state.Runtime.Result == nil ||
			state.Runtime.Failure != nil ||
			state.Snapshot == nil ||
			state.Snapshot.ID != value.InputSnapshotID ||
			state.Snapshot.OwnerUserID != value.OwnerUserID ||
			state.Snapshot.PracticeSessionID !=
				value.PracticeSessionID {
			return evaluationtransport.IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
		report, err := evaluation.ProjectIELTSSpeakingReport(
			*state.Snapshot,
			*state.Runtime.Result,
		)
		if err != nil {
			return evaluationtransport.IELTSSpeakingReportResource{},
				err
		}
		resource.Report = &report
	case evaluation.StatusFailed:
		if state.Runtime.ModuleStatus !=
			evaluation.IELTSSpeakingShadowRuntimeFailed ||
			state.Runtime.Result != nil ||
			state.Runtime.Failure == nil ||
			state.Snapshot != nil {
			return evaluationtransport.IELTSSpeakingReportResource{},
				evaluation.ErrInvalidRequest
		}
		failure := interviewShadowFailure(state.Runtime.Failure.Code)
		resource.StableFailure = &failure
	default:
		return evaluationtransport.IELTSSpeakingReportResource{},
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

func interviewShadowFailure(
	code string,
) evaluationtransport.EvaluationFailure {
	switch code {
	case "provider_invalid_response":
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonPolicyViolation,
		}
	case "evidence_ref_invalid":
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonEvidenceRefInvalid,
		}
	case "version_conflict":
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonVersionConflict,
		}
	case "runtime_configuration_changed":
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonInternalRetryable,
			Retryable:  true,
		}
	case "provider_canceled",
		"provider_timeout",
		"rate_limited",
		"timeout",
		"provider_unavailable",
		"invalid_response",
		"cancelled",
		"dependency_error",
		"attempts_exhausted":
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonInternalRetryable,
			Retryable:  true,
		}
	default:
		return evaluationtransport.EvaluationFailure{
			ReasonCode: evaluationtransport.ReasonInternalNonRetryable,
		}
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
