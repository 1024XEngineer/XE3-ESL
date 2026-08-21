package postgres_test

import (
	"testing"

	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	summarypostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary/postgres"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type agentRepositories struct {
	conversation *conversationpostgres.Repository
	context      *contextpostgres.Repository
	run          *runpostgres.Repository
	summary      *summarypostgres.Repository
}

func newAgentRepositories(
	t *testing.T,
	pool *pgxpool.Pool,
	ids identity.IDGenerator,
) *agentRepositories {
	t.Helper()
	conversationRepository, err := conversationpostgres.New(pool, ids)
	if err != nil {
		t.Fatalf("new Agent Conversation repository: %v", err)
	}
	contextRepository, err := contextpostgres.New(pool)
	if err != nil {
		t.Fatalf("new Agent Context repository: %v", err)
	}
	runRepository, err := runpostgres.New(pool, ids)
	if err != nil {
		t.Fatalf("new Agent Run repository: %v", err)
	}
	summaryRepository, err := summarypostgres.New(pool)
	if err != nil {
		t.Fatalf("new Agent Summary repository: %v", err)
	}
	return &agentRepositories{
		conversation: conversationRepository,
		context:      contextRepository,
		run:          runRepository,
		summary:      summaryRepository,
	}
}
