package preparation

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const interviewServiceTestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

func TestInterviewPreparationCreateParsesResumeAndJobTargetConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	repository := &interviewServiceRepositoryFake{}
	service := newInterviewServiceForTest(t, repository,
		&interviewServiceParserFake{parse: func(context.Context, JobTargetInput) (JobTargetCandidate, error) {
			started <- "job target"
			<-release
			return interviewServiceCandidate(), nil
		}},
		&interviewServiceResumeFake{extract: func(context.Context, string, string, InterviewResumeUpload) (ResumeMaterial, error) {
			started <- "resume"
			<-release
			return interviewServiceResume(), nil
		}},
	)
	result := make(chan error, 1)
	go func() {
		_, _, err := service.Create(context.Background(), interviewServiceActor(), "interview-create-key", interviewServiceRequest(true))
		result <- err
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case task := <-started:
			seen[task] = true
		case <-time.After(time.Second):
			t.Fatal("resume and job-target parsing did not start concurrently")
		}
	}
	if !seen["resume"] || !seen["job target"] {
		t.Fatalf("started tasks=%v", seen)
	}
	close(release)
	released = true
	if err := <-result; err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repository.createCalls != 1 || repository.command.ResumeContent == nil {
		t.Fatalf("create calls=%d command=%#v", repository.createCalls, repository.command)
	}
}

func TestInterviewPreparationCreateWithoutResumeOnlyParsesJobTarget(t *testing.T) {
	repository := &interviewServiceRepositoryFake{}
	parserCalls := 0
	service := newInterviewServiceForTest(t, repository,
		&interviewServiceParserFake{parse: func(context.Context, JobTargetInput) (JobTargetCandidate, error) {
			parserCalls++
			return interviewServiceCandidate(), nil
		}},
		nil,
	)

	_, _, err := service.Create(context.Background(), interviewServiceActor(), "interview-create-key", interviewServiceRequest(false))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if parserCalls != 1 || repository.createCalls != 1 || repository.command.ResumeContent != nil {
		t.Fatalf("parser calls=%d repository=%#v", parserCalls, repository)
	}
}

func TestInterviewPreparationCreateDoesNotPersistParallelParseFailures(t *testing.T) {
	resumeFailure := errors.New("resume failed")
	parserFailure := errors.New("job target failed")
	tests := []struct {
		name       string
		resumeErr  error
		parserErr  error
		wantError  error
		wantJoined bool
	}{
		{name: "resume", resumeErr: resumeFailure, wantError: resumeFailure},
		{name: "job target", parserErr: parserFailure, wantError: parserFailure, wantJoined: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &interviewServiceRepositoryFake{}
			service := newInterviewServiceForTest(t, repository,
				&interviewServiceParserFake{parse: func(context.Context, JobTargetInput) (JobTargetCandidate, error) {
					return interviewServiceCandidate(), test.parserErr
				}},
				&interviewServiceResumeFake{extract: func(context.Context, string, string, InterviewResumeUpload) (ResumeMaterial, error) {
					return interviewServiceResume(), test.resumeErr
				}},
			)

			_, _, err := service.Create(context.Background(), interviewServiceActor(), "interview-create-key", interviewServiceRequest(true))
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error=%v, want %v", err, test.wantError)
			}
			if errors.Is(err, ErrInterviewPreparationGeneration) != test.wantJoined {
				t.Fatalf("generation error=%t, want %t: %v", errors.Is(err, ErrInterviewPreparationGeneration), test.wantJoined, err)
			}
			if repository.createCalls != 0 {
				t.Fatalf("repository Create called %d times", repository.createCalls)
			}
		})
	}
}

func newInterviewServiceForTest(t *testing.T, repository InterviewPreparationRepository, parser JobTargetParser, resumes InterviewResumeExtractor) *InterviewPreparationService {
	t.Helper()
	service, err := NewInterviewPreparationService(repository, interviewServiceIDGenerator{}, parser, interviewServiceCatalog{}, resumes)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func interviewServiceRequest(withResume bool) CreateInterviewPreparationRequest {
	request := CreateInterviewPreparationRequest{Input: JobTargetInput{Source: JobTargetSourceQuickStart, JobTitle: "Backend Engineer"}}
	if withResume {
		request.Resume = &InterviewResumeUpload{
			Body: strings.NewReader("%PDF-1.7"), Size: 8,
			ChecksumSHA256: strings.Repeat("a", 64),
		}
	}
	return request
}

func interviewServiceActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "11111111-1111-4111-8111-111111111111", SessionID: "test-session"}
}

func interviewServiceCandidate() JobTargetCandidate {
	return JobTargetCandidate{
		Source: JobTargetSourceQuickStart, GeneralAdviceOnly: true,
		JobTitle: "Backend Engineer", Seniority: "Mid-level",
		Responsibilities: []string{"Build services"}, CoreSkills: []string{"Go"},
		CommunicationFocus: []string{"Explain trade-offs"}, PracticeGoals: []string{"Answer clearly"},
		ScopeNotice: "General advice only.",
		CatalogRecommendation: JobTargetCatalogRecommendation{
			SceneID: "project-deep-dive", SceneVersion: 1,
			SelectedRoleIDs: []string{"technical-interviewer"}, PracticeOptionID: "full-simulation",
		},
	}
}

func interviewServiceResume() ResumeMaterial {
	return ResumeMaterial{
		WorkExperiences: []ResumeWorkExperience{}, ProjectExperiences: []ResumeProjectExperience{},
		EducationExperiences: []ResumeEducationExperience{}, Skills: []string{}, Awards: []string{},
	}
}

type interviewServiceParserFake struct {
	parse func(context.Context, JobTargetInput) (JobTargetCandidate, error)
}

func (fake *interviewServiceParserFake) ParseJobTarget(ctx context.Context, input JobTargetInput) (JobTargetCandidate, error) {
	return fake.parse(ctx, input)
}

type interviewServiceResumeFake struct {
	extract func(context.Context, string, string, InterviewResumeUpload) (ResumeMaterial, error)
}

func (fake *interviewServiceResumeFake) Extract(ctx context.Context, userID, requestID string, upload InterviewResumeUpload) (ResumeMaterial, error) {
	return fake.extract(ctx, userID, requestID, upload)
}

type interviewServiceIDGenerator struct{}

func (interviewServiceIDGenerator) NewID() (string, error) { return interviewServiceTestID, nil }

type interviewServiceRepositoryFake struct {
	createCalls int
	command     CreateInterviewPreparationCommand
}

func (*interviewServiceRepositoryFake) ReplayCreate(context.Context, requestcontext.Actor, string, [sha256.Size]byte) (InterviewPreparation, bool, error) {
	return InterviewPreparation{}, false, nil
}

func (fake *interviewServiceRepositoryFake) Create(_ context.Context, actor requestcontext.Actor, command CreateInterviewPreparationCommand) (InterviewPreparation, bool, error) {
	fake.createCalls++
	fake.command = command
	return InterviewPreparation{
		ID: command.ID, UserID: actor.UserID, Input: command.Input, Candidate: command.Candidate,
		ResumeContent: command.ResumeContent, Status: InterviewPreparationDraft, Version: 1,
	}, false, nil
}

func (*interviewServiceRepositoryFake) Get(context.Context, requestcontext.Actor, string) (InterviewPreparation, error) {
	return InterviewPreparation{}, ErrInterviewPreparationNotFound
}

func (*interviewServiceRepositoryFake) Patch(context.Context, requestcontext.Actor, PatchInterviewPreparationCommand) (InterviewPreparation, bool, error) {
	return InterviewPreparation{}, false, ErrInterviewPreparationNotFound
}

type interviewServiceCatalog struct{}

func (interviewServiceCatalog) ListActiveScenes(context.Context) ([]scene.SceneDefinition, error) {
	return nil, nil
}

func (interviewServiceCatalog) GetScene(context.Context, string) (scene.SceneDefinition, error) {
	return scene.SceneDefinition{}, scene.ErrSceneNotFound
}

func (interviewServiceCatalog) ListRoles(context.Context, string) ([]scene.RoleDefinition, error) {
	return nil, nil
}

func (interviewServiceCatalog) ResolveSelection(_ context.Context, _ string, _ int, selectedRoleIDs []string, _ string) (scene.SelectionSnapshot, error) {
	return scene.SelectionSnapshot{
		Scene:           scene.ExecutableSceneSnapshot{Experience: scene.PracticeExperienceInterview},
		SelectedRoleIDs: append([]string(nil), selectedRoleIDs...),
	}, nil
}
