package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/practice"
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
	agentModule            *agent.Module
	agentVoiceReclaimer    AgentVoiceObjectReclaimer
	identityHTTP           *identity.HTTPHandler
	preparationApplication *preparation.PersistenceService
	preparationHTTP        *preparation.ProfileHTTPHandler
	practiceApplication    *practice.ContextApplication
	practiceHTTP           *practice.ContextHTTPHandler
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
	runConfiguration agent.RunConfiguration,
	catalog preparation.CatalogReader,
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
	practiceHTTP, err := practice.NewContextHTTPHandler(practiceApplication)
	if err != nil {
		return nil, err
	}

	return &IdentityAgentPracticeComposition{
		identityModule:         base.identity.module,
		agentModule:            base.agentModule,
		agentVoiceReclaimer:    base.agentVoiceReclaimer,
		identityHTTP:           base.identity.handler,
		preparationApplication: preparationApplication,
		preparationHTTP:        preparationHTTP,
		practiceApplication:    practiceApplication,
		practiceHTTP:           practiceHTTP,
	}, nil
}

func (c *IdentityAgentPracticeComposition) IdentityModule() *identity.Module {
	if c == nil {
		return nil
	}
	return c.identityModule
}

func (c *IdentityAgentPracticeComposition) AgentModule() *agent.Module {
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

func (c *IdentityAgentPracticeComposition) PreparationApplication() *preparation.PersistenceService {
	if c == nil {
		return nil
	}
	return c.preparationApplication
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
		c.practiceHTTP == nil {
		return nil, errors.New(
			"bootstrap: authenticated context routes are unavailable",
		)
	}
	registrars := make([]ProtectedRouteRegistrar, 0, len(additional)+2)
	registrars = append(registrars, c.preparationHTTP, c.practiceHTTP)
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
	) (agent.Thread, error)
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
	case errors.Is(err, agent.ErrInvalidRequest):
		return practicepersistence.ErrInvalidArgument
	case errors.Is(err, agent.ErrNotFound):
		return practicepersistence.ErrNotFound
	case errors.Is(err, agent.ErrConflict):
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
		ID:                     snapshot.ID,
		SourceProfileID:        snapshot.SourceProfileID,
		SourceVersion:          snapshot.SourceVersion,
		ResumeSnapshot:         snapshot.ResumeSnapshot,
		JobDescriptionSnapshot: snapshot.JobDescriptionSnapshot,
		BackgroundSnapshot:     snapshot.BackgroundSnapshot,
		CreatedAt:              snapshot.CreatedAt,
	}, nil
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
	fullOption, found := fullSimulationOption(detail.PracticeOptions)
	if !found {
		return practice.PlanCatalogSelection{},
			practicepersistence.ErrNotFound
	}
	snapshot, err := r.catalog.GetCatalogSnapshot(
		request.ScenarioDefinitionID,
		request.ScenarioDefinitionVersion,
		append([]string(nil), request.SelectedRoleIDs...),
		fullOption.ID,
		fullOption.Version,
	)
	if err != nil {
		return practice.PlanCatalogSelection{}, mapPracticeCatalogError(err)
	}
	if snapshot.ScenarioConfig.ID != request.ScenarioConfigID ||
		snapshot.ScenarioConfig.Version != request.ScenarioConfigVersion {
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

func exactPlanCatalogRequest(
	request practice.PlanCatalogRequest,
	detail preparation.ScenarioDetail,
) bool {
	return detail.ScenarioDefinition.ID == request.ScenarioDefinitionID &&
		detail.ScenarioDefinition.Version ==
			request.ScenarioDefinitionVersion &&
		detail.ScenarioDefinition.Status == preparation.ScenarioStatusActive &&
		detail.ScenarioConfig.ID == request.ScenarioConfigID &&
		detail.ScenarioConfig.Version == request.ScenarioConfigVersion &&
		detail.ScenarioConfig.ScenarioDefinitionID ==
			request.ScenarioDefinitionID
}

func fullSimulationOption(
	options []preparation.PracticeOptionDefinition,
) (preparation.PracticeOptionDefinition, bool) {
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
			ID:      snapshot.ScenarioDefinition.ID,
			Type:    string(snapshot.ScenarioDefinition.Type),
			Name:    snapshot.ScenarioDefinition.Name,
			Version: snapshot.ScenarioDefinition.Version,
			Status:  string(snapshot.ScenarioDefinition.Status),
		},
		ScenarioConfig: practicepersistence.ScenarioConfigSnapshot{
			ID: snapshot.ScenarioConfig.ID,
			ScenarioDefinitionID: snapshot.ScenarioConfig.
				ScenarioDefinitionID,
			Type:           string(snapshot.ScenarioConfig.Type),
			Version:        snapshot.ScenarioConfig.Version,
			JobTitle:       snapshot.ScenarioConfig.JobTitle,
			JobDescription: snapshot.ScenarioConfig.JobDescription,
			FocusAreas: append(
				[]string(nil),
				snapshot.ScenarioConfig.FocusAreas...,
			),
		},
		SelectedRoles: roles,
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
	_ practice.AgentContextReader       = (*agentPracticeContextReader)(nil)
	_ practice.PreparationContextReader = (*preparationPracticeContextReader)(nil)
	_ practice.CatalogContextReader     = (*practiceCatalogContextReader)(nil)
	_ RouteRegistrar                    = (*bearerProtectedRoutes)(nil)
)
