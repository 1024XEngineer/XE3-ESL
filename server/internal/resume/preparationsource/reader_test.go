package preparationsource

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	resumeapp "github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

type sourceStub struct {
	value resume.Revision
	err   error
}

func (stub sourceStub) ReadOwnedRevision(
	context.Context,
	requestcontext.Actor,
	string,
	int64,
) (resume.Revision, error) {
	return stub.value, stub.err
}

func TestReaderProjectsAndCopiesResumeMaterial(t *testing.T) {
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	value := resume.Revision{
		ResumeID: "50000000-0000-4000-8000-000000000001",
		Revision: 3,
		Content: resume.Content{
			WorkExperiences: []resume.WorkExperience{{
				Company: "Example", Duties: []string{"Built APIs"},
			}},
			ProjectExperiences:   []resume.ProjectExperience{},
			EducationExperiences: []resume.EducationExperience{},
			Skills:               []string{"Go"}, Awards: []string{},
		},
	}
	reader, err := New(sourceStub{value: value})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := reader.ReadOwnedRevision(
		context.Background(),
		actor,
		value.ResumeID,
		value.Revision,
	)
	if err != nil || got.ResumeID != value.ResumeID ||
		got.Revision != value.Revision || got.Material.Skills[0] != "Go" {
		t.Fatalf("ReadOwnedRevision = (%+v, %v)", got, err)
	}
	value.Content.Skills[0] = "mutated"
	value.Content.WorkExperiences[0].Duties[0] = "mutated"
	if got.Material.Skills[0] != "Go" ||
		got.Material.WorkExperiences[0].Duties[0] != "Built APIs" {
		t.Fatal("Preparation projection retained Resume-owned slices")
	}
}

func TestReaderMapsResumeRevisionFailures(t *testing.T) {
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "owner", err: resumeapp.ResumeNotFoundError(), want: preparation.ErrProfileNotFound},
		{name: "revision", err: resumeapp.ResumeVersionConflictError(), want: preparation.ErrProfileConflict},
		{name: "parsing", err: resumeapp.ResumeRevisionUnavailableError(), want: preparation.ErrProfileConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, err := New(sourceStub{err: test.err})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = reader.ReadOwnedRevision(
				context.Background(),
				actor,
				"50000000-0000-4000-8000-000000000001",
				1,
			)
			if err != test.want {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
