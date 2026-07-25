package bootstrap

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
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
	if ctx == nil || database == nil || generator == nil ||
		len(voiceConfigurations) > 1 {
		return nil, nil, errors.New(
			"bootstrap: Agent Run dependencies are required",
		)
	}
	identityModule, authenticator, err := newIdentityComposition(
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
	)
	if err != nil {
		return nil, nil, err
	}
	ids := identity.NewUUIDv4Generator(nil)
	matterRepository, err := matter.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, nil, err
	}
	matterService, err := matter.NewService(matterRepository)
	if err != nil {
		return nil, nil, err
	}
	agentRepository, err := agent.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, nil, err
	}
	agentService, err := agent.NewService(agentRepository, matterService)
	if err != nil {
		return nil, nil, err
	}
	contextAssembler, err := agent.NewContextAssembler(
		agentRepository,
		matterService,
	)
	if err != nil {
		return nil, nil, err
	}
	runService, err := agent.NewRunService(
		agentRepository,
		contextAssembler,
		generator,
		runConfiguration,
	)
	if err != nil {
		return nil, nil, err
	}
	if _, err := runService.RecoverInterruptedRuns(ctx); err != nil {
		return nil, nil, err
	}
	var voiceApplication *agent.VoiceSessionApplication
	if len(voiceConfigurations) == 1 {
		voiceApplication, err = buildVoiceApplication(
			matterService,
			voiceConfigurations[0],
		)
		if err != nil {
			return nil, nil, err
		}
	}
	handler, err := agent.NewHTTPHandlerWithRunsAndVoice(
		agentService,
		runService,
		voiceApplication,
		matterService,
		authenticator,
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	agentModule, err := agent.NewModule(handler)
	if err != nil {
		return nil, nil, err
	}
	return identityModule, agentModule, nil
}
