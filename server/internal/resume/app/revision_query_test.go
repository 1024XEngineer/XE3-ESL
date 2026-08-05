package app

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

func TestRevisionQueryRequiresExactReadyCurrentRevision(t *testing.T) {
	item := resume.Resume{
		ID:              "50000000-0000-4000-8000-000000000001",
		OwnerUserID:     serviceTestActor.UserID,
		FileStatus:      resume.FileAvailable,
		ParseStatus:     resume.ParseReady,
		CurrentRevision: 2,
	}
	revision := &resume.Revision{
		ResumeID: item.ID,
		Revision: 2,
		Content:  resume.Content{Skills: []string{"Go"}},
	}
	repository := &repositoryFake{item: item, detailRevision: revision}
	query, err := NewRevisionQuery(repository)
	if err != nil {
		t.Fatalf("NewRevisionQuery: %v", err)
	}

	got, err := query.ReadOwnedRevision(
		context.Background(),
		serviceTestActor,
		item.ID,
		2,
	)
	if err != nil || got.ResumeID != item.ID || got.Revision != 2 ||
		got.Content.WorkExperiences == nil || got.Content.Awards == nil {
		t.Fatalf("ReadOwnedRevision = (%+v, %v)", got, err)
	}

	if _, err := query.ReadOwnedRevision(
		context.Background(),
		serviceTestActor,
		item.ID,
		1,
	); !apperror.IsCategory(err, apperror.Conflict) {
		t.Fatalf("stale Revision error = %v", err)
	}

	repository.item.ParseStatus = resume.ParseRunning
	if _, err := query.ReadOwnedRevision(
		context.Background(),
		serviceTestActor,
		item.ID,
		2,
	); !apperror.IsCategory(err, apperror.FailedPrecondition) {
		t.Fatalf("incomplete parse error = %v", err)
	}
}

func TestRevisionQueryRejectsRepositoryOwnershipMismatch(t *testing.T) {
	repository := &repositoryFake{
		item: resume.Resume{
			ID:              "50000000-0000-4000-8000-000000000001",
			OwnerUserID:     "10000000-0000-4000-8000-000000000999",
			FileStatus:      resume.FileAvailable,
			ParseStatus:     resume.ParseReady,
			CurrentRevision: 1,
		},
		detailRevision: &resume.Revision{
			ResumeID: "50000000-0000-4000-8000-000000000001",
			Revision: 1,
		},
	}
	query, err := NewRevisionQuery(repository)
	if err != nil {
		t.Fatalf("NewRevisionQuery: %v", err)
	}
	if _, err := query.ReadOwnedRevision(
		context.Background(),
		serviceTestActor,
		repository.item.ID,
		1,
	); !apperror.IsCategory(err, apperror.NotFound) {
		t.Fatalf("ownership mismatch error = %v", err)
	}
}
