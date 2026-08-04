package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	evaluationprofile "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/evaluationprofile"
	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentaudiohttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/audio/http"
	agentconversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	summarypostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary/postgres"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentimagehttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image/http"
	imagepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image/postgres"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentevaluationfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/evaluationfeedback"
	agentvoicehttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/http"
	voicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	agentrunhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/http"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	evaluationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agentcapability"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	goalagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcapability"
	goalhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/http"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicehttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/http"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	reviewagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	evaluationhistory "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/evaluationhistory"
	reviewhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/http"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
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
	modelProviders AgentModelProviders,
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
		modelProviders,
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
	goalService         *goal.Service
	productionTools     *capability.Registry
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
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	memoryExtractionNotifier interface{ Notify() },
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*identityAgentComposition, error) {
	if ctx == nil || database == nil || modelProviders.Run == nil ||
		modelProviders.Memory == nil || modelProviders.Summary == nil ||
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
	goalRepository, err := goal.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	goalService, err := goal.NewService(goalRepository)
	if err != nil {
		return nil, err
	}
	conversationRepository, err := conversationpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	agentService, err := agentconversation.NewService(
		conversationRepository,
		goalService,
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
	evaluationRepository := evaluationpostgres.NewPostgresRepository(database)
	learningProfileReader, err := evaluationprofile.New(
		evaluationRepository,
	)
	if err != nil {
		return nil, err
	}
	reviewReports, err := evaluationhistory.New(evaluationRepository)
	if err != nil {
		return nil, err
	}
	reviewHistory := review.NewHistoryService(reviewReports)
	goalTools, err := goalagentcapability.NewServicePort(
		goalService,
		agentService,
	)
	if err != nil {
		return nil, err
	}
	reviewTools, err := reviewagentcapability.NewServicePort(reviewHistory)
	if err != nil {
		return nil, err
	}
	evaluationTools, err := evaluationagentcapability.NewServicePort(
		evaluationRepository,
	)
	if err != nil {
		return nil, err
	}
	productionTools, err := capability.NewRegistry(
		goalagentcapability.NewGoalCreateCapability(goalTools),
		goalagentcapability.NewGoalSearchCapability(goalTools),
		reviewagentcapability.NewReviewSearchTool(reviewTools),
		reviewagentcapability.NewReviewGetTool(reviewTools),
		evaluationagentcapability.NewLatestPracticeReportTool(evaluationTools),
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
	memoryBarrier, err := memory.NewExtractionBarrierCoordinator(
		memoryRepository,
		memory.SystemExtractionBarrierScheduler{},
		memory.ExtractionBarrierWaitPolicy{
			MaximumWait:  5 * time.Second,
			PollInterval: 50 * time.Millisecond,
		},
	)
	if err != nil {
		return nil, err
	}
	contextMemoryBarrier, err := newAgentMemoryExtractionBarrier(memoryBarrier)
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
	contextRepository, err := contextpostgres.New(database)
	if err != nil {
		return nil, err
	}
	contextAssembler, err := agentcontext.NewAssembler(
		contextRepository,
		goalService,
		learningProfileReader,
		stableProfileReader,
		contextMemorySearcher,
		contextMemoryBarrier,
		contextOptions...,
	)
	if err != nil {
		return nil, err
	}
	toolOptions, err := agentRunServiceOptions(productionTools)
	if err != nil {
		return nil, err
	}
	runRepository, err := runpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	var runStore agentrun.Repository = runRepository
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
			runpostgres.NewImageSubmissionRepository(database, ids)
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
		conversationRepository,
		contextRepository,
		contextAssembler,
		modelProviders.Run,
		runConfiguration,
		runOptions...,
	)
	if err != nil {
		return nil, err
	}
	memoryExtraction, err := buildMemoryExtractionProcessor(
		database,
		ids,
		runRepository,
		conversationRepository,
		contextRepository,
		modelProviders.Memory,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	summaryRepository, err := summarypostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	summaryService, err := agentsummary.NewService(
		summaryRepository,
		modelProviders.Summary,
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
		summaryRepository,
		summaryRepository,
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
	var voiceInputRepository agentvoice.Repository
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoiceInputEnabled {
		voiceInputRepository, err = voicepostgres.New(database, ids)
		if err != nil {
			return nil, err
		}
	}
	voiceInput, err := buildAgentVoiceInputApplication(
		voiceConfigurations,
		voiceInputRepository,
		voiceRunProcessor,
		ids,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	var voiceApplication *practicevoice.SessionApplication
	var sameQuestionRetry *practicevoice.SameQuestionRetryApplication
	var audioAssets *practicevoice.AudioAssetService
	if len(voiceConfigurations) == 1 {
		voiceApplication, sameQuestionRetry, audioAssets, err =
			buildProductionVoiceApplication(
				database,
				voiceConfigurations[0],
			)
		if err != nil {
			return nil, err
		}
	}
	errorRenderer := httpresponse.NewRenderer(nil)
	goalHTTP, err := goalhttp.NewHandler(goalService, errorRenderer)
	if err != nil {
		return nil, err
	}
	conversationHTTPOptions := []agentconversationhttp.Option{
		agentconversationhttp.WithToolCalls(runService),
	}
	if agentImages != nil {
		conversationHTTPOptions = append(
			conversationHTTPOptions,
			agentconversationhttp.WithMessageImages(agentImages),
		)
	}
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].SpeechFeedbackCoordinator != nil {
		conversationHTTPOptions = append(
			conversationHTTPOptions,
			agentconversationhttp.WithSpeechFeedback(
				voiceConfigurations[0].SpeechFeedbackCoordinator,
			),
		)
	}
	conversationHTTP, err := agentconversationhttp.NewHandler(
		agentService,
		errorRenderer,
		conversationHTTPOptions...,
	)
	if err != nil {
		return nil, err
	}
	runHTTP, err := agentrunhttp.NewHandler(runService, errorRenderer)
	if err != nil {
		return nil, err
	}
	registrars := []ProtectedRouteRegistrar{
		goalHTTP,
		conversationHTTP,
		runHTTP,
	}
	if agentImages != nil {
		imageHTTP, imageHTTPErr := agentimagehttp.NewHandler(
			agentImages,
			agentService,
			0,
			errorRenderer,
		)
		if imageHTTPErr != nil {
			return nil, imageHTTPErr
		}
		registrars = append(registrars, imageHTTP)
	}
	if voiceInput != nil {
		voiceHTTP, voiceHTTPErr := agentvoicehttp.NewHandler(
			voiceInput,
			agentService,
			voiceConfigurations[0].AudioReadTimeout,
			errorRenderer,
		)
		if voiceHTTPErr != nil {
			return nil, voiceHTTPErr
		}
		audioHTTP, audioHTTPErr := agentaudiohttp.NewHandler(
			voiceInput,
			errorRenderer,
		)
		if audioHTTPErr != nil {
			return nil, audioHTTPErr
		}
		registrars = append(registrars, voiceHTTP, audioHTTP)
	}
	if voiceApplication != nil {
		practiceHTTP, practiceHTTPErr := practicevoicehttp.NewHandler(
			voiceApplication,
			practicevoicehttp.Options{
				AudioReadTimeout:  voiceConfigurations[0].AudioReadTimeout,
				SameQuestionRetry: sameQuestionRetry,
				AudioAssets:       audioAssets,
			},
			errorRenderer,
		)
		if practiceHTTPErr != nil {
			return nil, practiceHTTPErr
		}
		reviewHTTP, reviewHTTPErr := reviewhttp.NewHandler(
			reviewHistory,
			voiceConfigurations[0].ReviewHistoryCursorKey,
			errorRenderer,
		)
		if reviewHTTPErr != nil {
			return nil, reviewHTTPErr
		}
		registrars = append(registrars, practiceHTTP, reviewHTTP)
	}
	handler := &bearerProtectedRoutes{
		authentication: identityContext.handler.AuthenticationMiddleware(),
		registrars:     registrars,
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
		goalService:         goalService,
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
	repository, err := imagepostgres.New(database)
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
	modelProviders AgentModelProviders,
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
		modelProviders,
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
		feedback, feedbackErr := agentevaluationfeedback.New(
			configuration.SpeechFeedbackCoordinator,
		)
		if feedbackErr != nil {
			return nil, feedbackErr
		}
		feedbackPorts = append(
			feedbackPorts,
			feedback,
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
