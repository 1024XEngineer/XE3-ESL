package bootstrap

import (
	"errors"

	agentmeme "github.com/1024XEngineer/XE3-ESL/server/internal/agent/meme"
	memepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/meme/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AgentMemeConfiguration struct {
	AssetRoot string
	Runtime   agentmeme.Config
}

func buildAgentMemeApplication(
	database *pgxpool.Pool,
	generator agentrun.TextGenerator,
	configuration *AgentMemeConfiguration,
) (agentrun.AssistantEnricher, *agentmeme.Service, error) {
	if configuration == nil || !configuration.Runtime.Enabled {
		return nil, nil, nil
	}
	if database == nil || generator == nil || configuration.AssetRoot == "" ||
		!configuration.Runtime.Valid() {
		return nil, nil, errors.New("bootstrap: Agent Meme dependencies are required")
	}
	catalog, err := agentmeme.NewFileCatalog(
		configuration.AssetRoot,
		configuration.Runtime.PackID,
		configuration.Runtime.PackVersion,
	)
	if err != nil {
		return nil, nil, err
	}
	repository, err := memepostgres.New(database)
	if err != nil {
		return nil, nil, err
	}
	classifier, err := agentmeme.NewToolClassifier(generator)
	if err != nil {
		return nil, nil, err
	}
	enricher, err := agentmeme.NewEnricher(
		configuration.Runtime,
		classifier,
		catalog,
		agentmeme.DeterministicSelector{},
		repository,
	)
	if err != nil {
		return nil, nil, err
	}
	service, err := agentmeme.NewService(repository, catalog)
	if err != nil {
		return nil, nil, err
	}
	return enricher, service, nil
}
