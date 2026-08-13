// Package benchmark provides the explicit Agent composition used by the real
// model routing benchmark. It is not imported by the production Server.
package benchmark

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	contextpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context/postgres"
	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	runhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/http"
	runpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal"
	goalagentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcontext"
	goalagentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentconversation"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
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
	goalRepository, err := goal.NewPostgresRepository(database, ids)
	if err != nil {
		return nil, err
	}
	goalService, err := goal.NewService(goalRepository)
	if err != nil {
		return nil, err
	}
	conversationGoals, err := goalagentconversation.New(goalService)
	if err != nil {
		return nil, err
	}
	contextGoals, err := goalagentcontext.New(goalService)
	if err != nil {
		return nil, err
	}
	conversationRepository, err := conversationpostgres.New(database, ids)
	if err != nil {
		return nil, err
	}
	conversationService, err := agentconversation.NewService(
		conversationRepository,
		conversationGoals,
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
		contextGoals,
		emptyLearningProfileReader{},
		emptyStableProfileReader{},
		emptyMemorySearcher{},
		readyMemoryExtractionBarrier{},
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
		contextRepository,
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
		conversationhttp.WithToolCalls(runService),
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
	return bootstrap.NewRouterWithReadinessAndRoutes(
		logger,
		database,
		[]bootstrap.RouteRegistrar{routes},
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
	handoff, err := agenthandoff.NewConfirmPracticePlan(agenthandoff.Item{
		Label:                    "确认并开始练习",
		PracticePlanID:           "00000000-0000-4000-8000-000000000648",
		PlanRevision:             1,
		Target:                   "IELTS Speaking practice",
		SceneName:                "IELTS Speaking",
		PracticeExperience:       "IELTS_SPEAKING",
		SceneCategory:            "IELTS_SPEAKING",
		PracticeMode:             mode,
		Roles:                    []string{"IELTS 口语考官"},
		PracticeScope:            "IELTS Speaking",
		SuggestedDurationSeconds: 300,
		MinEffectiveTurns:        1,
		MaxEffectiveTurns:        3,
		ExecutableStatus:         agenthandoff.PracticePlanReadyStatus,
		ConfirmationPrompt:       "确认后开始正式练习。",
	})
	if err != nil {
		return preparationcapability.PreviewResult{}, err
	}
	return preparationcapability.PreviewResult{
		Status:  "preview_ready",
		Handoff: handoff,
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

type emptyLearningProfileReader struct{}

func (emptyLearningProfileReader) ReadLearningProfile(
	context.Context,
	agentcontext.LearningProfileReadRequest,
) ([]agentcontext.LearningProfileDimension, error) {
	return []agentcontext.LearningProfileDimension{}, nil
}

type emptyStableProfileReader struct{}

func (emptyStableProfileReader) ReadStableProfile(
	context.Context,
	agentcontext.StableProfileReadRequest,
) ([]agentcontext.StableProfileMemory, error) {
	return []agentcontext.StableProfileMemory{}, nil
}

type emptyMemorySearcher struct{}

func (emptyMemorySearcher) Search(
	context.Context,
	agentcontext.MemorySearchRequest,
) ([]agentcontext.MemorySearchHit, error) {
	return []agentcontext.MemorySearchHit{}, nil
}

type readyMemoryExtractionBarrier struct{}

func (readyMemoryExtractionBarrier) Await(
	_ context.Context,
	request agentcontext.MemoryExtractionBarrierRequest,
) (agentcontext.MemoryExtractionBarrierResult, error) {
	return agentcontext.MemoryExtractionBarrierResult{
		PolicyVersion: agentcontext.MemoryExtractionBarrierPolicyV1,
		Cutoff:        request.Cutoff,
		Status:        agentcontext.MemoryExtractionBarrierReady,
	}, nil
}
