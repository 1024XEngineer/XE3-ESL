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
// text-generation composition. It has no Fake-provider fallback.
func NewIdentityAndAgentModules(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration agent.RunConfiguration,
) (*identity.Module, *agent.Module, error) {
	if ctx == nil || database == nil || generator == nil {
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
	handler, err := agent.NewHTTPHandlerWithRuns(
		agentService,
		runService,
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
