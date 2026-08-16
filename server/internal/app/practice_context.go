package app

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceapi "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/api"
	practiceplanpolicy "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/planpolicy"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	preparationsource "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/preparationsource"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	preparationagentthread "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentthread"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume"
	preparationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/repository/postgres"
	preparationrouter "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/router"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
	preparationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/transport/http"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProtectedRouteRegistrar is the delivery boundary accepted by the shared
// Bearer-protected route group. Preparation and Practice handlers register
// routes without owning or copying Identity's credential parser.
type ProtectedRouteRegistrar interface {
	RegisterRoutes(gin.IRoutes)
}

// preparationCatalog is the exact Scene surface consumed while composing
// Preparation: candidate generation reads catalog definitions and Plan
// creation resolves an authenticated, currently accessible selection.
type preparationCatalog interface {
	scene.CatalogReader
	scene.AccessibleSelectionReader
}

// IdentityAgentPracticeComposition contains the production modules and the
// narrow Preparation/Practice applications that share their Identity, Agent,
// and ID-generation dependencies.
type IdentityAgentPracticeComposition struct {
	identityModule       *identity.Module
	agentModule          RouteRegistrar
	mediaReclaimer       MediaObjectReclaimer
	summaryProcessor     agentsummary.Processor
	identityHTTP         *identity.HTTPHandler
	interviewApplication *preparation.InterviewPreparationService
	preparationHTTP      *preparationrouter.Router
	planApplication      *preparationservice.PlanService
	practiceApplication  *practice.SessionApplication
	practiceRepository   *practicepostgres.Repository
	practiceHTTP         *practiceapi.Handler
	productionTools      *capability.Registry
}

// NewIdentityAgentAndPracticeComposition builds the production Identity,
// Agent, Preparation, and Practice context vertical once. The optional
// Voice composition remains unchanged and continues to use its existing Ports.
func NewIdentityAgentAndPracticeComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	catalog preparationCatalog,
	ieltsQuestions ielts.QuestionSetResolver,
	jobTargetGenerator preparation.JobTargetGenerator,
	evaluationSchedulers PracticeEvaluationSchedulers,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		catalog,
		ieltsQuestions,
		jobTargetGenerator,
		evaluationSchedulers,
		nil,
		nil,
		nil,
		voiceConfigurations...,
	)
}

type AgentWorkerWakeups struct {
	ThreadSummary interface{ Notify() }
}

func NewIdentityAgentAndPracticeCompositionWithWorkerWakeups(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	modelProviders AgentModelProviders,
	runConfiguration agentrun.Configuration,
	catalog preparationCatalog,
	ieltsQuestions ielts.QuestionSetResolver,
	jobTargetGenerator preparation.JobTargetGenerator,
	evaluationSchedulers PracticeEvaluationSchedulers,
	wakeups AgentWorkerWakeups,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		catalog,
		ieltsQuestions,
		jobTargetGenerator,
		evaluationSchedulers,
		wakeups.ThreadSummary,
		nil,
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
	catalog preparationCatalog,
	ieltsQuestions ielts.QuestionSetResolver,
	jobTargetGenerator preparation.JobTargetGenerator,
	evaluationSchedulers PracticeEvaluationSchedulers,
	wakeups AgentWorkerWakeups,
	imageConfiguration *AgentImageConfiguration,
	resumeConfiguration *InterviewResumeConfiguration,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		catalog,
		ieltsQuestions,
		jobTargetGenerator,
		evaluationSchedulers,
		wakeups.ThreadSummary,
		imageConfiguration,
		resumeConfiguration,
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
	catalog preparationCatalog,
	ieltsQuestions ielts.QuestionSetResolver,
	jobTargetGenerator preparation.JobTargetGenerator,
	evaluationSchedulers PracticeEvaluationSchedulers,
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	resumeConfiguration *InterviewResumeConfiguration,
	voiceConfigurations ...RuntimeAudioConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	if catalog == nil || ieltsQuestions == nil || jobTargetGenerator == nil ||
		!evaluationSchedulers.valid() {
		return nil, errors.New("bootstrap: Preparation dependencies are required")
	}
	if len(voiceConfigurations) == 1 {
		voiceConfigurations[0].PracticeInteraction.Evaluation = evaluationSchedulers
	}
	base, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		modelProviders,
		runConfiguration,
		summaryNotifier,
		imageConfiguration,
		resumeConfiguration,
		voiceConfigurations...,
	)
	if err != nil {
		return nil, err
	}

	var resumeExtractor preparation.InterviewResumeExtractor
	if resumeConfiguration != nil {
		if base.mediaService == nil || resumeConfiguration.Parser == nil {
			return nil, errors.New("bootstrap: Interview Resume dependencies are required")
		}
		resumeExtractor, err = interviewresume.New(
			base.mediaService,
			resumeConfiguration.Parser,
		)
		if err != nil {
			return nil, err
		}
	}
	jobTargetParser, err := preparation.NewAIJobTargetParser(
		ctx,
		jobTargetGenerator,
		catalog,
	)
	if err != nil {
		return nil, err
	}
	interviewApplication, err := preparation.NewInterviewPreparationService(
		preparationpostgres.NewPostgresInterviewPreparationRepository(database),
		base.ids,
		jobTargetParser,
		catalog,
		resumeExtractor,
	)
	if err != nil {
		return nil, err
	}
	interviewHTTP, err := preparationhttp.NewInterviewPreparationHTTPHandler(
		interviewApplication,
	)
	if err != nil {
		return nil, err
	}

	threadReader, err := preparationagentthread.New(base.agentService)
	if err != nil {
		return nil, err
	}
	planApplication, err := preparationservice.NewPlanService(
		preparationpostgres.NewPostgresPlanRepository(database),
		base.ids,
		interviewApplication,
		threadReader,
		catalog,
		ieltsQuestions,
		practiceplanpolicy.NewResolver(),
	)
	if err != nil {
		return nil, err
	}
	planHTTP, err := preparationhttp.NewPlanHTTPHandler(planApplication)
	if err != nil {
		return nil, err
	}
	preparationHTTP, err := preparationrouter.New(
		interviewHTTP,
		planHTTP,
	)
	if err != nil {
		return nil, err
	}
	practiceRepository, err := practicepostgres.New(
		database,
		evaluationSchedulers.Completion,
		evaluationSchedulers.TurnFeedback,
		base.ids,
	)
	if err != nil {
		return nil, err
	}
	planSource, err := preparationsource.New(planApplication)
	if err != nil {
		return nil, err
	}
	practiceApplication, err := practice.NewSessionApplication(
		practiceRepository,
		base.ids,
		planSource,
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
		)
		if err != nil {
			return nil, err
		}
		if err := base.productionTools.Register(
			preparationagentcapability.NewPreviewTool(previewPort),
		); err != nil {
			return nil, err
		}
		if err := base.productionTools.Register(
			preparationagentcapability.NewIELTSWarmUpTool(),
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
		identityModule:       base.identity.module,
		agentModule:          base.agentModule,
		mediaReclaimer:       base.mediaReclaimer,
		summaryProcessor:     base.summaryProcessor,
		identityHTTP:         base.identity.handler,
		interviewApplication: interviewApplication,
		preparationHTTP:      preparationHTTP,
		planApplication:      planApplication,
		practiceApplication:  practiceApplication,
		practiceRepository:   practiceRepository,
		practiceHTTP:         practiceHTTP,
		productionTools:      base.productionTools,
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

// MediaReclaimer exposes the shared object lifecycle worker port.
func (c *IdentityAgentPracticeComposition) MediaReclaimer() MediaObjectReclaimer {
	if c == nil {
		return nil
	}
	return c.mediaReclaimer
}

func (c *IdentityAgentPracticeComposition) ThreadSummaryProcessor() agentsummary.Processor {
	if c == nil {
		return nil
	}
	return c.summaryProcessor
}

func (c *IdentityAgentPracticeComposition) InterviewPreparationApplication() *preparation.InterviewPreparationService {
	if c == nil {
		return nil
	}
	return c.interviewApplication
}

func (c *IdentityAgentPracticeComposition) PlanApplication() *preparationservice.PlanService {
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

// PracticeRepository exposes the single infrastructure adapter to the root
// composition. Review narrows it through its consumer-owned transaction port.
func (c *IdentityAgentPracticeComposition) PracticeRepository() *practicepostgres.Repository {
	if c == nil {
		return nil
	}
	return c.practiceRepository
}

// ProtectedRoutes always includes the Preparation and Practice context write
// routes. Any additional registrar inherits the exact same Identity
// AuthenticationMiddleware.
func (c *IdentityAgentPracticeComposition) ProtectedRoutes(
	additional ...ProtectedRouteRegistrar,
) (RouteRegistrar, error) {
	if c == nil || c.identityHTTP == nil || c.preparationHTTP == nil ||
		c.practiceHTTP == nil {
		return nil, errors.New(
			"bootstrap: authenticated context routes are unavailable",
		)
	}
	registrars := make([]ProtectedRouteRegistrar, 0, len(additional)+4)
	registrars = append(
		registrars,
		c.preparationHTTP,
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
