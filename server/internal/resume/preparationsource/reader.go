// Package preparationsource adapts Resume-owned revisions into Preparation's
// consumer-owned material contract.
package preparationsource

import (
	"context"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

type source interface {
	ReadOwnedRevision(
		context.Context,
		requestcontext.Actor,
		string,
		int64,
	) (resume.Revision, error)
}

// Reader maps Resume's aggregate-owned Revision into Preparation's immutable
// material projection.
type Reader struct {
	source source
}

func New(source source) (*Reader, error) {
	if source == nil {
		return nil, fmt.Errorf("Preparation Resume Revision source is required")
	}
	return &Reader{source: source}, nil
}

func (reader *Reader) ReadOwnedRevision(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
	revision int64,
) (preparation.ResumeRevisionSnapshot, error) {
	if reader == nil || reader.source == nil || ctx == nil || !actor.Valid() {
		return preparation.ResumeRevisionSnapshot{}, preparation.ErrProfileInvalid
	}
	value, err := reader.source.ReadOwnedRevision(
		ctx,
		actor,
		resumeID,
		revision,
	)
	if err != nil {
		return preparation.ResumeRevisionSnapshot{}, mapError(err)
	}
	if value.ResumeID != resumeID || value.Revision != revision {
		return preparation.ResumeRevisionSnapshot{}, preparation.ErrProfileConflict
	}
	return preparation.ResumeRevisionSnapshot{
		ResumeID: value.ResumeID,
		Revision: value.Revision,
		Material: mapMaterial(value.Content),
	}, nil
}

func mapMaterial(content resume.Content) preparation.ResumeMaterial {
	material := preparation.ResumeMaterial{
		TargetPosition:       content.TargetPosition,
		ProfessionalSummary:  content.ProfessionalSummary,
		WorkExperiences:      make([]preparation.ResumeWorkExperience, len(content.WorkExperiences)),
		ProjectExperiences:   make([]preparation.ResumeProjectExperience, len(content.ProjectExperiences)),
		EducationExperiences: make([]preparation.ResumeEducationExperience, len(content.EducationExperiences)),
		Skills:               append([]string{}, content.Skills...),
		Awards:               append([]string{}, content.Awards...),
	}
	for index, item := range content.WorkExperiences {
		material.WorkExperiences[index] = preparation.ResumeWorkExperience{
			Company: item.Company, Position: item.Position,
			StartDate: item.StartDate, EndDate: item.EndDate,
			Duties:       append([]string{}, item.Duties...),
			Achievements: append([]string{}, item.Achievements...),
		}
	}
	for index, item := range content.ProjectExperiences {
		material.ProjectExperiences[index] = preparation.ResumeProjectExperience{
			ProjectName: item.ProjectName, Role: item.Role,
			Description:  item.Description,
			Technologies: append([]string{}, item.Technologies...),
			Duties:       append([]string{}, item.Duties...),
			Achievements: append([]string{}, item.Achievements...),
		}
	}
	for index, item := range content.EducationExperiences {
		material.EducationExperiences[index] = preparation.ResumeEducationExperience{
			School: item.School, Major: item.Major, Degree: item.Degree,
			GPA: item.GPA, StartDate: item.StartDate, EndDate: item.EndDate,
		}
	}
	return material
}

func mapError(err error) error {
	switch {
	case apperror.IsCategory(err, apperror.InvalidArgument):
		return preparation.ErrProfileInvalid
	case apperror.IsCategory(err, apperror.NotFound):
		return preparation.ErrProfileNotFound
	case apperror.IsCategory(err, apperror.Conflict),
		apperror.IsCategory(err, apperror.FailedPrecondition):
		return preparation.ErrProfileConflict
	default:
		return fmt.Errorf("read Resume Revision for Preparation: %w", err)
	}
}

var _ preparation.ResumeRevisionReader = (*Reader)(nil)
