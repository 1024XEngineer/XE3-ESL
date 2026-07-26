package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewIdentityAndAgentDataModules(
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
) (*identity.Module, *agent.Module, error) {
	if database == nil {
		return nil, nil, errors.New(
			"bootstrap: agent data database is required",
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
	handler, err := agent.NewHTTPHandler(
		agentService,
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
