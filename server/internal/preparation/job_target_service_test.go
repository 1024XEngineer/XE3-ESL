package preparation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestJobTargetServiceKeepsParsedCandidateUnconfirmed(t *testing.T) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	candidate := validJobTargetCandidateFixture(JobTargetSourceQuickStart)
	input := JobTargetInput{
		Source:   JobTargetSourceQuickStart,
		JobTitle: "Platform engineer",
	}
	repository := &jobTargetRepositoryStub{}
	repository.claim = func(
		_ context.Context,
		_ requestcontext.Actor,
		command AnalyzeJobTargetCommand,
	) (JobTarget, JobTargetAnalysisClaim, bool, bool, error) {
		if command.Request.ExpectedInputVersion != 1 ||
			command.Lease != defaultJobTargetAnalysisLease {
			t.Fatalf("unexpected analysis command: %#v", command)
		}
		return JobTarget{
				ID:           command.TargetID,
				Input:        input,
				InputVersion: 1,
				Stage:        JobTargetStageParsing,
			},
			JobTargetAnalysisClaim{
				AttemptID:       "attempt-1",
				TargetID:        command.TargetID,
				OwnerUserID:     "user-1",
				InputVersion:    1,
				AnalysisVersion: 1,
				WorkerToken:     "worker-1",
				LeaseUntil:      time.Now().Add(time.Minute),
				Input:           input,
				Intent:          command.Intent,
			},
			true,
			false,
			nil
	}
	repository.complete = func(
		_ context.Context,
		claim JobTargetAnalysisClaim,
		got JobTargetCandidate,
	) (JobTarget, error) {
		if claim.InputVersion != 1 || !reflect.DeepEqual(got, candidate) {
			t.Fatalf("unexpected completion: %#v %#v", claim, got)
		}
		return JobTarget{
			ID:           claim.TargetID,
			UserID:       claim.OwnerUserID,
			Input:        input,
			InputVersion: 1,
			Stage:        JobTargetStageAwaitingConfirmation,
			Analysis: &JobTargetAnalysis{
				InputVersion:    1,
				AnalysisVersion: 1,
				Attempt:         1,
				Status:          JobTargetAnalysisSucceeded,
				Candidate:       &candidate,
			},
		}, nil
	}
	parser := &jobTargetParserStub{candidate: candidate}
	service := mustJobTargetService(t, repository, parser, catalog)

	target, replayed, err := service.Analyze(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"analyze-key-001",
		AnalyzeJobTargetRequest{ExpectedInputVersion: 1},
	)
	if err != nil || replayed {
		t.Fatalf("Analyze replayed=%t error=%v", replayed, err)
	}
	if target.Stage != JobTargetStageAwaitingConfirmation ||
		target.Confirmation != nil ||
		target.Analysis == nil ||
		target.Analysis.Candidate == nil {
		t.Fatalf("parsed target was not kept unconfirmed: %#v", target)
	}
	if !reflect.DeepEqual(parser.input, input) {
		t.Fatalf("parser input = %#v, want %#v", parser.input, input)
	}
}

func TestJobTargetServiceRejectsUnknownCatalogOutputAndFailsClaim(t *testing.T) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	input := JobTargetInput{
		Source:         JobTargetSourceJobDescription,
		JobDescription: "Build reliable APIs.",
	}
	candidate := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)
	candidate.CatalogRecommendation.SelectedRoleIDs = []string{
		"role_model_invented",
	}
	repository := &jobTargetRepositoryStub{}
	repository.claim = claimedJobTargetAnalysis(input)
	failedCategory := ""
	repository.fail = func(
		_ context.Context,
		_ JobTargetAnalysisClaim,
		category string,
	) (JobTarget, error) {
		failedCategory = category
		return JobTarget{Stage: JobTargetStageAnalysisFailed}, nil
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{candidate: candidate},
		catalog,
	)

	_, _, err := service.Analyze(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"analyze-key-002",
		AnalyzeJobTargetRequest{ExpectedInputVersion: 1},
	)
	if !errors.Is(err, ErrJobTargetAnalysisFailed) {
		t.Fatalf("Analyze error = %v, want analysis failed", err)
	}
	if failedCategory != "invalid_result" {
		t.Fatalf("failed category = %q, want invalid_result", failedCategory)
	}
	if repository.complete != nil && repository.completeCalled {
		t.Fatal("invalid model output reached completion")
	}
}

func TestJobTargetServiceRejectsMultipleInterviewRolesFromParser(t *testing.T) {
	t.Parallel()

	input := JobTargetInput{
		Source:         JobTargetSourceJobDescription,
		JobDescription: "Build reliable APIs.",
	}
	candidate := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)
	candidate.CatalogRecommendation.SelectedRoleIDs = []string{
		TechnicalInterviewerRoleID,
		HRInterviewerRoleID,
	}
	candidate.CatalogRecommendation.PracticeOptionID =
		FullSimulationOptionID

	repository := &jobTargetRepositoryStub{}
	repository.claim = claimedJobTargetAnalysis(input)
	failedCategory := ""
	repository.fail = func(
		_ context.Context,
		_ JobTargetAnalysisClaim,
		category string,
	) (JobTarget, error) {
		failedCategory = category
		return JobTarget{Stage: JobTargetStageAnalysisFailed}, nil
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{candidate: candidate},
		mustBuiltinCatalog(t),
	)

	_, _, err := service.Analyze(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"analyze-key-multiple-roles",
		AnalyzeJobTargetRequest{ExpectedInputVersion: 1},
	)
	if !errors.Is(err, ErrJobTargetAnalysisFailed) {
		t.Fatalf("Analyze error = %v, want analysis failed", err)
	}
	if failedCategory != "invalid_result" {
		t.Fatalf("failed category = %q, want invalid_result", failedCategory)
	}
	if repository.completeCalled {
		t.Fatal("multi-role interview output reached completion")
	}
}

func TestJobTargetServicePropagatesLateWorkerFence(t *testing.T) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	input := JobTargetInput{
		Source:         JobTargetSourceJobDescription,
		JobDescription: "Design backend services.",
	}
	repository := &jobTargetRepositoryStub{
		claim: claimedJobTargetAnalysis(input),
		complete: func(
			context.Context,
			JobTargetAnalysisClaim,
			JobTargetCandidate,
		) (JobTarget, error) {
			return JobTarget{}, ErrJobTargetAnalysisClaimLost
		},
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{
			candidate: validJobTargetCandidateFixture(
				JobTargetSourceJobDescription,
			),
		},
		catalog,
	)

	_, _, err := service.Analyze(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"analyze-key-003",
		AnalyzeJobTargetRequest{ExpectedInputVersion: 1},
	)
	if !errors.Is(err, ErrJobTargetAnalysisClaimLost) {
		t.Fatalf("Analyze error = %v, want claim lost", err)
	}
}

func TestJobTargetServicePersistsAfterProviderCancelsRequest(
	t *testing.T,
) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	input := JobTargetInput{
		Source:         JobTargetSourceJobDescription,
		JobDescription: "Design backend services.",
	}
	completions := 0
	repository := &jobTargetRepositoryStub{
		claim: claimedJobTargetAnalysis(input),
		complete: func(
			ctx context.Context,
			claim JobTargetAnalysisClaim,
			candidate JobTargetCandidate,
		) (JobTarget, error) {
			if ctx.Err() != nil {
				t.Fatalf("persistence context inherited cancellation: %v", ctx.Err())
			}
			completions++
			return JobTarget{
				ID:           claim.TargetID,
				UserID:       claim.OwnerUserID,
				Input:        claim.Input,
				InputVersion: claim.InputVersion,
				Stage:        JobTargetStageAwaitingConfirmation,
			}, nil
		},
	}
	requestContext, cancel := context.WithCancel(context.Background())
	parser := &jobTargetParserStub{
		candidate: validJobTargetCandidateFixture(
			JobTargetSourceJobDescription,
		),
		after: cancel,
	}
	service := mustJobTargetService(t, repository, parser, catalog)

	target, _, err := service.Analyze(
		requestContext,
		jobTargetActor(),
		"target-1",
		"analyze-key-cancelled",
		AnalyzeJobTargetRequest{ExpectedInputVersion: 1},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if completions != 1 ||
		target.Stage != JobTargetStageAwaitingConfirmation {
		t.Fatalf(
			"completions=%d target=%#v",
			completions,
			target,
		)
	}
}

func TestJobTargetQuickStartCannotMasqueradeAsJDAnalysis(t *testing.T) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	candidate := validJobTargetCandidateFixture(JobTargetSourceQuickStart)
	candidate.GeneralAdviceOnly = false
	if err := validateJobTargetCandidate(
		candidate,
		JobTargetSourceQuickStart,
		catalog,
	); !errors.Is(err, ErrJobTargetInvalid) {
		t.Fatalf("candidate error = %v, want invalid", err)
	}

	if validJobTargetInput(JobTargetInput{
		Source:         JobTargetSourceQuickStart,
		JobTitle:       "Backend engineer",
		JobDescription: "This must not be accepted as quick start.",
	}) {
		t.Fatal("quick-start input accepted a hidden job description")
	}
}

func TestJobTargetConfirmRequiresExplicitVersionsAndValidCandidate(
	t *testing.T,
) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	repository := &jobTargetRepositoryStub{}
	called := false
	repository.confirm = func(
		_ context.Context,
		_ requestcontext.Actor,
		command ConfirmJobTargetCommand,
	) (JobTarget, bool, error) {
		called = true
		if command.Request.ExpectedInputVersion != 3 ||
			command.Request.ExpectedAnalysisVersion != 2 {
			t.Fatalf("unexpected confirmation command: %#v", command)
		}
		return JobTarget{Stage: JobTargetStageConfirmed}, false, nil
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{},
		catalog,
	)
	candidate := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)

	target, replayed, err := service.Confirm(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"confirm-key-001",
		ConfirmJobTargetRequest{
			ExpectedInputVersion:    3,
			ExpectedAnalysisVersion: 2,
			Candidate:               candidate,
		},
	)
	if err != nil || replayed || !called ||
		target.Stage != JobTargetStageConfirmed {
		t.Fatalf(
			"Confirm target=%#v replayed=%t called=%t error=%v",
			target,
			replayed,
			called,
			err,
		)
	}
}

func TestJobTargetConfirmRejectsMultipleInterviewRoles(t *testing.T) {
	t.Parallel()

	repository := &jobTargetRepositoryStub{
		confirm: func(
			context.Context,
			requestcontext.Actor,
			ConfirmJobTargetCommand,
		) (JobTarget, bool, error) {
			t.Fatal("multi-role interview confirmation reached repository")
			return JobTarget{}, false, nil
		},
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{},
		mustBuiltinCatalog(t),
	)
	candidate := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)
	candidate.CatalogRecommendation.SelectedRoleIDs = []string{
		TechnicalInterviewerRoleID,
		HRInterviewerRoleID,
	}
	candidate.CatalogRecommendation.PracticeOptionID =
		FullSimulationOptionID

	_, _, err := service.Confirm(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"confirm-key-multiple-roles",
		ConfirmJobTargetRequest{
			ExpectedInputVersion:    1,
			ExpectedAnalysisVersion: 1,
			Candidate:               candidate,
		},
	)
	if !errors.Is(err, ErrJobTargetInvalid) {
		t.Fatalf("Confirm error = %v, want invalid", err)
	}
}

func TestJobTargetConfirmationCandidateUnicodeAndTotalJSONLimits(
	t *testing.T,
) {
	t.Parallel()

	catalog := mustBuiltinCatalog(t)
	unicodeAtLimit := strings.Repeat(
		"界",
		maxJobTargetCandidateItemCharacters,
	)
	valid := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)
	valid.Responsibilities = []string{unicodeAtLimit}
	valid.CoreSkills = []string{unicodeAtLimit}
	valid.CommunicationFocus = []string{unicodeAtLimit}
	valid.PracticeGoals = []string{unicodeAtLimit}
	if err := validateJobTargetCandidate(
		valid,
		JobTargetSourceJobDescription,
		catalog,
	); err != nil {
		t.Fatalf("2048-character Unicode candidate: %v", err)
	}

	tooManyCharacters := valid
	tooManyCharacters.Responsibilities = []string{
		unicodeAtLimit + "界",
	}
	if err := validateJobTargetCandidate(
		tooManyCharacters,
		JobTargetSourceJobDescription,
		catalog,
	); !errors.Is(err, ErrJobTargetInvalid) {
		t.Fatalf("2049-character item error = %v", err)
	}

	oversized := validJobTargetCandidateFixture(
		JobTargetSourceJobDescription,
	)
	items := make([]string, maxJobTargetCandidateItems)
	for index := range items {
		items[index] = fmt.Sprintf(
			"%02d-%s",
			index,
			strings.Repeat("界", 500),
		)
	}
	oversized.Responsibilities = append([]string(nil), items...)
	oversized.CoreSkills = append([]string(nil), items...)
	oversized.CommunicationFocus = append([]string(nil), items...)
	oversized.PracticeGoals = append([]string(nil), items...)
	encoded, err := json.Marshal(oversized)
	if err != nil {
		t.Fatalf("marshal oversized candidate: %v", err)
	}
	if len(encoded) <= maxJobTargetCandidateJSONBytes {
		t.Fatalf(
			"oversized fixture bytes=%d, want >%d",
			len(encoded),
			maxJobTargetCandidateJSONBytes,
		)
	}

	repository := &jobTargetRepositoryStub{
		confirm: func(
			context.Context,
			requestcontext.Actor,
			ConfirmJobTargetCommand,
		) (JobTarget, bool, error) {
			t.Fatal("oversized confirmation reached repository")
			return JobTarget{}, false, nil
		},
	}
	service := mustJobTargetService(
		t,
		repository,
		&jobTargetParserStub{},
		catalog,
	)
	if _, _, err := service.Confirm(
		context.Background(),
		jobTargetActor(),
		"target-1",
		"confirm-size-boundary",
		ConfirmJobTargetRequest{
			ExpectedInputVersion:    1,
			ExpectedAnalysisVersion: 1,
			Candidate:               oversized,
		},
	); !errors.Is(err, ErrJobTargetInvalid) {
		t.Fatalf("oversized confirmation error = %v", err)
	}
}

func validJobTargetCandidateFixture(
	source JobTargetSource,
) JobTargetCandidate {
	return JobTargetCandidate{
		Source:             source,
		GeneralAdviceOnly:  source == JobTargetSourceQuickStart,
		JobTitle:           "Platform engineer",
		Seniority:          "Senior",
		Responsibilities:   []string{"Design reliable backend services."},
		CoreSkills:         []string{"Distributed systems"},
		CommunicationFocus: []string{"Explain engineering trade-offs."},
		PracticeGoals:      []string{"Practice a concise project deep dive."},
		ScopeNotice:        "Suggestions are limited to the current technical interview content pack.",
		CatalogRecommendation: JobTargetCatalogRecommendation{
			ScenarioDefinitionID:      ProgrammerInterviewScenarioID,
			ScenarioDefinitionVersion: 1,
			SelectedRoleIDs:           []string{TechnicalInterviewerRoleID},
			PracticeOptionID:          TechnicalFocusOptionID,
			PracticeOptionVersion:     1,
		},
	}
}

func claimedJobTargetAnalysis(
	input JobTargetInput,
) func(
	context.Context,
	requestcontext.Actor,
	AnalyzeJobTargetCommand,
) (JobTarget, JobTargetAnalysisClaim, bool, bool, error) {
	return func(
		_ context.Context,
		actor requestcontext.Actor,
		command AnalyzeJobTargetCommand,
	) (JobTarget, JobTargetAnalysisClaim, bool, bool, error) {
		return JobTarget{
				ID:           command.TargetID,
				UserID:       actor.UserID,
				Input:        input,
				InputVersion: command.Request.ExpectedInputVersion,
				Stage:        JobTargetStageParsing,
			},
			JobTargetAnalysisClaim{
				AttemptID:       "attempt-1",
				TargetID:        command.TargetID,
				OwnerUserID:     actor.UserID,
				InputVersion:    command.Request.ExpectedInputVersion,
				AnalysisVersion: 1,
				WorkerToken:     "worker-1",
				LeaseUntil:      time.Now().Add(time.Minute),
				Input:           input,
				Intent:          command.Intent,
			},
			true,
			false,
			nil
	}
}

func mustJobTargetService(
	t *testing.T,
	repository JobTargetRepository,
	parser JobTargetParser,
	catalog CatalogReader,
) *JobTargetService {
	t.Helper()
	service, err := NewJobTargetService(
		repository,
		jobTargetIDGenerator{},
		parser,
		catalog,
	)
	if err != nil {
		t.Fatalf("NewJobTargetService: %v", err)
	}
	return service
}

func jobTargetActor() requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    "user-1",
		SessionID: "session-1",
	}
}

type jobTargetIDGenerator struct{}

func (jobTargetIDGenerator) NewID() (string, error) {
	return "target-generated", nil
}

type jobTargetParserStub struct {
	candidate JobTargetCandidate
	err       error
	input     JobTargetInput
	after     func()
}

func (p *jobTargetParserStub) ParseJobTarget(
	_ context.Context,
	input JobTargetInput,
) (JobTargetCandidate, error) {
	p.input = input
	if p.after != nil {
		p.after()
	}
	return cloneJobTargetCandidate(p.candidate), p.err
}

type jobTargetRepositoryStub struct {
	create   func(context.Context, requestcontext.Actor, CreateJobTargetCommand) (JobTarget, bool, error)
	get      func(context.Context, requestcontext.Actor, string) (JobTarget, error)
	update   func(context.Context, requestcontext.Actor, UpdateJobTargetCommand) (JobTarget, bool, error)
	claim    func(context.Context, requestcontext.Actor, AnalyzeJobTargetCommand) (JobTarget, JobTargetAnalysisClaim, bool, bool, error)
	complete func(context.Context, JobTargetAnalysisClaim, JobTargetCandidate) (JobTarget, error)
	fail     func(context.Context, JobTargetAnalysisClaim, string) (JobTarget, error)
	confirm  func(context.Context, requestcontext.Actor, ConfirmJobTargetCommand) (JobTarget, bool, error)
	discard  func(context.Context, requestcontext.Actor, DiscardJobTargetCommand) (JobTarget, bool, error)

	completeCalled bool
}

func (r *jobTargetRepositoryStub) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateJobTargetCommand,
) (JobTarget, bool, error) {
	if r.create == nil {
		panic("unexpected Create")
	}
	return r.create(ctx, actor, command)
}

func (r *jobTargetRepositoryStub) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	targetID string,
) (JobTarget, error) {
	if r.get == nil {
		panic("unexpected Get")
	}
	return r.get(ctx, actor, targetID)
}

func (r *jobTargetRepositoryStub) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateJobTargetCommand,
) (JobTarget, bool, error) {
	if r.update == nil {
		panic("unexpected Update")
	}
	return r.update(ctx, actor, command)
}

func (r *jobTargetRepositoryStub) ClaimAnalysis(
	ctx context.Context,
	actor requestcontext.Actor,
	command AnalyzeJobTargetCommand,
) (JobTarget, JobTargetAnalysisClaim, bool, bool, error) {
	if r.claim == nil {
		panic("unexpected ClaimAnalysis")
	}
	return r.claim(ctx, actor, command)
}

func (r *jobTargetRepositoryStub) CompleteAnalysis(
	ctx context.Context,
	claim JobTargetAnalysisClaim,
	candidate JobTargetCandidate,
) (JobTarget, error) {
	r.completeCalled = true
	if r.complete == nil {
		panic("unexpected CompleteAnalysis")
	}
	return r.complete(ctx, claim, candidate)
}

func (r *jobTargetRepositoryStub) FailAnalysis(
	ctx context.Context,
	claim JobTargetAnalysisClaim,
	category string,
) (JobTarget, error) {
	if r.fail == nil {
		panic("unexpected FailAnalysis")
	}
	return r.fail(ctx, claim, category)
}

func (r *jobTargetRepositoryStub) Confirm(
	ctx context.Context,
	actor requestcontext.Actor,
	command ConfirmJobTargetCommand,
) (JobTarget, bool, error) {
	if r.confirm == nil {
		panic("unexpected Confirm")
	}
	return r.confirm(ctx, actor, command)
}

func (r *jobTargetRepositoryStub) Discard(
	ctx context.Context,
	actor requestcontext.Actor,
	command DiscardJobTargetCommand,
) (JobTarget, bool, error) {
	if r.discard == nil {
		panic("unexpected Discard")
	}
	return r.discard(ctx, actor, command)
}

var _ JobTargetRepository = (*jobTargetRepositoryStub)(nil)
var _ JobTargetParser = (*jobTargetParserStub)(nil)
