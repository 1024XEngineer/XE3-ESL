package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationapi "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/api"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
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
	interviewCoordinator *scoring.InterviewShadowCoordinator
	ieltsCoordinator     *scoring.IELTSSpeakingShadowCoordinator
	handler              *evaluationapi.HTTPHandler
	worker               evaluationShadowProcessor
}

func NewEvaluationComposition(
	database *pgxpool.Pool,
	textGenerator scoring.TextGenerator,
	configuration EvaluationConfiguration,
) (*EvaluationComposition, error) {
	if database == nil || textGenerator == nil ||
		configuration.Provider == "" ||
		configuration.Model == "" ||
		configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 ||
		configuration.GenerationTimeout <= 0 ||
		configuration.GenerationTimeout >
			scoring.MaxGenerationTimeout ||
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
	voiceRepository, err := practicevoicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	audioRepository, err := practicevoicepostgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	evidenceSource, err := evidence.NewEvidenceSourceReader(
		practiceRepository,
		voiceRepository,
		audioRepository,
	)
	if err != nil {
		return nil, err
	}
	repository := evaluationpostgres.NewPostgresRepository(database)
	evidenceRepository := evidence.NewPostgresRepository(database)
	evidenceService := evidence.NewEvidenceSnapshotService(
		evidenceSource,
		evidenceRepository,
	)
	evaluationService := evaluation.NewService(repository, evidenceRepository)
	coordinator, err := scoring.NewInterviewShadowCoordinator(
		evidenceService,
		evaluationService,
	)
	if err != nil {
		return nil, err
	}
	ieltsCoordinator, err :=
		scoring.NewIELTSSpeakingShadowCoordinator(
			evidenceService,
			evaluationService,
		)
	if err != nil {
		return nil, err
	}
	provider, err := scoring.NewInterviewShadowProvider(
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
	worker, err := scoring.NewInterviewShadowWorker(
		repository,
		scoring.NewInterviewShadowEngine(provider),
		runtimeConfiguration,
	)
	if err != nil {
		return nil, err
	}
	ieltsProvider, err := scoring.NewIELTSSpeakingShadowProvider(
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
	ieltsWorker, err := scoring.NewIELTSSpeakingShadowWorker(
		repository,
		scoring.NewIELTSSpeakingShadowEngine(ieltsProvider),
		ieltsRuntimeConfiguration,
	)
	if err != nil {
		return nil, err
	}
	generalProvider, err := scoring.NewGeneralSceneProvider(
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
	generalWorker, err := scoring.NewGeneralSceneWorker(
		repository,
		scoring.NewGeneralSceneEngine(generalProvider),
		generalConfiguration,
	)
	if err != nil {
		return nil, err
	}
	completionIntake, err := scoring.NewCompletionIntake(
		practiceRepository,
		evidenceService,
		evaluationService,
		scoring.CompletionIntakeConfiguration{
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
	application, err := evaluationapi.NewApplication(
		evaluationService,
		repository,
		repository,
		repository,
		runtimeConfiguration,
		ieltsRuntimeConfiguration,
	)
	if err != nil {
		return nil, err
	}
	handler, err := evaluationapi.NewHTTPHandler(application)
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
) (scoring.GeneralSceneRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return scoring.GeneralSceneRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(scoring.GeneralSceneSystemContract),
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
		StrategyRef:           scoring.GeneralSceneStrategyRef,
		PipelineVersion:       scoring.GeneralScenePipelineVersion,
		PromptVersion:         scoring.GeneralScenePromptVersion,
		PromptContractHash:    "sha256:" + hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: scoring.GeneralSceneProviderSchemaVersion,
		GateVersion:           scoring.GeneralSceneGateVersion,
		AggregationVersion:    scoring.GeneralSceneAggregationVersion,
		CalibrationVersion:    scoring.GeneralSceneCalibrationVersion,
		Provider:              configuration.Provider,
		Model:                 configuration.Model,
		MaxOutputTokens:       configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return scoring.GeneralSceneRuntimeConfiguration{}, err
	}
	result := scoring.GeneralSceneRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     scoring.GeneralSceneStrategyRef,
		PipelineVersion: scoring.GeneralScenePipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   scoring.GeneralScenePromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return scoring.GeneralSceneRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	return result, nil
}

func (composition *EvaluationComposition) InterviewShadowCoordinator() *scoring.InterviewShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.interviewCoordinator
}

func (composition *EvaluationComposition) IELTSSpeakingShadowCoordinator() *scoring.IELTSSpeakingShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.ieltsCoordinator
}

func (composition *EvaluationComposition) HTTPHandler() *evaluationapi.HTTPHandler {
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
) (scoring.InterviewShadowRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return scoring.InterviewShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(scoring.InterviewShadowSystemContract),
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
		StrategyRef:     scoring.InterviewShadowStrategyRef,
		PipelineVersion: scoring.InterviewShadowPipelineVersion,
		PromptVersion:   scoring.InterviewShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: scoring.InterviewShadowProviderSchemaVersion,
		GateVersion:           scoring.InterviewShadowGateVersion,
		AggregationVersion:    scoring.InterviewShadowAggregationVersion,
		CalibrationVersion:    scoring.InterviewShadowCalibrationVersion,
		Provider:              configuration.Provider,
		Model:                 configuration.Model,
		MaxOutputTokens:       configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return scoring.InterviewShadowRuntimeConfiguration{}, err
	}
	result := scoring.InterviewShadowRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     scoring.InterviewShadowStrategyRef,
		PipelineVersion: scoring.InterviewShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   scoring.InterviewShadowPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return scoring.InterviewShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	return result, nil
}

func ieltsSpeakingShadowRuntimeConfiguration(
	configuration EvaluationConfiguration,
) (scoring.IELTSSpeakingShadowRuntimeConfiguration, error) {
	if configuration.MaxOutputTokens < 1 ||
		configuration.MaxOutputTokens > 1_000_000 {
		return scoring.IELTSSpeakingShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	promptContractHash := sha256.Sum256(
		[]byte(scoring.IELTSSpeakingShadowSystemContract),
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
		StrategyRef:     scoring.IELTSSpeakingShadowStrategyRef,
		PipelineVersion: scoring.IELTSSpeakingShadowPipelineVersion,
		PromptVersion:   scoring.IELTSSpeakingShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: scoring.
			IELTSSpeakingShadowProviderSchemaVersion,
		RubricVersion:      scoring.IELTSSpeakingShadowRubricVersion,
		GateVersion:        scoring.IELTSSpeakingShadowGateVersion,
		AggregationVersion: scoring.IELTSSpeakingShadowAggregationVersion,
		CalibrationVersion: scoring.IELTSSpeakingShadowCalibrationVersion,
		Provider:           configuration.Provider,
		Model:              configuration.Model,
		MaxOutputTokens:    configuration.MaxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return scoring.IELTSSpeakingShadowRuntimeConfiguration{}, err
	}
	result := scoring.IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:     configuration.MaxAttempts,
		LeaseDuration:   configuration.LeaseDuration,
		StrategyRef:     scoring.IELTSSpeakingShadowStrategyRef,
		PipelineVersion: scoring.IELTSSpeakingShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   scoring.IELTSSpeakingShadowPromptVersion,
		Provider:        configuration.Provider,
		Model:           configuration.Model,
	}
	if !result.Valid() {
		return scoring.IELTSSpeakingShadowRuntimeConfiguration{},
			evaluation.ErrInvalidRequest
	}
	return result, nil
}
