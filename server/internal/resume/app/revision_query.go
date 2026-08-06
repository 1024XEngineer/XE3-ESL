package app

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// RevisionQuery exposes only the owner-scoped, parsed Revision capability
// needed by downstream business modules.
type RevisionQuery struct {
	repository Repository
}

func NewRevisionQuery(repository Repository) (*RevisionQuery, error) {
	if repository == nil {
		return nil, errors.New("resume: revision repository is required")
	}
	return &RevisionQuery{repository: repository}, nil
}

// ReadOwnedRevision returns the exact current parsed Revision for the Actor.
// A stale Revision number is rejected instead of silently reading either the
// latest or an older Revision.
func (query *RevisionQuery) ReadOwnedRevision(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
	revision int64,
) (resume.Revision, error) {
	if query == nil || query.repository == nil || ctx == nil || ctx.Err() != nil ||
		!actor.Valid() || !validUUID(actor.UserID) || !validUUID(resumeID) ||
		revision < 1 {
		return resume.Revision{}, InvalidResumeError()
	}
	detail, err := query.repository.FindDetailByOwnerAndID(
		ctx,
		actor.UserID,
		resumeID,
	)
	if err != nil {
		return resume.Revision{}, err
	}
	if detail.Resume.ID != resumeID ||
		detail.Resume.OwnerUserID != actor.UserID {
		return resume.Revision{}, ResumeNotFoundError()
	}
	if detail.Resume.Temporary &&
		(detail.Resume.ExpiresAt == nil || !detail.Resume.ExpiresAt.After(time.Now().UTC())) {
		return resume.Revision{}, ResumeNotFoundError()
	}
	if detail.Resume.CurrentRevision != revision {
		return resume.Revision{}, ResumeVersionConflictError()
	}
	if detail.Resume.FileStatus != resume.FileAvailable ||
		detail.Resume.ParseStatus != resume.ParseReady ||
		detail.Revision == nil {
		return resume.Revision{}, ResumeRevisionUnavailableError()
	}
	if detail.Revision.ResumeID != resumeID ||
		detail.Revision.Revision != revision {
		return resume.Revision{}, RepositoryError(
			errors.New("resume: current Revision identity is inconsistent"),
		)
	}
	result := *detail.Revision
	result.Content = normalizeContent(result.Content)
	return result, nil
}
