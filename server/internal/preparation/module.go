// Package preparation owns scenarios, roles, and confirmed background snapshots.
package preparation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "preparation" }

// CreateProfileRequest is the application input for one preparation profile.
type CreateProfileRequest struct {
	ResumeRef                    string `json:"resume_ref,omitempty"`
	JobDescriptionRef            string `json:"job_description_ref,omitempty"`
	BackgroundSummary            string `json:"background_summary"`
	JobTargetID                  string `json:"job_target_id,omitempty"`
	JobTargetConfirmationVersion int    `json:"job_target_confirmation_version,omitempty"`
}

// CreateSnapshotRequest pins one immutable version of a preparation profile.
type CreateSnapshotRequest struct {
	SourceVersion int `json:"source_version"`
}

// Backend is the persistence/provider-facing boundary owned by Preparation.
// Production and deterministic smoke adapters implement the same application
// operations without exposing transport concerns to the module.
type Backend interface {
	CreateProfile(CreateProfileRequest) (map[string]any, error)
	CreateSnapshot(string, CreateSnapshotRequest) (map[string]any, error)
	ProfileExists(string) bool
	SnapshotExists(string) bool
}

// Service is Preparation's formal application-service entry point.
type Service struct {
	backend Backend
	catalog CatalogReader
}

func NewService(backend Backend, catalog CatalogReader) *Service {
	return &Service{backend: backend, catalog: catalog}
}

func (s *Service) ListActiveScenarios() []ScenarioDefinition {
	return s.catalog.ListActiveScenarios()
}

func (s *Service) GetScenarioDetail(id string) (ScenarioDetail, error) {
	return s.catalog.GetScenarioDetail(id)
}

func (s *Service) ListRoles(scenarioID string) ([]RoleDefinition, error) {
	return s.catalog.ListRoles(scenarioID)
}

func (s *Service) GetCatalogSnapshot(
	scenarioDefinitionID string,
	scenarioVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
	practiceOptionVersion int,
) (CatalogSnapshot, error) {
	return s.catalog.GetCatalogSnapshot(
		scenarioDefinitionID,
		scenarioVersion,
		selectedRoleIDs,
		practiceOptionID,
		practiceOptionVersion,
	)
}

func (s *Service) CreateProfile(request CreateProfileRequest) (map[string]any, error) {
	return s.backend.CreateProfile(request)
}

func (s *Service) CreateSnapshot(
	profileID string,
	request CreateSnapshotRequest,
) (map[string]any, error) {
	return s.backend.CreateSnapshot(profileID, request)
}

func (s *Service) ProfileExists(id string) bool {
	return s.backend.ProfileExists(id)
}

func (s *Service) SnapshotExists(id string) bool {
	return s.backend.SnapshotExists(id)
}
