package bootstrap

import (
	"context"
	"errors"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
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
	runConfiguration agent.RunConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*identity.Module, *agent.Module, error) {
	composition, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, nil, err
	}
	return composition.identity.module, composition.agentModule, nil
}

type identityAgentComposition struct {
	identity            *identityComposition
	agentModule         *agent.Module
	agentService        *agent.Service
	agentVoiceReclaimer AgentVoiceObjectReclaimer
	matterService       *matter.Service
	ids                 *identity.UUIDv4Generator
}

// AgentVoiceObjectReclaimer is the narrow lifecycle capability retained by
// the production composition. The command owns scheduling and shutdown while
// Agent keeps repository and object-key details private.
type AgentVoiceObjectReclaimer interface {
	ReclaimVoiceObjects(
		context.Context,
		int,
	) (agent.VoiceCleanupResult, error)
}

func buildIdentityAgentComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration agent.RunConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*identityAgentComposition, error) {
	if ctx == nil || database == nil || generator == nil ||
		len(voiceConfigurations) > 1 {
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
	agentRepository, err := agent.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	agentService, err := agent.NewService(agentRepository, matterService)
	if err != nil {
		return nil, err
	}
	contextAssembler, err := agent.NewContextAssembler(
		agentRepository,
		matterService,
	)
	if err != nil {
		return nil, err
	}
	runService, err := agent.NewRunService(
		agentRepository,
		contextAssembler,
		generator,
		runConfiguration,
	)
	if err != nil {
		return nil, err
	}
	if _, err := runService.RecoverInterruptedRuns(ctx); err != nil {
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
	var voiceApplication *agent.VoiceSessionApplication
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
	var voiceHTTPOptions []agent.VoiceHTTPOptions
	if len(voiceConfigurations) == 1 {
		voiceHTTPOption := agent.VoiceHTTPOptions{
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
	handler, err := agent.NewHTTPHandlerWithRunsVoiceAndAudio(
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
	agentModule, err := agent.NewModule(handler)
	if err != nil {
		return nil, err
	}
	return &identityAgentComposition{
		identity:            identityContext,
		agentModule:         agentModule,
		agentService:        agentService,
		agentVoiceReclaimer: agentVoiceMessages,
		matterService:       matterService,
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
	runConfiguration agent.RunConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (
	*identity.Module,
	*agent.Module,
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
	repository agent.VoiceMessageRepository,
	runs agent.VoicePendingRunProcessor,
	ids agent.IDGenerator,
	runConfiguration agent.RunConfiguration,
) (*agent.VoiceMessageService, error) {
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
	sources, err := agent.NewSignedVoiceAudioLoader(
		configuration.ObjectStore,
		client,
		configuration.ScratchDirectory,
		configuration.ObjectReadAllowedHosts,
	)
	if err != nil {
		return nil, err
	}
	return agent.NewVoiceMessageService(
		repository,
		configuration.ObjectStore,
		sources,
		configuration.Recognizer,
		configuration.Synthesizer,
		runs,
		ids,
		agent.VoiceMessageConfig{
			RunConfiguration: runConfiguration,
			ScratchDirectory: configuration.ScratchDirectory,
			CandidateTTL:     configuration.AudioStagedTTL,
			UploadLease:      configuration.AudioUploadLease,
			ASRLease:         configuration.ASRLease,
		},
	)
}
