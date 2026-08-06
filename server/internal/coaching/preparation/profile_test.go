package preparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	preparationmodel "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	preparationport "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestPersistenceServiceResolvesTypedScenarioContext(t *testing.T) {
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	request := CreateProfileRequest{
		Kind: preparationmodel.PreparationKindScenario,
		Scenario: &preparationmodel.ScenarioContextInput{
			Situation:          "Return a damaged product",
			UserRole:           "Customer",
			CounterpartRole:    "Store manager",
			Goal:               "Agree on a replacement",
			CounterpartPersona: "Professional and cautious",
		},
	}
	resolver := &profileContextResolverStub{}
	repository := &profileRepositoryReplayStub{}
	service, err := NewPersistenceServiceWithContext(
		repository,
		profileFixedIDGenerator("profile-1"),
		&profileResumeReaderStub{},
		resolver,
	)
	if err != nil {
		t.Fatalf("NewPersistenceServiceWithContext: %v", err)
	}

	_, replayed, err := service.CreateProfile(
		context.Background(),
		actor,
		"scenario-create-key",
		request,
	)
	if err != nil || replayed {
		t.Fatalf("CreateProfile = (%t, %v)", replayed, err)
	}
	if resolver.command.Input.Scenario != request.Scenario ||
		repository.createdProfile.Context == nil ||
		repository.createdProfile.Context.Scenario.Situation !=
			request.Scenario.Situation {
		t.Fatalf(
			"resolver command = %#v, create command = %#v",
			resolver.command,
			repository.createdProfile,
		)
	}
}

func TestPersistenceServiceReplaysBeforeAllocatingResourceID(t *testing.T) {
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	profile := Profile{
		ID:                "profile-1",
		UserID:            actor.UserID,
		BackgroundSummary: "Backend engineer.",
		Version:           1,
	}
	snapshot := Snapshot{
		ID:              "snapshot-1",
		SourceProfileID: profile.ID,
		SourceVersion:   1,
	}
	repository := &profileRepositoryReplayStub{
		profile:       profile,
		profileFound:  true,
		snapshot:      snapshot,
		snapshotFound: true,
	}
	ids := &failingProfileIDGenerator{}
	service, err := NewPersistenceService(
		repository,
		ids,
		&profileResumeReaderStub{},
	)
	if err != nil {
		t.Fatalf("NewPersistenceService: %v", err)
	}

	replayedProfile, replayed, err := service.CreateProfile(
		context.Background(),
		actor,
		"profile-replay-key",
		CreateProfileRequest{BackgroundSummary: profile.BackgroundSummary},
	)
	if err != nil || !replayed || replayedProfile != profile {
		t.Fatalf(
			"CreateProfile replay = (%+v, %v, %v)",
			replayedProfile,
			replayed,
			err,
		)
	}
	replayedSnapshot, replayed, err := service.CreateSnapshot(
		context.Background(),
		actor,
		profile.ID,
		"snapshot-replay-key",
		CreateSnapshotRequest{SourceVersion: 1},
	)
	if err != nil || !replayed || replayedSnapshot != snapshot {
		t.Fatalf(
			"CreateSnapshot replay = (%+v, %v, %v)",
			replayedSnapshot,
			replayed,
			err,
		)
	}
	if ids.calls != 0 {
		t.Fatalf("resource ID generator calls = %d, want 0", ids.calls)
	}
}

func TestPersistenceServiceResolvesExactResumeRevisionAfterReplayCheck(
	t *testing.T,
) {
	actor := requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
	request := CreateProfileRequest{
		ResumeID:          "50000000-0000-4000-8000-000000000001",
		ResumeRevision:    2,
		BackgroundSummary: "Backend engineer.",
	}
	resolved := ResumeRevisionSnapshot{
		ResumeID: request.ResumeID,
		Revision: request.ResumeRevision,
		Material: ResumeMaterial{
			WorkExperiences:      []ResumeWorkExperience{},
			ProjectExperiences:   []ResumeProjectExperience{},
			EducationExperiences: []ResumeEducationExperience{},
			Skills:               []string{"Go"},
			Awards:               []string{},
		},
	}
	repository := &profileRepositoryReplayStub{}
	reader := &profileResumeReaderStub{read: func(
		_ context.Context,
		gotActor requestcontext.Actor,
		resumeID string,
		revision int64,
	) (ResumeRevisionSnapshot, error) {
		if gotActor != actor || resumeID != request.ResumeID ||
			revision != request.ResumeRevision {
			t.Fatalf(
				"Resume read = (%+v, %q, %d)",
				gotActor,
				resumeID,
				revision,
			)
		}
		return resolved, nil
	}}
	service, err := NewPersistenceService(
		repository,
		profileFixedIDGenerator("profile-1"),
		reader,
	)
	if err != nil {
		t.Fatalf("NewPersistenceService: %v", err)
	}

	profile, replayed, err := service.CreateProfile(
		context.Background(),
		actor,
		"profile-create-key",
		request,
	)
	if err != nil || replayed || profile.ResumeID != request.ResumeID ||
		profile.ResumeRevision != request.ResumeRevision {
		t.Fatalf("CreateProfile = (%+v, %t, %v)", profile, replayed, err)
	}
	if repository.createdProfile.ResumeRevision == nil ||
		!validResumeRevisionSnapshot(*repository.createdProfile.ResumeRevision) {
		t.Fatalf("created command = %+v", repository.createdProfile)
	}
	resolved.Material.Skills[0] = "mutated"
	if repository.createdProfile.ResumeRevision.Material.Skills[0] != "Go" {
		t.Fatal("repository command retained mutable Reader material")
	}
}

func TestCreateProfileRequestTextBoundaries(t *testing.T) {
	referenceAtLimit := strings.Repeat("界", maxPreparationReferenceLength)
	summaryAtLimit := strings.Repeat("语", maxPreparationSummaryLength)
	if !validCreateProfileRequest(CreateProfileRequest{
		ResumeID:          "50000000-0000-4000-8000-000000000001",
		ResumeRevision:    1,
		JobDescriptionRef: referenceAtLimit,
		BackgroundSummary: summaryAtLimit,
	}) {
		t.Fatal("request at documented limits was rejected")
	}
	for _, request := range []CreateProfileRequest{
		{
			ResumeID:          "50000000-0000-4000-8000-000000000001",
			BackgroundSummary: "valid",
		},
		{
			ResumeRevision:    1,
			BackgroundSummary: "valid",
		},
		{
			JobDescriptionRef: referenceAtLimit + "j",
			BackgroundSummary: "valid",
		},
		{BackgroundSummary: summaryAtLimit + "s"},
		{BackgroundSummary: " leading"},
		{BackgroundSummary: "trailing "},
		{BackgroundSummary: "contains\x00nul"},
	} {
		if validCreateProfileRequest(request) {
			t.Fatalf("invalid boundary request was accepted: %#v", request)
		}
	}
}

type profileResumeReaderStub struct {
	read func(
		context.Context,
		requestcontext.Actor,
		string,
		int64,
	) (ResumeRevisionSnapshot, error)
}

type profileContextResolverStub struct {
	command preparationport.ResolveCommand
}

func (stub *profileContextResolverStub) Resolve(
	_ context.Context,
	command preparationport.ResolveCommand,
) (preparationmodel.ResolvedContext, error) {
	stub.command = command
	if command.Input.Scenario == nil {
		return preparationmodel.ResolvedContext{}, preparationmodel.ErrInvalidContext
	}
	input := command.Input.Scenario
	return preparationmodel.ResolvedContext{
		Kind: preparationmodel.PreparationKindScenario,
		Scenario: &preparationmodel.ScenarioContextSnapshot{
			Situation:          input.Situation,
			UserRole:           input.UserRole,
			CounterpartRole:    input.CounterpartRole,
			Goal:               input.Goal,
			CounterpartPersona: input.CounterpartPersona,
		},
	}, nil
}

func (stub *profileResumeReaderStub) ReadOwnedRevision(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
	revision int64,
) (ResumeRevisionSnapshot, error) {
	if stub.read == nil {
		return ResumeRevisionSnapshot{}, errors.New(
			"unexpected ReadOwnedRevision",
		)
	}
	return stub.read(ctx, actor, resumeID, revision)
}

type profileRepositoryReplayStub struct {
	profile        Profile
	profileFound   bool
	snapshot       Snapshot
	snapshotFound  bool
	createdProfile CreateProfileCommand
}

func (stub *profileRepositoryReplayStub) ReplayProfile(
	context.Context,
	requestcontext.Actor,
	IdempotencyIntent,
) (Profile, bool, error) {
	return stub.profile, stub.profileFound, nil
}

func (stub *profileRepositoryReplayStub) CreateProfile(
	_ context.Context,
	actor requestcontext.Actor,
	command CreateProfileCommand,
) (Profile, bool, error) {
	stub.createdProfile = command
	return Profile{
		ID: command.ProfileID, UserID: actor.UserID,
		ResumeID:          command.Request.ResumeID,
		ResumeRevision:    command.Request.ResumeRevision,
		BackgroundSummary: command.Request.BackgroundSummary,
		Version:           1,
	}, false, nil
}

func (stub *profileRepositoryReplayStub) ReplaySnapshot(
	context.Context,
	requestcontext.Actor,
	IdempotencyIntent,
) (Snapshot, bool, error) {
	return stub.snapshot, stub.snapshotFound, nil
}

func (*profileRepositoryReplayStub) CreateSnapshot(
	context.Context,
	requestcontext.Actor,
	CreateSnapshotCommand,
) (Snapshot, bool, error) {
	return Snapshot{}, false, errors.New("unexpected CreateSnapshot")
}

func (*profileRepositoryReplayStub) ReadProfile(
	context.Context,
	requestcontext.Actor,
	string,
) (Profile, error) {
	return Profile{}, errors.New("unexpected ReadProfile")
}

func (*profileRepositoryReplayStub) ReadSnapshot(
	context.Context,
	requestcontext.Actor,
	string,
) (Snapshot, error) {
	return Snapshot{}, errors.New("unexpected ReadSnapshot")
}

func (*profileRepositoryReplayStub) DeleteProfileData(
	context.Context,
	DeleteProfileDataCommand,
) error {
	return errors.New("unexpected DeleteProfileData")
}

type failingProfileIDGenerator struct {
	calls int
}

type profileFixedIDGenerator string

func (generator profileFixedIDGenerator) NewID() (string, error) {
	return string(generator), nil
}

func (generator *failingProfileIDGenerator) NewID() (string, error) {
	generator.calls++
	return "", errors.New("ID source unavailable")
}
