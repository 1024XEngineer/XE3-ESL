package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"time"

	agentapp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/app"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentpersistence "github.com/1024XEngineer/XE3-ESL/server/internal/agent/persistence"
	agentruntime "github.com/1024XEngineer/XE3-ESL/server/internal/agent/runtime"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/summary"
	agenttransport "github.com/1024XEngineer/XE3-ESL/server/internal/agent/transport"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
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
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	voiceConfigurations ...VoiceConfiguration,
) (*identity.Module, RouteRegistrar, error) {
	if len(voiceConfigurations) == 1 &&
		voiceConfigurations[0].AgentVoiceMessagesEnabled {
		return nil, nil, errors.New(
			"bootstrap: Agent voice messages require the cleanup-aware composition",
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
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, err
	}
	return composition.identity.module, composition.agentModule, nil
}

type identityAgentComposition struct {
	identity            *identityComposition
	agentModule         RouteRegistrar
	agentService        *agentapp.Service
	agentVoiceReclaimer AgentVoiceObjectReclaimer
	matterService       *matter.Service
	memoryExtraction    memory.ExtractionProcessor
	summaryProcessor    agentsummary.Processor
	ids                 *identity.UUIDv4Generator
}

// AgentVoiceObjectReclaimer is the narrow lifecycle capability retained by
// the production composition. The command owns scheduling and shutdown while
// Agent keeps repository and object-key details private.
type AgentVoiceObjectReclaimer interface {
	ReclaimVoiceObjects(
		context.Context,
		int,
	) (core.VoiceCleanupResult, error)
}

func buildIdentityAgentComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	memoryExtractionNotifier interface{ Notify() },
	summaryNotifier interface{ Notify() },
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
	agentRepository, err := agentpersistence.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	agentService, err := agentapp.NewService(agentRepository, matterService)
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
	contextAssembler, err := agentruntime.NewContextAssembler(
		agentRepository,
		matterService,
		stableProfileReader,
		contextMemorySearcher,
	)
	if err != nil {
		return nil, err
	}
	runOptions, err := agentRunServiceOptions()
	if err != nil {
		return nil, err
	}
	runRepository := core.RunRepository(agentRepository)
	notifiers := make([]interface{ Notify() }, 0, 2)
	if memoryExtractionNotifier != nil {
		notifiers = append(notifiers, memoryExtractionNotifier)
	}
	if summaryNotifier != nil {
		notifiers = append(notifiers, summaryNotifier)
	}
	if len(notifiers) > 0 {
		runRepository = &runCompletionNotifyingRepository{
			RunRepository: agentRepository,
			notifiers:     notifiers,
		}
	}
	runService, err := agentruntime.NewRunService(
		runRepository,
		contextAssembler,
		generator,
		runConfiguration,
		runOptions...,
	)
	if err != nil {
		return nil, err
	}
	if _, err := runService.RecoverInterruptedRuns(ctx); err != nil {
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
	agentVoiceMessages, err := buildAgentVoiceMessageApplication(
		voiceConfigurations,
		agentRepository,
		runService,
		ids,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	var voiceApplication *agentvoice.VoiceSessionApplication
	var audioAssets *conversation.AudioAssetService
	if len(voiceConfigurations) == 1 {
		voiceApplication, audioAssets, err = buildProductionVoiceApplication(
			database,
			generator,
			matterService,
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
		if agentVoiceMessages != nil {
			voiceHTTPOption.AgentMessages = agentVoiceMessages
		}
		voiceHTTPOptions = append(
			voiceHTTPOptions,
			voiceHTTPOption,
		)
	}
	handler, err := agenttransport.NewHTTPHandlerWithRunsVoiceAndAudio(
		agentService,
		runService,
		voiceApplication,
		audioAssets,
		matterService,
		identityContext.service,
		nil,
		voiceHTTPOptions...,
	)
	if err != nil {
		return nil, err
	}
	var agentVoiceReclaimer AgentVoiceObjectReclaimer
	if agentVoiceMessages != nil {
		agentVoiceReclaimer = agentVoiceMessages
	}
	return &identityAgentComposition{
		identity:            identityContext,
		agentModule:         handler,
		agentService:        agentService,
		agentVoiceReclaimer: agentVoiceReclaimer,
		matterService:       matterService,
		memoryExtraction:    memoryExtraction,
		summaryProcessor:    summaryProcessor,
		ids:                 ids,
	}, nil
}

// NewIdentityAgentModulesWithVoiceCleanup retains the narrow cleanup
// capability needed by the server scheduler without exposing Agent storage.
func NewIdentityAgentModulesWithVoiceCleanup(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
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
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return composition.identity.module,
		composition.agentModule,
		composition.agentVoiceReclaimer,
		nil
}

func buildAgentVoiceMessageApplication(
	voiceConfigurations []VoiceConfiguration,
	repository agentvoice.VoiceMessageRepository,
	runs agentvoice.VoicePendingRunProcessor,
	ids core.IDGenerator,
	runConfiguration core.RunConfiguration,
) (*agentvoice.VoiceMessageService, error) {
	if len(voiceConfigurations) == 0 ||
		!voiceConfigurations[0].AgentVoiceMessagesEnabled {
		return nil, nil
	}
	configuration := voiceConfigurations[0]
	if configuration.ObjectStore == nil {
		return nil, errors.New(
			"bootstrap: Agent voice message object storage is required",
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
	sources, err := agentvoice.NewSignedVoiceAudioLoader(
		configuration.ObjectStore,
		client,
		configuration.ScratchDirectory,
		configuration.ObjectReadAllowedHosts,
	)
	if err != nil {
		return nil, err
	}
	return agentvoice.NewVoiceMessageService(
		repository,
		configuration.ObjectStore,
		sources,
		configuration.Recognizer,
		configuration.Synthesizer,
		runs,
		ids,
		agentvoice.VoiceMessageConfig{
			RunConfiguration: runConfiguration,
			ScratchDirectory: configuration.ScratchDirectory,
			CandidateTTL:     configuration.AudioStagedTTL,
			UploadLease:      configuration.AudioUploadLease,
			ASRLease:         configuration.ASRLease,
		},
	)
}
