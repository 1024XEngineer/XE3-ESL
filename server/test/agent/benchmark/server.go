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
	turnIntentGenerator preparationcapability.PracticeTurnIntentGenerator,
	configuration agentrun.Configuration,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
) (http.Handler, error) {
	if ctx == nil || database == nil || logger == nil || generator == nil ||
		turnIntentGenerator == nil || !configuration.Valid() {
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
	previewPort, err := newRuntimeBenchmarkPreviewPort(
		logger, conversationRepository, turnIntentGenerator,
	)
	if err != nil {
		return nil, err
	}
	previewTool, err := preparationcapability.NewPreviewTool(previewPort)
	if err != nil {
		return nil, err
	}
	fixtureTools = append(
		fixtureTools,
		preparationcapability.NewIELTSWarmUpTool(),
		previewTool,
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

type benchmarkPreviewPort struct {
	logger           *slog.Logger
	manifest         preparationcapability.PreviewCatalogManifest
	messages         preparationcapability.TrustedMessageReader
	turnIntents      *preparationcapability.PracticeTurnIntentResolver
	authorizedIntent preparationcapability.PracticeTurnIntent
}

func newBenchmarkPreviewPort(
	logger *slog.Logger,
) (*benchmarkPreviewPort, error) {
	manifest, err := previewCatalogManifestFixture()
	if err != nil {
		return nil, err
	}
	return &benchmarkPreviewPort{
		logger: logger, manifest: manifest,
		authorizedIntent: preparationcapability.PracticeTurnIntentRequestCreate,
	}, nil
}

func newRuntimeBenchmarkPreviewPort(
	logger *slog.Logger,
	messages preparationcapability.TrustedMessageReader,
	generator preparationcapability.PracticeTurnIntentGenerator,
) (*benchmarkPreviewPort, error) {
	if messages == nil || generator == nil {
		return nil, errors.New("agent benchmark: practice turn dependencies are required")
	}
	port, err := newBenchmarkPreviewPort(logger)
	if err != nil {
		return nil, err
	}
	resolver, err := preparationcapability.NewPracticeTurnIntentResolver(generator)
	if err != nil {
		return nil, err
	}
	port.messages = messages
	port.turnIntents = resolver
	port.authorizedIntent = ""
	return port, nil
}

func (port *benchmarkPreviewPort) AuthorizePracticeTurn(
	ctx context.Context,
	request capability.ExposureRequest,
) (preparationcapability.PracticeTurnIntent, error) {
	if port == nil || ctx == nil || !request.Actor.Valid() ||
		request.ThreadID == "" || request.RunID == "" ||
		request.InputMessageID == "" {
		return "", capability.ErrExecutionRejected
	}
	if port.authorizedIntent != "" {
		return port.authorizedIntent, nil
	}
	if port.messages == nil || port.turnIntents == nil {
		return "", capability.ErrExecutionRejected
	}
	message, err := port.messages.FindMessage(
		ctx, request.Actor.UserID, request.ThreadID, request.InputMessageID,
	)
	if err != nil || message.Role != agentconversation.MessageRoleUser ||
		message.ID != request.InputMessageID || message.ThreadID != request.ThreadID ||
		message.OwnerID != request.Actor.UserID || message.Sequence < 1 {
		return "", capability.ErrExecutionRejected
	}
	return port.turnIntents.Resolve(ctx, message.Content, false)
}

func (port *benchmarkPreviewPort) PreviewCatalogManifest() preparationcapability.PreviewCatalogManifest {
	return port.manifest
}

func (port *benchmarkPreviewPort) PreviewPractice(
	_ context.Context,
	call capability.CallContext,
	input preparationcapability.PreviewInput,
) (preparationcapability.PreviewResult, error) {
	port.logger.Info(
		"agent.benchmark.preview.input",
		"run_id", call.RunID,
		"thread_id", call.ThreadID,
		"tool_call_id", call.ToolCallID,
		"kind", string(input.SceneResolution.Kind),
		"catalog_scene_id", input.SceneResolution.CatalogSceneID,
		"candidate_scene_ids", append(
			[]string{},
			input.SceneResolution.CandidateSceneIDs...,
		),
	)
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindNeedsClarification {
		status := preparationcapability.PreviewOutcomeAmbiguous
		resolution := preparationcapability.SceneResolutionAmbiguous
		if len(input.SceneResolution.CandidateSceneIDs) == 1 {
			status = preparationcapability.PreviewOutcomeNeedsDetails
			resolution = preparationcapability.SceneResolutionNeedsDetails
		}
		candidates := make([]preparationcapability.CatalogCandidate, len(input.SceneResolution.CandidateSceneIDs))
		for index, id := range input.SceneResolution.CandidateSceneIDs {
			candidates[index] = preparationcapability.CatalogCandidate{SceneID: id}
		}
		return preparationcapability.PreviewResult{
			Status:                status,
			SceneResolution:       resolution,
			CatalogCandidateCount: len(candidates),
			Candidates:            candidates,
			AssistantText:         "请选择一个具体场景后再继续。",
		}, nil
	}
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindCustom &&
		(input.SceneIntent.ExperienceHint == "INTERVIEW" ||
			input.SceneIntent.ExperienceHint == "IELTS_SPEAKING") {
		return preparationcapability.PreviewResult{
			Status:           preparationcapability.PreviewOutcomeRequiresSpecializedFlow,
			SceneResolution:  preparationcapability.SceneResolutionRejected,
			ResolutionReason: preparationcapability.ResolutionReasonSpecializedFlowRequired,
			AssistantText: "面试和雅思练习使用各自的正式准备流程。" +
				"请选择目录中的面试或雅思场景。",
		}, nil
	}
	action, err := agentclientaction.New(
		preparationcapability.ConfirmPracticePlanActionType,
		json.RawMessage(`{
  "label": "确认并开始练习",
  "practice_plan_id": "00000000-0000-4000-8000-000000000001",
  "plan_version": 1,
  "scene_id": "benchmark_scene",
  "scene_name": "Agent Routing Benchmark",
  "user_role": "练习者",
  "ai_roles": ["对话角色"],
  "practice_goal": "验证 Agent 场景路由",
  "practice_experience": "WORKPLACE",
  "scene_category": "WORKPLACE_GENERAL",
  "practice_mode": "FULL_SIMULATION",
  "practice_scope": "完整模拟",
  "suggested_duration_seconds": 480,
  "min_effective_turns": 1,
  "max_effective_turns": 5,
  "confirmation_prompt": "确认后将创建练习会话；确认前不会开始练习。"
}`),
	)
	if err != nil {
		return preparationcapability.PreviewResult{}, err
	}
	resolution := preparationcapability.SceneResolutionCatalogResolved
	source := preparationcapability.PreviewPlanSourceCatalog
	if input.SceneResolution.Kind == preparationcapability.SceneResolutionKindCustom {
		resolution = preparationcapability.SceneResolutionCustomResolved
		source = preparationcapability.PreviewPlanSourceCustom
	}
	return preparationcapability.PreviewResult{
		Status:          preparationcapability.PreviewOutcomeReady,
		SceneResolution: resolution,
		PlanID:          "00000000-0000-4000-8000-000000000001",
		PlanSource:      source,
		ClientAction:    action,
		AssistantText:   "练习已准备好，请确认开始。",
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
