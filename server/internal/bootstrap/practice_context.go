package bootstrap

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceapi "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/api"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	preparationagentthread "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentthread"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProtectedRouteRegistrar is the delivery boundary accepted by the shared
// Bearer-protected route group. Preparation and Practice handlers register
// routes without owning or copying Identity's credential parser.
type ProtectedRouteRegistrar interface {
	RegisterRoutes(gin.IRoutes)
}

// IdentityAgentPracticeComposition contains the production modules and the
// narrow Preparation/Practice applications that share their Identity, Agent,
// Goal, and ID-generation dependencies.
type IdentityAgentPracticeComposition struct {
	identityModule         *identity.Module
	agentModule            RouteRegistrar
	agentVoiceReclaimer    AgentVoiceObjectReclaimer
	agentImageReclaimer    AgentImageObjectReclaimer
	memoryExtraction       memory.ExtractionProcessor
	summaryProcessor       agentsummary.Processor
	identityHTTP           *identity.HTTPHandler
	preparationApplication *preparation.PersistenceService
	preparationHTTP        *preparation.ProfileHTTPHandler
	jobTargetApplication   *preparation.JobTargetService
	jobTargetHTTP          *preparation.JobTargetHTTPHandler
	planApplication        *preparation.PlanService
	planHTTP               *preparation.PlanHTTPHandler
	practiceApplication    *practice.SessionApplication
	practiceHTTP           *practiceapi.Handler
	productionTools        *capability.Registry
}

// NewIdentityAgentAndPracticeComposition builds the production Identity,
// Agent, Goal, Preparation, and Practice context vertical once. The optional
// Voice composition remains unchanged and continues to use its existing Ports.
func NewIdentityAgentAndPracticeComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	catalog scene.CatalogReader,
	jobTargetGenerator preparation.JobTargetGenerator,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		memorySearcher,
		catalog,
		jobTargetGenerator,
		nil,
		nil,
		nil,
		voiceConfigurations...,
	)
}

// NewIdentityAgentAndPracticeCompositionWithMemoryWakeup wires a payload-free
// notification emitted only after a completed Agent Run has been committed.
func NewIdentityAgentAndPracticeCompositionWithMemoryWakeup(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	catalog scene.CatalogReader,
	jobTargetGenerator preparation.JobTargetGenerator,
	memoryExtractionNotifier interface{ Notify() },
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		memorySearcher,
		catalog,
		jobTargetGenerator,
		memoryExtractionNotifier,
		nil,
		nil,
		voiceConfigurations...,
	)
}

type AgentWorkerWakeups struct {
	MemoryExtraction interface{ Notify() }
	ThreadSummary    interface{ Notify() }
}

func NewIdentityAgentAndPracticeCompositionWithWorkerWakeups(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	catalog scene.CatalogReader,
	jobTargetGenerator preparation.JobTargetGenerator,
	wakeups AgentWorkerWakeups,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		memorySearcher,
		catalog,
		jobTargetGenerator,
		wakeups.MemoryExtraction,
		wakeups.ThreadSummary,
		nil,
		voiceConfigurations...,
	)
}

func NewIdentityAgentAndPracticeCompositionWithWorkerWakeupsAndImages(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	catalog scene.CatalogReader,
	jobTargetGenerator preparation.JobTargetGenerator,
	wakeups AgentWorkerWakeups,
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		memorySearcher,
		catalog,
		jobTargetGenerator,
		wakeups.MemoryExtraction,
		wakeups.ThreadSummary,
		imageConfiguration,
		voiceConfigurations...,
	)
}

func newIdentityAgentAndPracticeComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	memorySearcher memory.Searcher,
	catalog scene.CatalogReader,
	jobTargetGenerator preparation.JobTargetGenerator,
	memoryExtractionNotifier interface{ Notify() },
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	if catalog == nil || jobTargetGenerator == nil {
		return nil, errors.New("bootstrap: Preparation dependencies are required")
	}
	base, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		memorySearcher,
		memoryExtractionNotifier,
		summaryNotifier,
		imageConfiguration,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, err
	}

	preparationRepository := preparation.NewPostgresProfileRepository(database)
	preparationApplication, err := preparation.NewPersistenceService(
		preparationRepository,
		base.ids,
	)
	if err != nil {
		return nil, err
	}
	preparationHTTP, err := preparation.NewProfileHTTPHandler(
		preparationApplication,
	)
	if err != nil {
		return nil, err
	}
	jobTargetParser, err := preparation.NewAIJobTargetParser(
		ctx,
		jobTargetGenerator,
		catalog,
	)
	if err != nil {
		return nil, err
	}
	jobTargetApplication, err := preparation.NewJobTargetService(
		preparation.NewPostgresJobTargetRepository(database),
		base.ids,
		jobTargetParser,
		catalog,
	)
	if err != nil {
		return nil, err
	}
	jobTargetHTTP, err := preparation.NewJobTargetHTTPHandler(
		jobTargetApplication,
	)
	if err != nil {
		return nil, err
	}

	threadReader, err := preparationagentthread.New(base.agentService)
	if err != nil {
		return nil, err
	}
	planApplication, err := preparation.NewPlanService(
		preparation.NewPostgresPlanRepository(database),
		base.ids,
		preparationApplication,
		base.goalService,
		threadReader,
		catalog,
		preparation.NewPolicyCatalog(),
	)
	if err != nil {
		return nil, err
	}
	planHTTP, err := preparation.NewPlanHTTPHandler(planApplication)
	if err != nil {
		return nil, err
	}
	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	practiceApplication, err := practice.NewSessionApplication(
		practiceRepository,
		base.ids,
		planApplication,
	)
	if err != nil {
		return nil, err
	}
	if base.productionTools != nil {
		previewCatalog, err := scene.NewCatalogPreviewResolver(catalog)
		if err != nil {
			return nil, err
		}
		previewPort, err := preparationagentcapability.NewServicePort(
			planApplication,
			previewCatalog,
			preparationApplication,
		)
		if err != nil {
			return nil, err
		}
		if err := base.productionTools.Register(
			preparationagentcapability.NewPreviewTool(previewPort),
		); err != nil {
			return nil, err
		}
	}
	practiceHTTP, err := practiceapi.NewHandler(practiceApplication)
	if err != nil {
		return nil, err
	}
	if err := base.recoverInterruptedRuns(ctx); err != nil {
		return nil, err
	}

	return &IdentityAgentPracticeComposition{
		identityModule:         base.identity.module,
		agentModule:            base.agentModule,
		agentVoiceReclaimer:    base.agentVoiceReclaimer,
		agentImageReclaimer:    base.agentImageReclaimer,
		memoryExtraction:       base.memoryExtraction,
		summaryProcessor:       base.summaryProcessor,
		identityHTTP:           base.identity.handler,
		preparationApplication: preparationApplication,
		preparationHTTP:        preparationHTTP,
		jobTargetApplication:   jobTargetApplication,
		jobTargetHTTP:          jobTargetHTTP,
		planApplication:        planApplication,
		planHTTP:               planHTTP,
		practiceApplication:    practiceApplication,
		practiceHTTP:           practiceHTTP,
		productionTools:        base.productionTools,
	}, nil
}

func (c *IdentityAgentPracticeComposition) IdentityModule() *identity.Module {
	if c == nil {
		return nil
	}
	return c.identityModule
}

func (c *IdentityAgentPracticeComposition) AgentModule() RouteRegistrar {
	if c == nil {
		return nil
	}
	return c.agentModule
}

// AgentVoiceReclaimer exposes only the lifecycle operation required by the
// server cleanup scheduler.
func (c *IdentityAgentPracticeComposition) AgentVoiceReclaimer() AgentVoiceObjectReclaimer {
	if c == nil {
		return nil
	}
	return c.agentVoiceReclaimer
}

func (c *IdentityAgentPracticeComposition) AgentImageReclaimer() AgentImageObjectReclaimer {
	if c == nil {
		return nil
	}
	return c.agentImageReclaimer
}

// MemoryExtractionProcessor exposes only the bounded batch operation required
// by the server scheduler. Memory keeps job, provider, and persistence details.
func (c *IdentityAgentPracticeComposition) MemoryExtractionProcessor() memory.ExtractionProcessor {
	if c == nil {
		return nil
	}
	return c.memoryExtraction
}

func (c *IdentityAgentPracticeComposition) ThreadSummaryProcessor() agentsummary.Processor {
	if c == nil {
		return nil
	}
	return c.summaryProcessor
}

func (c *IdentityAgentPracticeComposition) PreparationApplication() *preparation.PersistenceService {
	if c == nil {
		return nil
	}
	return c.preparationApplication
}

func (c *IdentityAgentPracticeComposition) JobTargetApplication() *preparation.JobTargetService {
	if c == nil {
		return nil
	}
	return c.jobTargetApplication
}

func (c *IdentityAgentPracticeComposition) PlanApplication() *preparation.PlanService {
	if c == nil {
		return nil
	}
	return c.planApplication
}

func (c *IdentityAgentPracticeComposition) PracticeApplication() *practice.SessionApplication {
	if c == nil {
		return nil
	}
	return c.practiceApplication
}

// ResolveSessionByPlan exposes the exact Practice-owned resolver needed by
// voice integration. It never infers a Plan from Thread state.
func (c *IdentityAgentPracticeComposition) ResolveSessionByPlan(
	ctx context.Context,
	actor requestcontext.Actor,
	planID string,
) (practice.SessionBootstrap, error) {
	if c == nil || c.practiceApplication == nil {
		return practice.SessionBootstrap{},
			practice.ErrInvalidArgument
	}
	return c.practiceApplication.ResolveSessionByPlan(ctx, actor, planID)
}

// ProtectedRoutes always includes the Preparation and Practice context write
// routes. Any additional registrar inherits the exact same Identity
// AuthenticationMiddleware.
func (c *IdentityAgentPracticeComposition) ProtectedRoutes(
	additional ...ProtectedRouteRegistrar,
) (RouteRegistrar, error) {
	if c == nil || c.identityHTTP == nil || c.preparationHTTP == nil ||
		c.jobTargetHTTP == nil ||
		c.planHTTP == nil ||
		c.practiceHTTP == nil {
		return nil, errors.New(
			"bootstrap: authenticated context routes are unavailable",
		)
	}
	registrars := make([]ProtectedRouteRegistrar, 0, len(additional)+4)
	registrars = append(
		registrars,
		c.preparationHTTP,
		c.jobTargetHTTP,
		c.planHTTP,
		c.practiceHTTP,
	)
	for _, registrar := range additional {
		if registrar == nil {
			return nil, errors.New(
				"bootstrap: protected route registrar is required",
			)
		}
		registrars = append(registrars, registrar)
	}
	return &bearerProtectedRoutes{
		authentication: c.identityHTTP.AuthenticationMiddleware(),
		registrars:     registrars,
	}, nil
}

type bearerProtectedRoutes struct {
	authentication gin.HandlerFunc
	registrars     []ProtectedRouteRegistrar
}

func (r *bearerProtectedRoutes) RegisterRoutes(router *gin.Engine) {
	protected := router.Group("")
	protected.Use(r.authentication)
	for _, registrar := range r.registrars {
		registrar.RegisterRoutes(protected)
	}
}

var (
	_ RouteRegistrar = (*bearerProtectedRoutes)(nil)
)
