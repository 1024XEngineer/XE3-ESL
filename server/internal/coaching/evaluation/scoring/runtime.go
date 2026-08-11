package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

const (
	runtimeLeaseDuration = 60 * time.Second
	runtimeRetryDelay    = 5 * time.Second
	runtimeMaxAttempts   = 3
)

type Configuration struct {
	provider          string
	model             string
	maxOutputTokens   int
	generationTimeout time.Duration
	leaseDuration     time.Duration
	retryDelay        time.Duration
	maxAttempts       int
}

func NewConfiguration(
	provider string,
	model string,
	maxOutputTokens int,
) (Configuration, error) {
	configuration := Configuration{
		provider:          provider,
		model:             model,
		maxOutputTokens:   maxOutputTokens,
		generationTimeout: MaxGenerationTimeout,
		leaseDuration:     runtimeLeaseDuration,
		retryDelay:        runtimeRetryDelay,
		maxAttempts:       runtimeMaxAttempts,
	}
	if !configuration.valid() {
		return Configuration{}, evaluation.ErrInvalidRequest
	}
	return configuration, nil
}

func (configuration Configuration) valid() bool {
	return validRuntimeLineage(configuration.provider) &&
		validModelIdentifier(configuration.model) &&
		configuration.maxOutputTokens >= 1 &&
		configuration.maxOutputTokens <= 1_000_000 &&
		configuration.generationTimeout > 0 &&
		configuration.generationTimeout <= MaxGenerationTimeout &&
		configuration.leaseDuration >= time.Second &&
		configuration.leaseDuration <= 10*time.Minute &&
		configuration.retryDelay >= 0 &&
		configuration.retryDelay <= time.Hour &&
		configuration.maxAttempts >= 1 &&
		configuration.maxAttempts <= 10
}

type RuntimeRepository interface {
	InterviewShadowRuntimeRepository
	IELTSSpeakingShadowRuntimeRepository
	IELTSAcousticSnapshotRepository
	GeneralSceneRuntimeRepository
}

type RuntimeEvidence interface {
	sceneShadowEvidenceFreezer
	completedEvidenceFreezer
}

type RuntimeEvaluations interface {
	sceneShadowEvaluationCreator
	completedEvaluationCreator
}

type Runtime struct {
	interviewCoordinator   *InterviewShadowCoordinator
	ieltsCoordinator       *IELTSSpeakingShadowCoordinator
	processor              Processor
	interviewConfiguration InterviewShadowRuntimeConfiguration
	ieltsConfiguration     IELTSSpeakingShadowRuntimeConfiguration
}

func NewRuntimeWithIELTSAcoustics(
	repository RuntimeRepository,
	completions practice.CompletionHandoffRepository,
	evidenceService RuntimeEvidence,
	evaluationService RuntimeEvaluations,
	textGenerator TextGenerator,
	policies *EvaluationPolicyRegistry,
	configuration Configuration,
	acoustics IELTSSpeakingAcousticSource,
) (*Runtime, error) {
	if repository == nil || completions == nil || evidenceService == nil ||
		evaluationService == nil || textGenerator == nil || policies == nil ||
		acoustics == nil ||
		!configuration.valid() {
		return nil, evaluation.ErrInvalidRequest
	}
	interviewConfiguration, err :=
		interviewRuntimeConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	ieltsConfiguration, err := ieltsRuntimeConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	generalConfiguration, err := generalRuntimeConfiguration(configuration)
	if err != nil {
		return nil, err
	}
	interviewProvider, err := NewInterviewShadowProvider(
		textGenerator,
		configuration.generationTimeout,
	)
	if err != nil {
		return nil, err
	}
	interviewWorker, err := NewInterviewShadowWorker(
		repository,
		NewInterviewShadowEngine(interviewProvider),
		interviewConfiguration,
	)
	if err != nil {
		return nil, err
	}
	ieltsProvider, err := NewIELTSSpeakingShadowProvider(
		textGenerator,
		configuration.generationTimeout,
	)
	if err != nil {
		return nil, err
	}
	ieltsEngine := NewIELTSSpeakingShadowEngine(ieltsProvider)
	ieltsWorker, err := NewIELTSSpeakingShadowWorkerWithAcousticSnapshots(
		repository,
		ieltsEngine,
		ieltsConfiguration,
		acoustics,
	)
	if err != nil {
		return nil, err
	}
	generalProvider, err := NewGeneralSceneProvider(
		textGenerator,
		configuration.generationTimeout,
	)
	if err != nil {
		return nil, err
	}
	generalWorker, err := NewGeneralSceneWorker(
		repository,
		NewGeneralSceneEngine(generalProvider),
		generalConfiguration,
	)
	if err != nil {
		return nil, err
	}
	intake, err := NewCompletionIntake(
		completions,
		evidenceService,
		evaluationService,
		policies,
		CompletionIntakeConfiguration{
			MaxAttempts:   configuration.maxAttempts,
			LeaseDuration: configuration.leaseDuration,
			RetryDelay:    configuration.retryDelay,
		},
	)
	if err != nil {
		return nil, err
	}
	processor, err := NewProcessor(
		intake,
		interviewWorker,
		ieltsWorker,
		generalWorker,
	)
	if err != nil {
		return nil, err
	}
	interviewCoordinator, err := NewInterviewShadowCoordinator(
		evidenceService,
		evaluationService,
	)
	if err != nil {
		return nil, err
	}
	ieltsCoordinator, err := NewIELTSSpeakingShadowCoordinator(
		evidenceService,
		evaluationService,
	)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		interviewCoordinator:   interviewCoordinator,
		ieltsCoordinator:       ieltsCoordinator,
		processor:              processor,
		interviewConfiguration: interviewConfiguration,
		ieltsConfiguration:     ieltsConfiguration,
	}, nil
}

func (runtime *Runtime) InterviewCoordinator() *InterviewShadowCoordinator {
	if runtime == nil {
		return nil
	}
	return runtime.interviewCoordinator
}

func (runtime *Runtime) IELTSSpeakingCoordinator() *IELTSSpeakingShadowCoordinator {
	if runtime == nil {
		return nil
	}
	return runtime.ieltsCoordinator
}

func (runtime *Runtime) Processor() Processor {
	if runtime == nil {
		return nil
	}
	return runtime.processor
}

func (runtime *Runtime) InterviewConfiguration() InterviewShadowRuntimeConfiguration {
	if runtime == nil {
		return InterviewShadowRuntimeConfiguration{}
	}
	return runtime.interviewConfiguration
}

func (runtime *Runtime) IELTSSpeakingConfiguration() IELTSSpeakingShadowRuntimeConfiguration {
	if runtime == nil {
		return IELTSSpeakingShadowRuntimeConfiguration{}
	}
	return runtime.ieltsConfiguration
}

func generalRuntimeConfiguration(
	configuration Configuration,
) (GeneralSceneRuntimeConfiguration, error) {
	promptContractHash := sha256.Sum256([]byte(GeneralSceneSystemContract))
	legacyManifest := struct {
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
		StrategyRef:           GeneralSceneStrategyRef,
		PipelineVersion:       GeneralScenePipelineVersion,
		PromptVersion:         GeneralScenePromptVersion,
		PromptContractHash:    "sha256:" + hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: GeneralSceneProviderSchemaVersion,
		GateVersion:           GeneralSceneGateVersion,
		AggregationVersion:    GeneralSceneAggregationVersion,
		CalibrationVersion:    GeneralSceneCalibrationVersion,
		Provider:              configuration.provider,
		Model:                 configuration.model,
		MaxOutputTokens:       configuration.maxOutputTokens,
	}
	legacyEncoded, err := json.Marshal(legacyManifest)
	if err != nil {
		return GeneralSceneRuntimeConfiguration{}, err
	}
	legacyHash := sha256.Sum256(legacyEncoded)
	atomicPromptContractHash := sha256.Sum256(
		[]byte(GeneralSceneAtomicSystemContract),
	)
	atomicManifest := struct {
		SchemaVersion        string `json:"schema_version"`
		LegacyConfigHash     string `json:"legacy_config_hash"`
		AtomicPromptVersion  string `json:"atomic_prompt_version"`
		AtomicPromptHash     string `json:"atomic_prompt_contract_hash"`
		AtomicProviderSchema string `json:"atomic_provider_schema_version"`
		AtomicResultSchema   string `json:"atomic_result_schema_version"`
		AtomicAggregation    string `json:"atomic_aggregation_version"`
	}{
		SchemaVersion:       evaluation.SchemaVersion,
		LegacyConfigHash:    "sha256:" + hex.EncodeToString(legacyHash[:]),
		AtomicPromptVersion: GeneralSceneAtomicPromptVersion,
		AtomicPromptHash: "sha256:" + hex.EncodeToString(
			atomicPromptContractHash[:],
		),
		AtomicProviderSchema: GeneralSceneAtomicProviderSchemaVersion,
		AtomicResultSchema:   GeneralSceneAtomicResultSchemaVersion,
		AtomicAggregation:    GeneralSceneAtomicAggregationVersion,
	}
	atomicEncoded, err := json.Marshal(atomicManifest)
	if err != nil {
		return GeneralSceneRuntimeConfiguration{}, err
	}
	result := GeneralSceneRuntimeConfiguration{
		MaxAttempts:         configuration.maxAttempts,
		LeaseDuration:       configuration.leaseDuration,
		StrategyRef:         GeneralSceneStrategyRef,
		PipelineVersion:     GeneralScenePipelineVersion,
		FullConfigHash:      legacyHash,
		IELTSFullConfigHash: sha256.Sum256(atomicEncoded),
		PromptVersion:       GeneralScenePromptVersion,
		Provider:            configuration.provider,
		Model:               configuration.model,
	}
	if !result.Valid() {
		return GeneralSceneRuntimeConfiguration{}, evaluation.ErrInvalidRequest
	}
	return result, nil
}

func interviewRuntimeConfiguration(
	configuration Configuration,
) (InterviewShadowRuntimeConfiguration, error) {
	promptContractHash := sha256.Sum256([]byte(InterviewShadowSystemContract))
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
		StrategyRef:     InterviewShadowStrategyRef,
		PipelineVersion: InterviewShadowPipelineVersion,
		PromptVersion:   InterviewShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: InterviewShadowProviderSchemaVersion,
		GateVersion:           InterviewShadowGateVersion,
		AggregationVersion:    InterviewShadowAggregationVersion,
		CalibrationVersion:    InterviewShadowCalibrationVersion,
		Provider:              configuration.provider,
		Model:                 configuration.model,
		MaxOutputTokens:       configuration.maxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return InterviewShadowRuntimeConfiguration{}, err
	}
	result := InterviewShadowRuntimeConfiguration{
		MaxAttempts:     configuration.maxAttempts,
		LeaseDuration:   configuration.leaseDuration,
		StrategyRef:     InterviewShadowStrategyRef,
		PipelineVersion: InterviewShadowPipelineVersion,
		FullConfigHash:  sha256.Sum256(encoded),
		PromptVersion:   InterviewShadowPromptVersion,
		Provider:        configuration.provider,
		Model:           configuration.model,
	}
	if !result.Valid() {
		return InterviewShadowRuntimeConfiguration{}, evaluation.ErrInvalidRequest
	}
	return result, nil
}

func ieltsRuntimeConfiguration(
	configuration Configuration,
) (IELTSSpeakingShadowRuntimeConfiguration, error) {
	promptContractHash := sha256.Sum256(
		[]byte(IELTSSpeakingShadowSystemContract),
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
		StrategyRef:     IELTSSpeakingShadowStrategyRef,
		PipelineVersion: IELTSSpeakingShadowPipelineVersion,
		PromptVersion:   IELTSSpeakingShadowPromptVersion,
		PromptContractHash: "sha256:" +
			hex.EncodeToString(promptContractHash[:]),
		ProviderSchemaVersion: IELTSSpeakingShadowProviderSchemaVersion,
		RubricVersion:         IELTSSpeakingShadowRubricVersion,
		GateVersion:           IELTSSpeakingShadowGateVersion,
		AggregationVersion:    IELTSSpeakingShadowAggregationVersion,
		CalibrationVersion:    IELTSSpeakingShadowCalibrationVersion,
		Provider:              configuration.provider,
		Model:                 configuration.model,
		MaxOutputTokens:       configuration.maxOutputTokens,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return IELTSSpeakingShadowRuntimeConfiguration{}, err
	}
	result := IELTSSpeakingShadowRuntimeConfiguration{
		MaxAttempts:          configuration.maxAttempts,
		LeaseDuration:        configuration.leaseDuration,
		AcousticWaitDuration: IELTSAcousticSnapshotWaitDurationV1,
		StrategyRef:          IELTSSpeakingShadowStrategyRef,
		PipelineVersion:      IELTSSpeakingShadowPipelineVersion,
		FullConfigHash:       sha256.Sum256(encoded),
		PromptVersion:        IELTSSpeakingShadowPromptVersion,
		Provider:             configuration.provider,
		Model:                configuration.model,
	}
	if !result.Valid() {
		return IELTSSpeakingShadowRuntimeConfiguration{}, evaluation.ErrInvalidRequest
	}
	return result, nil
}

var (
	_ RuntimeEvidence    = (*evidence.EvidenceSnapshotService)(nil)
	_ RuntimeEvaluations = (*evaluation.Service)(nil)
)
