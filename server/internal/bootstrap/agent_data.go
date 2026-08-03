package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	agentstore "github.com/1024XEngineer/XE3-ESL/server/internal/agent/store"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	agenttransport "github.com/1024XEngineer/XE3-ESL/server/internal/agent/transport"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
	evaluationagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/evaluation/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	matteragenttool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	reviewagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewIdentityAndAgentModules builds the production Identity, Agent data, and
// text-generation composition. An optional voice composition is enabled only
// when every owning module supplies its explicit Port; there is no Fake
// fallback.
func NewIdentityAndAgentModules(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	voiceConfigurations ...VoiceConfiguration,
) (*identity.Module, RouteRegistrar, error) {
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoiceInputEnabled {
		return nil, nil, errors.New(
			"bootstrap: Agent voice input requires the cleanup-aware composition",
		)
	}
	composition, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		nil,
		nil,
		nil,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := composition.recoverInterruptedRuns(ctx); err != nil {
		return nil, nil, err
	}
	return composition.identity.module, composition.agentModule, nil
}

type identityAgentComposition struct {
	identity            *identityComposition
	agentModule         RouteRegistrar
	agentService        *agentconversation.Service
	agentVoiceReclaimer AgentVoiceObjectReclaimer
	agentImageReclaimer AgentImageObjectReclaimer
	matterService       *matter.Service
	productionTools     *tool.Registry
	runService          *agentrun.Service
	memoryExtraction    memory.ExtractionProcessor
	summaryProcessor    agentsummary.Processor
	ids                 *identity.UUIDv4Generator
}

// AgentVoiceObjectReclaimer is the narrow lifecycle capability retained by
// the production composition. The command owns scheduling and shutdown while
// Agent keeps repository and object-key details private.
type AgentVoiceObjectReclaimer interface {
	ReclaimObjects(
		context.Context,
		int,
	) (agentvoice.CleanupResult, error)
}

type AgentImageObjectReclaimer interface {
	Reclaim(context.Context, int) (agentimage.CleanupResult, error)
}

func buildIdentityAgentComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	memoryExtractionNotifier interface{ Notify() },
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*identityAgentComposition, error) {
	if ctx == nil || database == nil || generator == nil ||
		memorySearcher == nil || len(voiceConfigurations) > 1 {
		return nil, errors.New(
			"bootstrap: Agent Run dependencies are required",
		)
	}
	identityContext, err := buildIdentityComposition(
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
	)
	if err != nil {
		return nil, err
	}
	ids := identity.NewUUIDv4Generator(nil)
	matterRepository, err := matter.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	matterService, err := matter.NewService(matterRepository)
	if err != nil {
		return nil, err
	}
	agentRepository, err := agentstore.NewPostgresStore(database, ids)
	if err != nil {
		return nil, err
	}
	agentService, err := agentconversation.NewService(
		agentRepository,
		matterService,
	)
	if err != nil {
		return nil, err
	}
	agentImages, err := buildAgentImageApplication(
		imageConfiguration,
		database,
		ids,
	)
	if err != nil {
		return nil, err
	}
	reviewRepository := review.NewPostgresRepository(database)
	reviewHistory := review.NewHistoryService(reviewRepository)
	matterTools, err := matteragenttool.NewServicePort(
		matterService,
		agentService,
	)
	if err != nil {
		return nil, err
	}
	reviewTools, err := reviewagenttool.NewServicePort(reviewHistory)
	if err != nil {
		return nil, err
	}
	evaluationTools, err := evaluationagenttool.NewServicePort(
		evaluation.NewPostgresRepository(database),
	)
	if err != nil {
		return nil, err
	}
	productionTools, err := tool.NewRegistry(
		matteragenttool.NewScenarioCreateTool(matterTools),
		matteragenttool.NewScenarioSearchTool(matterTools),
		reviewagenttool.NewReviewSearchTool(reviewTools),
		reviewagenttool.NewReviewGetTool(reviewTools),
		evaluationagenttool.NewLatestPracticeReportTool(evaluationTools),
	)
	if err != nil {
		return nil, err
	}
	contextMemorySearcher, err := newAgentMemoryContextSearcher(memorySearcher)
	if err != nil {
		return nil, err
	}
	memoryRepository, err := memory.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	stableProfileReader, err := newAgentStableProfileReader(memoryRepository)
	if err != nil {
		return nil, err
	}
	var contextOptions []agentcontext.Option
	if agentImages != nil {
		contextOptions = append(
			contextOptions,
			agentcontext.WithImageReader(agentImages),
		)
	}
	contextAssembler, err := agentcontext.NewAssembler(
		agentRepository,
		matterService,
		stableProfileReader,
		contextMemorySearcher,
		contextOptions...,
	)
	if err != nil {
		return nil, err
	}
	toolOptions, err := agentRunServiceOptions(productionTools)
	if err != nil {
		return nil, err
	}
	var runStore agentrun.Repository = agentRepository
	notifiers := make([]interface{ Notify() }, 0, 2)
	if memoryExtractionNotifier != nil {
		notifiers = append(notifiers, memoryExtractionNotifier)
	}
	if summaryNotifier != nil {
		notifiers = append(notifiers, summaryNotifier)
	}
	if len(notifiers) > 0 {
		runStore = &runCompletionNotifyingRepository{
			Repository: runStore,
			notifiers:  notifiers,
		}
	}
	runOptions := append([]agentrun.Option(nil), toolOptions.runOptions...)
	if agentImages != nil {
		imageSubmissions, imageSubmissionErr :=
			agentstore.NewGormImageRunRepositoryFromPool(
				agentRepository,
				database,
				ids,
			)
		if imageSubmissionErr != nil {
			return nil, imageSubmissionErr
		}
		runOptions = append(
			runOptions,
			agentrun.WithImageSubmissions(imageSubmissions),
		)
	}
	runService, err := agentrun.NewService(
		runStore,
		agentRepository,
		contextAssembler,
		generator,
		runConfiguration,
		runOptions...,
	)
	if err != nil {
		return nil, err
	}
	memoryExtraction, err := buildMemoryExtractionProcessor(
		database,
		ids,
		agentRepository,
		generator,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	summaryService, err := agentsummary.NewService(
		agentRepository,
		generator,
		agentsummary.Configuration{
			PolicyVersion: "summary-policy-v1",
			PromptVersion: "summary-prompt-v1",
			Provider:      runConfiguration.Provider,
			Model:         runConfiguration.Model,
		},
	)
	if err != nil {
		return nil, err
	}
	summaryProcessor, err := agentsummary.NewWorker(
		agentRepository,
		agentRepository,
		summaryService,
		agentsummary.WorkerConfiguration{
			TriggerPolicyVersion: agentsummary.TriggerPolicyV2,
			TriggerMessages:      agentsummary.DefaultTriggerMessages,
			RetainRecentMessages: agentsummary.DefaultRetainedMessages,
			LeaseDuration:        2 * time.Minute,
			MaxAttempts:          agentsummary.DefaultWorkerMaxAttempts,
			Summary: agentsummary.Configuration{
				PolicyVersion: "summary-policy-v1",
				PromptVersion: "summary-prompt-v1",
				Provider:      runConfiguration.Provider,
				Model:         runConfiguration.Model,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	voiceRunProcessor := agentvoice.PendingRunProcessor(runService)
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoiceInputEnabled {
		voiceRunProcessor, err = newDeferredAgentVoiceRunProcessor(
			ctx,
			runService,
			slog.Default(),
		)
		if err != nil {
			return nil, err
		}
	}
	voiceInput, err := buildAgentVoiceInputApplication(
		voiceConfigurations,
		agentRepository,
		voiceRunProcessor,
		ids,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	var voiceApplication *practicevoice.SessionApplication
	var sameQuestionRetry *practicevoice.SameQuestionRetryApplication
	var audioAssets *conversation.AudioAssetService
	if len(voiceConfigurations) == 1 {
		voiceApplication, sameQuestionRetry, audioAssets, err =
			buildProductionVoiceApplication(
				database,
				generator,
				matterService,
				reviewRepository,
				reviewHistory,
				voiceConfigurations[0],
			)
		if err != nil {
			return nil, err
		}
	}
	var voiceHTTPOptions []agenttransport.VoiceHTTPOptions
	if len(voiceConfigurations) == 1 {
		voiceHTTPOption := agenttransport.VoiceHTTPOptions{
			AudioReadTimeout: voiceConfigurations[0].AudioReadTimeout,
			ReviewHistoryCursorKey: append(
				[]byte(nil),
				voiceConfigurations[0].ReviewHistoryCursorKey...,
			),
		}
		if voiceInput != nil {
			voiceHTTPOption.VoiceInput = voiceInput
		}
		if voiceConfigurations[0].SpeechFeedbackCoordinator != nil {
			voiceHTTPOption.SpeechFeedback =
				voiceConfigurations[0].SpeechFeedbackCoordinator
		}
		voiceHTTPOption.SameQuestionRetry = sameQuestionRetry
		voiceHTTPOptions = append(
			voiceHTTPOptions,
			voiceHTTPOption,
		)
	}
	var imageApplication agentimage.Application
	if agentImages != nil {
		imageApplication = agentImages
	}
	handler, err := agenttransport.NewHTTPHandlerWithRunsVoiceAudioAndImages(
		agentService,
		runService,
		voiceApplication,
		audioAssets,
		imageApplication,
		matterService,
		identityContext.service,
		nil,
		voiceHTTPOptions...,
	)
	if err != nil {
		return nil, err
	}
	var agentVoiceReclaimer AgentVoiceObjectReclaimer
	if voiceInput != nil {
		agentVoiceReclaimer = voiceInput
	}
	var agentImageReclaimer AgentImageObjectReclaimer
	if agentImages != nil {
		agentImageReclaimer = agentImages
	}
	return &identityAgentComposition{
		identity:            identityContext,
		agentModule:         handler,
		agentService:        agentService,
		agentVoiceReclaimer: agentVoiceReclaimer,
		agentImageReclaimer: agentImageReclaimer,
		matterService:       matterService,
		productionTools:     toolOptions.productionRegistry,
		runService:          runService,
		memoryExtraction:    memoryExtraction,
		summaryProcessor:    summaryProcessor,
		ids:                 ids,
	}, nil
}

func buildAgentImageApplication(
	configuration *AgentImageConfiguration,
	database *pgxpool.Pool,
	ids agentimage.IDGenerator,
) (*agentimage.Service, error) {
	if configuration == nil {
		return nil, nil
	}
	if configuration.ObjectStore == nil {
		return nil, errors.New(
			"bootstrap: Agent image object storage is required",
		)
	}
	repository, err :=
		agentstore.NewGormImageAssetRepositoryFromPool(database)
	if err != nil {
		return nil, err
	}
	return agentimage.NewService(
		repository,
		configuration.ObjectStore,
		ids,
		agentimage.Config{
			StagedTTL:   configuration.StagedTTL,
			UploadLease: configuration.UploadLease,
		},
		agentimage.WithLogger(slog.Default()),
	)
}

// NewIdentityAgentModulesWithVoiceCleanup retains the narrow cleanup
// capability needed by the server scheduler without exposing Agent storage.
func NewIdentityAgentModulesWithVoiceCleanup(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	voiceConfigurations ...VoiceConfiguration,
) (
	*identity.Module,
	RouteRegistrar,
	AgentVoiceObjectReclaimer,
	error,
) {
	composition, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		nil,
		nil,
		nil,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := composition.recoverInterruptedRuns(ctx); err != nil {
		return nil, nil, nil, err
	}
	return composition.identity.module,
		composition.agentModule,
		composition.agentVoiceReclaimer,
		nil
}

func (composition *identityAgentComposition) recoverInterruptedRuns(
	ctx context.Context,
) error {
	if composition == nil || composition.runService == nil {
		return errors.New("bootstrap: Agent Run service is required")
	}
	_, err := composition.runService.RecoverInterrupted(ctx)
	return err
}

func buildAgentVoiceInputApplication(
	voiceConfigurations []VoiceConfiguration,
	repository agentvoice.Repository,
	runs agentvoice.PendingRunProcessor,
	ids agentvoice.IDGenerator,
	runConfiguration agentrun.Configuration,
) (*agentvoice.Service, error) {
	if len(voiceConfigurations) == 0 ||
		!voiceConfigurations[0].AgentVoiceInputEnabled {
		return nil, nil
	}
	configuration := voiceConfigurations[0]
	if configuration.ObjectStore == nil {
		return nil, errors.New(
			"bootstrap: Agent voice input object storage is required",
		)
	}
	var client *http.Client
	if configuration.AudioReadTimeout > 0 {
		client = &http.Client{
			Timeout: configuration.AudioReadTimeout,
			CheckRedirect: func(
				*http.Request,
				[]*http.Request,
			) error {
				return http.ErrUseLastResponse
			},
		}
	}
	sources, err := agentvoice.NewSignedAudioLoader(
		configuration.ObjectStore,
		client,
		configuration.ScratchDirectory,
		configuration.ObjectReadAllowedHosts,
	)
	if err != nil {
		return nil, err
	}
	feedbackPorts := make([]agentvoice.FeedbackPort, 0, 1)
	if configuration.SpeechFeedbackCoordinator != nil {
		feedbackPorts = append(
			feedbackPorts,
			&voiceSpeechFeedbackAdapter{
				coordinator: configuration.SpeechFeedbackCoordinator,
			},
		)
	}
	return agentvoice.NewService(
		repository,
		configuration.ObjectStore,
		sources,
		configuration.Recognizer,
		configuration.Synthesizer,
		runs,
		ids,
		agentvoice.Config{
			Configuration:    runConfiguration,
			ScratchDirectory: configuration.ScratchDirectory,
			CandidateTTL:     configuration.AudioStagedTTL,
			UploadLease:      configuration.AudioUploadLease,
			ASRLease:         configuration.ASRLease,
		},
		feedbackPorts...,
	)
}
