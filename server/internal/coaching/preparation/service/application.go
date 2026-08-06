package service

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

type ProfileService = preparation.PersistenceService
type JobTargetApplication = preparation.JobTargetService

func NewProfileService(
	repository preparation.ProfileRepository,
	ids preparation.ResourceIDGenerator,
	resumes preparation.ResumeRevisionReader,
	contexts preparation.ContextResolver,
) (*ProfileService, error) {
	return preparation.NewPersistenceServiceWithContext(
		repository,
		ids,
		resumes,
		contexts,
	)
}

func NewJobTargetApplication(
	repository preparation.JobTargetRepository,
	ids preparation.ResourceIDGenerator,
	parser preparation.JobTargetParser,
	catalog scene.CatalogReader,
) (*JobTargetApplication, error) {
	return preparation.NewJobTargetService(repository, ids, parser, catalog)
}
