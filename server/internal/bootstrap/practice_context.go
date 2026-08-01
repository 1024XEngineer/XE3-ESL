package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/summary"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/memory"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
	practiceagenttool "github.com/1024XEngineer/XE3-ESL/server/internal/practice/agenttool"
	practicepersistence "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
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
// Matter, and ID-generation dependencies.
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
	practiceApplication    *practice.ContextApplication
	practiceHTTP           *practice.ContextHTTPHandler
	productionTools        *tool.Registry
}

// NewIdentityAgentAndPracticeComposition builds the production Identity,
// Agent, Matter, Preparation, and Practice context vertical once. The optional
// Voice composition remains unchanged and continues to use its existing Ports.
func NewIdentityAgentAndPracticeComposition(
	ctx context.Context,
	database *pgxpool.Pool,
	trustedProxyCIDRs []string,
	trustedProxyHeader string,
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	catalog preparation.CatalogReader,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		catalog,
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
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	catalog preparation.CatalogReader,
	memoryExtractionNotifier interface{ Notify() },
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		catalog,
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
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	catalog preparation.CatalogReader,
	wakeups AgentWorkerWakeups,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		catalog,
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
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	catalog preparation.CatalogReader,
	wakeups AgentWorkerWakeups,
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	return newIdentityAgentAndPracticeComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
		runConfiguration,
		memorySearcher,
		catalog,
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
	generator ai.TextGenerator,
	runConfiguration core.RunConfiguration,
	memorySearcher memory.Searcher,
	catalog preparation.CatalogReader,
	memoryExtractionNotifier interface{ Notify() },
	summaryNotifier interface{ Notify() },
	imageConfiguration *AgentImageConfiguration,
	voiceConfigurations ...VoiceConfiguration,
) (*IdentityAgentPracticeComposition, error) {
	if catalog == nil {
		return nil, errors.New("bootstrap: Preparation catalog is required")
	}
	base, err := buildIdentityAgentComposition(
		ctx,
		database,
		trustedProxyCIDRs,
		trustedProxyHeader,
		generator,
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
		generator,
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

	agentContext, err := newAgentPracticeContextReader(
		base.agentService,
		base.matterService,
	)
	if err != nil {
		return nil, err
	}
	preparationContext, err := newPreparationPracticeContextReader(
		preparationApplication,
	)
	if err != nil {
		return nil, err
	}
	catalogContext, err := newPracticeCatalogContextReader(catalog)
	if err != nil {
		return nil, err
	}
	practiceApplication, err := practice.NewContextApplication(
		practicepostgres.New(database),
		base.ids,
		agentContext,
		preparationContext,
		catalogContext,
	)
	if err != nil {
		return nil, err
	}
	if base.productionTools != nil {
		previewCatalog, err := preparation.NewCatalogPreviewResolver(catalog)
		if err != nil {
			return nil, err
		}
		previewPort, err := practiceagenttool.NewServicePort(
			practiceApplication,
			previewCatalog,
			preparationApplication,
		)
		if err != nil {
			return nil, err
		}
		if err := base.productionTools.Register(
			practiceagenttool.NewPreviewTool(previewPort),
		); err != nil {
			return nil, err
		}
		startPort, err := practiceagenttool.NewStartServicePort(
			practiceApplication,
		)
		if err != nil {
			return nil, err
		}
		if err := base.productionTools.Register(
			practiceagenttool.NewStartTool(startPort),
		); err != nil {
			return nil, err
		}
	}
	practiceHTTP, err := practice.NewContextHTTPHandler(practiceApplication)
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

func (c *IdentityAgentPracticeComposition) PracticeApplication() *practice.ContextApplication {
	if c == nil {
		return nil
	}
	return c.practiceApplication
}

// ResolveSessionByThread exposes only the exact Practice-owned resolver needed
// by the later voice integration. It does not advance voice lifecycle state.
func (c *IdentityAgentPracticeComposition) ResolveSessionByThread(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
) (practicepersistence.ContextSessionBootstrap, error) {
	if c == nil || c.practiceApplication == nil {
		return practicepersistence.ContextSessionBootstrap{},
			practicepersistence.ErrInvalidArgument
	}
	return c.practiceApplication.ResolveSessionByThread(ctx, actor, threadID)
}

// ProtectedRoutes always includes the Preparation and Practice context write
// routes. Any additional registrar inherits the exact same Identity
// AuthenticationMiddleware.
func (c *IdentityAgentPracticeComposition) ProtectedRoutes(
	additional ...ProtectedRouteRegistrar,
) (RouteRegistrar, error) {
	if c == nil || c.identityHTTP == nil || c.preparationHTTP == nil ||
		c.jobTargetHTTP == nil ||
		c.practiceHTTP == nil {
		return nil, errors.New(
			"bootstrap: authenticated context routes are unavailable",
		)
	}
	registrars := make([]ProtectedRouteRegistrar, 0, len(additional)+3)
	registrars = append(
		registrars,
		c.preparationHTTP,
		c.jobTargetHTTP,
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

type agentThreadReader interface {
	GetThread(
		context.Context,
		requestcontext.Actor,
		string,
	) (core.Thread, error)
}

type agentPracticeContextReader struct {
	threads agentThreadReader
	matters matter.Reader
}

func newAgentPracticeContextReader(
	threads agentThreadReader,
	matters matter.Reader,
) (*agentPracticeContextReader, error) {
	if threads == nil || matters == nil {
		return nil, errors.New(
			"bootstrap: Agent practice context dependencies are required",
		)
	}
	return &agentPracticeContextReader{
		threads: threads,
		matters: matters,
	}, nil
}

func (r *agentPracticeContextReader) ValidatePracticeAnchor(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	matterID string,
) (practice.PracticeAnchor, error) {
	if r == nil || r.threads == nil || r.matters == nil ||
		ctx == nil || !actor.Valid() {
		return practice.PracticeAnchor{},
			practicepersistence.ErrInvalidArgument
	}
	thread, err := r.threads.GetThread(ctx, actor, threadID)
	if err != nil {
		return practice.PracticeAnchor{}, mapAgentPracticeContextError(err)
	}
	if thread.ID != threadID || thread.OwnerID != actor.UserID {
		return practice.PracticeAnchor{}, practicepersistence.ErrNotFound
	}
	if matterID == "" {
		return practice.PracticeAnchor{ThreadID: thread.ID}, nil
	}
	if thread.ActiveMatterID == "" || thread.ActiveMatterID != matterID {
		return practice.PracticeAnchor{}, practicepersistence.ErrConflict
	}
	item, err := r.matters.ReadOwned(ctx, actor, matterID)
	if err != nil {
		return practice.PracticeAnchor{}, mapMatterPracticeContextError(err)
	}
	if item.ID != matterID || item.OwnerID != actor.UserID {
		return practice.PracticeAnchor{}, practicepersistence.ErrNotFound
	}
	if item.Status != matter.StatusActive {
		return practice.PracticeAnchor{}, practicepersistence.ErrConflict
	}
	return practice.PracticeAnchor{
		ThreadID: thread.ID,
		MatterID: item.ID,
	}, nil
}

func mapAgentPracticeContextError(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidRequest):
		return practicepersistence.ErrInvalidArgument
	case errors.Is(err, core.ErrNotFound):
		return practicepersistence.ErrNotFound
	case errors.Is(err, core.ErrConflict):
		return practicepersistence.ErrConflict
	default:
		return fmt.Errorf("bootstrap: read Agent practice context: %w", err)
	}
}

func mapMatterPracticeContextError(err error) error {
	switch {
	case errors.Is(err, matter.ErrInvalidRequest):
		return practicepersistence.ErrInvalidArgument
	case errors.Is(err, matter.ErrNotFound):
		return practicepersistence.ErrNotFound
	case errors.Is(err, matter.ErrConflict):
		return practicepersistence.ErrConflict
	default:
		return fmt.Errorf("bootstrap: read Matter practice context: %w", err)
	}
}

type preparationPracticeContextReader struct {
	reader preparation.ProfileSnapshotReader
}

func newPreparationPracticeContextReader(
	reader preparation.ProfileSnapshotReader,
) (*preparationPracticeContextReader, error) {
	if reader == nil {
		return nil, errors.New(
			"bootstrap: Preparation profile reader is required",
		)
	}
	return &preparationPracticeContextReader{reader: reader}, nil
}

func (r *preparationPracticeContextReader) ReadPreparationProfile(
	ctx context.Context,
	actor requestcontext.Actor,
	profileID string,
) (practice.PreparationProfileRef, error) {
	if r == nil || r.reader == nil {
		return practice.PreparationProfileRef{},
			practicepersistence.ErrInvalidArgument
	}
	profile, err := r.reader.ReadProfile(ctx, actor, profileID)
	if err != nil {
		return practice.PreparationProfileRef{},
			mapPreparationPracticeContextError(err)
	}
	if profile.ID != profileID || profile.UserID != actor.UserID ||
		profile.Version < 1 {
		return practice.PreparationProfileRef{},
			practicepersistence.ErrNotFound
	}
	return practice.PreparationProfileRef{
		ID:      profile.ID,
		Version: profile.Version,
	}, nil
}

func (r *preparationPracticeContextReader) ReadPreparationSnapshot(
	ctx context.Context,
	actor requestcontext.Actor,
	snapshotID string,
) (practicepersistence.PreparationSnapshot, error) {
	if r == nil || r.reader == nil {
		return practicepersistence.PreparationSnapshot{},
			practicepersistence.ErrInvalidArgument
	}
	snapshot, err := r.reader.ReadSnapshot(ctx, actor, snapshotID)
	if err != nil {
		return practicepersistence.PreparationSnapshot{},
			mapPreparationPracticeContextError(err)
	}
	if snapshot.ID != snapshotID || snapshot.SourceVersion < 1 {
		return practicepersistence.PreparationSnapshot{},
			practicepersistence.ErrNotFound
	}
	return practicepersistence.PreparationSnapshot{
		ID:                                 snapshot.ID,
		SourceProfileID:                    snapshot.SourceProfileID,
		SourceVersion:                      snapshot.SourceVersion,
		SourceJobTargetID:                  snapshot.SourceJobTargetID,
		SourceJobTargetConfirmationVersion: snapshot.SourceJobTargetConfirmationVersion,
		JobTargetInputSnapshot: mapJobTargetInputSnapshot(
			snapshot.JobTargetInputSnapshot,
		),
		JobTargetCandidateSnapshot: mapJobTargetCandidateSnapshot(
			snapshot.JobTargetCandidateSnapshot,
		),
		ResumeSnapshot:         snapshot.ResumeSnapshot,
		JobDescriptionSnapshot: snapshot.JobDescriptionSnapshot,
		BackgroundSnapshot:     snapshot.BackgroundSnapshot,
		CreatedAt:              snapshot.CreatedAt,
	}, nil
}

func mapJobTargetInputSnapshot(
	input *preparation.JobTargetInput,
) *practicepersistence.JobTargetInputSnapshot {
	if input == nil {
		return nil
	}
	return &practicepersistence.JobTargetInputSnapshot{
		Source:              string(input.Source),
		JobTitle:            input.JobTitle,
		JobDescription:      input.JobDescription,
		Company:             input.Company,
		Seniority:           input.Seniority,
		CandidateBackground: input.CandidateBackground,
		ResumeRef:           input.ResumeRef,
		PracticeFocus:       input.PracticeFocus,
	}
}

func mapJobTargetCandidateSnapshot(
	candidate *preparation.JobTargetCandidate,
) *practicepersistence.JobTargetCandidateSnapshot {
	if candidate == nil {
		return nil
	}
	return &practicepersistence.JobTargetCandidateSnapshot{
		Source:             string(candidate.Source),
		GeneralAdviceOnly:  candidate.GeneralAdviceOnly,
		JobTitle:           candidate.JobTitle,
		Seniority:          candidate.Seniority,
		Responsibilities:   append([]string(nil), candidate.Responsibilities...),
		CoreSkills:         append([]string(nil), candidate.CoreSkills...),
		CommunicationFocus: append([]string(nil), candidate.CommunicationFocus...),
		PracticeGoals:      append([]string(nil), candidate.PracticeGoals...),
		ScopeNotice:        candidate.ScopeNotice,
		CatalogRecommendation: practicepersistence.
			JobTargetCatalogRecommendationSnapshot{
			ScenarioDefinitionID: candidate.CatalogRecommendation.
				ScenarioDefinitionID,
			ScenarioDefinitionVersion: candidate.CatalogRecommendation.
				ScenarioDefinitionVersion,
			SelectedRoleIDs: append(
				[]string(nil),
				candidate.CatalogRecommendation.SelectedRoleIDs...,
			),
			PracticeOptionID: candidate.CatalogRecommendation.
				PracticeOptionID,
			PracticeOptionVersion: candidate.CatalogRecommendation.
				PracticeOptionVersion,
		},
	}
}

func mapPreparationPracticeContextError(err error) error {
	switch {
	case errors.Is(err, preparation.ErrProfileInvalid):
		return practicepersistence.ErrInvalidArgument
	case errors.Is(err, preparation.ErrProfileNotFound):
		return practicepersistence.ErrNotFound
	case errors.Is(err, preparation.ErrProfileConflict),
		errors.Is(err, preparation.ErrProfileIdempotencyConflict),
		errors.Is(err, preparation.ErrProfileDeletionGeneration):
		return practicepersistence.ErrConflict
	default:
		return fmt.Errorf("bootstrap: read Preparation context: %w", err)
	}
}

type practiceCatalogContextReader struct {
	catalog preparation.CatalogReader
}

func newPracticeCatalogContextReader(
	catalog preparation.CatalogReader,
) (*practiceCatalogContextReader, error) {
	if catalog == nil {
		return nil, errors.New("bootstrap: Practice catalog is required")
	}
	return &practiceCatalogContextReader{catalog: catalog}, nil
}

func (r *practiceCatalogContextReader) ReadPlanCatalog(
	request practice.PlanCatalogRequest,
) (practice.PlanCatalogSelection, error) {
	detail, err := r.catalog.GetScenarioDetail(request.ScenarioDefinitionID)
	if err != nil {
		return practice.PlanCatalogSelection{}, mapPracticeCatalogError(err)
	}
	if !exactPlanCatalogRequest(request, detail) {
		return practice.PlanCatalogSelection{},
			practicepersistence.ErrConflict
	}
	option, found := requestedPracticeOption(
		detail.PracticeOptions,
		request.PracticeOptionID,
		request.PracticeOptionVersion,
	)
	if !found {
		return practice.PlanCatalogSelection{},
			practicepersistence.ErrNotFound
	}
	snapshot, err := r.catalog.GetCatalogSnapshot(
		request.ScenarioDefinitionID,
		request.ScenarioDefinitionVersion,
		append([]string(nil), request.SelectedRoleIDs...),
		option.ID,
		option.Version,
	)
	if err != nil {
		return practice.PlanCatalogSelection{}, mapPracticeCatalogError(err)
	}
	if request.ScenarioConfigID != "" &&
		(snapshot.ScenarioConfig.ID != request.ScenarioConfigID ||
			snapshot.ScenarioConfig.Version != request.ScenarioConfigVersion) {
		return practice.PlanCatalogSelection{},
			practicepersistence.ErrConflict
	}
	return mapPlanCatalogSelection(snapshot), nil
}

func (r *practiceCatalogContextReader) ReadSessionCatalog(
	request practice.SessionCatalogRequest,
) (practice.SessionCatalogSelection, error) {
	detail, err := r.catalog.GetScenarioDetail(
		request.Plan.ScenarioDefinitionID,
	)
	if err != nil {
		return practice.SessionCatalogSelection{}, mapPracticeCatalogError(err)
	}
	if detail.ScenarioDefinition.Version !=
		request.Plan.ScenarioDefinitionVersion ||
		detail.ScenarioConfig.ID != request.Plan.ScenarioConfigID ||
		detail.ScenarioConfig.Version != request.Plan.ScenarioConfigVersion {
		return practice.SessionCatalogSelection{},
			practicepersistence.ErrConflict
	}
	option, found := practiceOption(
		detail.PracticeOptions,
		request.PracticeOptionID,
	)
	if !found {
		return practice.SessionCatalogSelection{},
			practicepersistence.ErrNotFound
	}
	snapshot, err := r.catalog.GetCatalogSnapshot(
		request.Plan.ScenarioDefinitionID,
		request.Plan.ScenarioDefinitionVersion,
		append([]string(nil), request.RoleDefinitionIDs...),
		option.ID,
		option.Version,
	)
	if err != nil {
		return practice.SessionCatalogSelection{},
			mapPracticeCatalogError(err)
	}
	if snapshot.ScenarioConfig.ID != request.Plan.ScenarioConfigID ||
		snapshot.ScenarioConfig.Version != request.Plan.ScenarioConfigVersion {
		return practice.SessionCatalogSelection{},
			practicepersistence.ErrConflict
	}
	return practice.SessionCatalogSelection{
		PlanCatalogSelection: mapPlanCatalogSelection(snapshot),
		PracticeOption:       mapPracticeOption(snapshot.PracticeOption),
	}, nil
}

func (r *practiceCatalogContextReader) ResolveIELTSQuestionSet(
	request practice.IELTSQuestionSetRequest,
) (practice.IELTSQuestionSetSelection, error) {
	reader, ok := r.catalog.(preparation.IELTSQuestionBankReader)
	if !ok {
		return practice.IELTSQuestionSetSelection{},
			practicepersistence.ErrConflict
	}
	resolved, err := reader.ResolveIELTSQuestionSet(
		preparation.IELTSQuestionSetSelection{
			Mode:         preparation.IELTSPracticeMode(request.Mode),
			Part1SetID:   request.Part1SetID,
			TopicGroupID: request.TopicGroupID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, preparation.ErrIELTSQuestionSetNotFound):
			return practice.IELTSQuestionSetSelection{},
				practicepersistence.ErrNotFound
		case errors.Is(err, preparation.ErrIELTSPracticeModeInvalid),
			errors.Is(err, preparation.ErrIELTSQuestionBankUnavailable):
			return practice.IELTSQuestionSetSelection{},
				practicepersistence.ErrConflict
		default:
			return practice.IELTSQuestionSetSelection{},
				fmt.Errorf("bootstrap: resolve IELTS question set: %w", err)
		}
	}
	return practice.IELTSQuestionSetSelection{
		BankID:         resolved.BankID,
		Season:         resolved.Season,
		Mode:           string(resolved.Mode),
		Part1SetID:     resolved.Part1SetID,
		TopicGroupID:   resolved.TopicGroupID,
		TopicTitle:     resolved.TopicTitle,
		Part2CueCard:   resolved.Part2CueCard,
		TurnBlueprints: append([]string(nil), resolved.TurnBlueprints...),
		Part1Questions: resolved.Part1Questions,
		Part2Questions: resolved.Part2Questions,
		Part3Questions: resolved.Part3Questions,
	}, nil
}

func exactPlanCatalogRequest(
	request practice.PlanCatalogRequest,
	detail preparation.ScenarioDetail,
) bool {
	return detail.ScenarioDefinition.ID == request.ScenarioDefinitionID &&
		detail.ScenarioDefinition.Version ==
			request.ScenarioDefinitionVersion &&
		detail.ScenarioDefinition.Status == preparation.ScenarioStatusActive &&
		detail.ScenarioConfig.ScenarioDefinitionID ==
			request.ScenarioDefinitionID &&
		(request.ScenarioConfigID == "" ||
			(detail.ScenarioConfig.ID == request.ScenarioConfigID &&
				detail.ScenarioConfig.Version ==
					request.ScenarioConfigVersion))
}

func requestedPracticeOption(
	options []preparation.PracticeOptionDefinition,
	optionID string,
	optionVersion int,
) (preparation.PracticeOptionDefinition, bool) {
	if optionID != "" {
		option, found := practiceOption(options, optionID)
		return option, found && option.Version == optionVersion
	}
	for _, option := range options {
		if option.Type == preparation.PracticeOptionFullSimulation {
			return option, true
		}
	}
	return preparation.PracticeOptionDefinition{}, false
}

func practiceOption(
	options []preparation.PracticeOptionDefinition,
	optionID string,
) (preparation.PracticeOptionDefinition, bool) {
	for _, option := range options {
		if option.ID == optionID {
			return option, true
		}
	}
	return preparation.PracticeOptionDefinition{}, false
}

func mapPlanCatalogSelection(
	snapshot preparation.CatalogSnapshot,
) practice.PlanCatalogSelection {
	roles := make([]practicepersistence.RoleSnapshot, len(snapshot.SelectedRoles))
	for index, role := range snapshot.SelectedRoles {
		roles[index] = mapRoleSnapshot(role)
	}
	return practice.PlanCatalogSelection{
		ScenarioDefinition: practicepersistence.ScenarioDefinitionSnapshot{
			ID: snapshot.ScenarioDefinition.ID,
			Type: practicepersistence.ScenarioFamily(
				snapshot.ScenarioDefinition.Type,
			),
			Model: practicepersistence.ScenarioModel(
				snapshot.ScenarioDefinition.Model,
			),
			Name:             snapshot.ScenarioDefinition.Name,
			Version:          snapshot.ScenarioDefinition.Version,
			Status:           string(snapshot.ScenarioDefinition.Status),
			TurnPolicyRef:    snapshot.ScenarioDefinition.TurnPolicyRef,
			SessionPolicyRef: snapshot.ScenarioDefinition.SessionPolicyRef,
		},
		ScenarioConfig: practicepersistence.ScenarioConfigSnapshot{
			ID: snapshot.ScenarioConfig.ID,
			ScenarioDefinitionID: snapshot.ScenarioConfig.
				ScenarioDefinitionID,
			Type:           practicepersistence.ScenarioFamily(snapshot.ScenarioConfig.Type),
			Model:          practicepersistence.ScenarioModel(snapshot.ScenarioConfig.Model),
			Version:        snapshot.ScenarioConfig.Version,
			JobTitle:       snapshot.ScenarioConfig.JobTitle,
			JobDescription: snapshot.ScenarioConfig.JobDescription,
			PromptModel: practicepersistence.ScenarioPromptModel{
				PublicSceneBrief: snapshot.ScenarioConfig.PromptModel.PublicSceneBrief,
				PracticeGoal:     snapshot.ScenarioConfig.PromptModel.PracticeGoal,
				UserRole:         snapshot.ScenarioConfig.PromptModel.UserRole,
				AIRole:           snapshot.ScenarioConfig.PromptModel.AIRole,
				PersonaSummary:   snapshot.ScenarioConfig.PromptModel.PersonaSummary,
				FocusAreas: append(
					[]string(nil),
					snapshot.ScenarioConfig.PromptModel.FocusAreas...,
				),
				TurnBlueprints: append(
					[]string(nil),
					snapshot.ScenarioConfig.PromptModel.TurnBlueprints...,
				),
				SuggestedDurationSeconds: snapshot.ScenarioConfig.PromptModel.SuggestedDurationSeconds,
			},
		},
		SelectedRoles:  roles,
		PracticeOption: mapPracticeOption(snapshot.PracticeOption),
	}
}

func mapRoleSnapshot(
	role preparation.RoleDefinition,
) practicepersistence.RoleSnapshot {
	return practicepersistence.RoleSnapshot{
		ID:                   role.ID,
		ScenarioDefinitionID: role.ScenarioDefinitionID,
		Type:                 role.Type,
		DisplayName:          role.DisplayName,
		Responsibilities:     role.Responsibilities,
		Style:                role.Style,
		FocusAreas:           append([]string(nil), role.FocusAreas...),
		VoiceConfigRef:       role.VoiceConfigRef,
		Version:              role.Version,
	}
}

func mapPracticeOption(
	option preparation.PracticeOptionDefinition,
) practicepersistence.PracticeOptionSnapshot {
	return practicepersistence.PracticeOptionSnapshot{
		ID:                   option.ID,
		ScenarioDefinitionID: option.ScenarioDefinitionID,
		RoleDefinitionID:     option.RoleDefinitionID,
		Type:                 string(option.Type),
		DisplayName:          option.DisplayName,
		Version:              option.Version,
	}
}

func mapPracticeCatalogError(err error) error {
	switch {
	case errors.Is(err, preparation.ErrScenarioDefinitionNotFound),
		errors.Is(err, preparation.ErrRoleDefinitionNotFound),
		errors.Is(err, preparation.ErrPracticeOptionNotFound):
		return practicepersistence.ErrNotFound
	case errors.Is(err, preparation.ErrCatalogSelectionInvalid):
		return practicepersistence.ErrConflict
	default:
		return fmt.Errorf("bootstrap: read Practice catalog: %w", err)
	}
}

var (
	_ practice.AgentContextReader        = (*agentPracticeContextReader)(nil)
	_ practice.PreparationContextReader  = (*preparationPracticeContextReader)(nil)
	_ practice.CatalogContextReader      = (*practiceCatalogContextReader)(nil)
	_ practice.IELTSCatalogContextReader = (*practiceCatalogContextReader)(nil)
	_ RouteRegistrar                     = (*bearerProtectedRoutes)(nil)
)
