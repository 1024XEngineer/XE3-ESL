// Package benchmark provides the explicit Agent composition used by the real
// model routing benchmark. It is not imported by the production Server.
package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	runhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/http"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/app"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewHandler builds the benchmark-only Identity and Agent HTTP stack. The
// caller supplies a real model generator while this composition explicitly
// supplies deterministic tool fixtures.
func NewHandler(
	ctx context.Context,
	database *pgxpool.Pool,
	logger *slog.Logger,
	generator agentrun.TextGenerator,
	configuration agentrun.Configuration,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
) (http.Handler, error) {
	if ctx == nil || database == nil || logger == nil || generator == nil ||
		!configuration.Valid() {
		return nil, errors.New("agent benchmark: dependencies are required")
	}

	identityHandler, err := newIdentityHandler(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
	)
	if err != nil {
		return nil, err
	}
	ids := identity.NewUUIDv4Generator(nil)
	conversationRepository, err := conversationpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	conversationService, err := agentconversation.NewService(
		conversationRepository,
	)
	if err != nil {
		return nil, err
	}
	contextRepository, err := contextpostgres.New(database)
	if err != nil {
		return nil, err
	}
	contextAssembler, err := agentcontext.NewAssembler(
		contextRepository,
		agentcontext.Instruction{
			Version: "benchmark-agent-v1",
			Content: "You are the Agent routing benchmark.",
		},
		emptyCoachingProfileContributor{},
	)
	if err != nil {
		return nil, err
	}
	runRepository, err := runpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	fixtureTools := capabilityfixture.Tools(capabilityfixture.NewStore())
	fixtureTools = append(
		fixtureTools,
		preparationcapability.NewIELTSWarmUpTool(),
		preparationcapability.NewPreviewTool(benchmarkPreviewPort{}),
	)
	fixtureRegistry, err := capability.NewRegistry(fixtureTools...)
	if err != nil {
		return nil, err
	}
	runService, err := agentrun.NewService(
		runRepository,
		conversationRepository,
		contextAssembler,
		generator,
		configuration,
		agentrun.WithRunLogger(logger),
		agentrun.WithToolRegistry(fixtureRegistry),
	)
	if err != nil {
		return nil, err
	}
	renderer := httpresponse.NewRenderer(nil)
	conversationHandler, err := conversationhttp.NewHandler(
		conversationService,
		renderer,
		conversationhttp.WithClientActions(runService),
	)
	if err != nil {
		return nil, err
	}
	runHandler, err := runhttp.NewHandler(runService, renderer)
	if err != nil {
		return nil, err
	}
	routes := &benchmarkRoutes{
		identity:     identityHandler,
		conversation: conversationHandler,
		runs:         runHandler,
	}
	return app.NewRouterWithReadinessAndRoutes(
		logger,
		database,
		[]app.RouteRegistrar{routes},
	), nil
}

type benchmarkPreviewPort struct{}

func (benchmarkPreviewPort) PreviewPractice(
	_ context.Context,
	_ capability.CallContext,
	input preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	mode := input.IELTSPracticeMode
	if mode == "" {
		mode = "FULL_SIMULATION"
	}
	action, err := agentclientaction.New(
		preparationcapability.ConfirmPracticePlanActionType,
		json.RawMessage(`{"practice_mode":"`+mode+`"}`),
	)
	if err != nil {
		return preparationcapability.PreviewResult{}, err
	}
	return preparationcapability.PreviewResult{
		Status:       "preview_ready",
		ClientAction: action,
	}, nil
}

func newIdentityHandler(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
) (*identity.HTTPHandler, error) {
	passwords, err := identity.NewDefaultArgon2idHasher()
	if err != nil {
		return nil, err
	}
	tokens := identity.NewOpaqueSessionTokens(nil)
	dummyMaterial, _, err := tokens.Generate()
	if err != nil {
		return nil, err
	}
	dummyHash, err := passwords.Hash(ctx, dummyMaterial)
	if err != nil {
		return nil, err
	}
	repository, err := identity.NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		return nil, err
	}
	service, err := identity.NewService(
		repository,
		passwords,
		tokens,
		dummyHash,
	)
	if err != nil {
		return nil, err
	}
	rateLimits, err := identity.NewDefaultRateLimiters(identity.SystemClock{})
	if err != nil {
		return nil, err
	}
	sourceIPs, err := identity.NewTrustedProxyResolver(
		trustedProxyCIDRs,
		trustedProxyHeader,
	)
	if err != nil {
		return nil, err
	}
	return identity.NewHTTPHandler(
		service,
		service,
		service,
		rateLimits,
		nil,
		identity.WithSourceIPResolver(sourceIPs),
	)
}

type benchmarkRoutes struct {
	identity     *identity.HTTPHandler
	conversation *conversationhttp.Handler
	runs         *runhttp.Handler
}

func (routes *benchmarkRoutes) RegisterRoutes(router *gin.Engine) {
	routes.identity.RegisterRoutes(router)
	protected := router.Group("")
	protected.Use(routes.identity.AuthenticationMiddleware())
	routes.conversation.RegisterRoutes(protected)
	routes.runs.RegisterRoutes(protected)
}

type emptyCoachingProfileContributor struct{}

func (emptyCoachingProfileContributor) Contribute(
	context.Context,
	requestcontext.Actor,
) (agentcontext.CoachingProfileContribution, error) {
	return agentcontext.CoachingProfileContribution{Enabled: true}, nil
}
