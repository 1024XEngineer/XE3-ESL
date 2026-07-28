package preparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

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
		profile:  profile,
		snapshot: snapshot,
	}
	ids := &failingProfileIDGenerator{}
	service, err := NewPersistenceService(repository, ids)
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

func TestCreateProfileRequestTextBoundaries(t *testing.T) {
	referenceAtLimit := strings.Repeat("界", maxPreparationReferenceLength)
	summaryAtLimit := strings.Repeat("语", maxPreparationSummaryLength)
	if !validCreateProfileRequest(CreateProfileRequest{
		ResumeRef:         referenceAtLimit,
		JobDescriptionRef: referenceAtLimit,
		BackgroundSummary: summaryAtLimit,
	}) {
		t.Fatal("request at documented limits was rejected")
	}
	for _, request := range []CreateProfileRequest{
		{
			ResumeRef:         referenceAtLimit + "r",
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

type profileRepositoryReplayStub struct {
	profile  Profile
	snapshot Snapshot
}

func (stub *profileRepositoryReplayStub) ReplayProfile(
	context.Context,
	requestcontext.Actor,
	IdempotencyIntent,
) (Profile, bool, error) {
	return stub.profile, true, nil
}

func (*profileRepositoryReplayStub) CreateProfile(
	context.Context,
	requestcontext.Actor,
	CreateProfileCommand,
) (Profile, bool, error) {
	return Profile{}, false, errors.New("unexpected CreateProfile")
}

func (stub *profileRepositoryReplayStub) ReplaySnapshot(
	context.Context,
	requestcontext.Actor,
	IdempotencyIntent,
) (Snapshot, bool, error) {
	return stub.snapshot, true, nil
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

func (generator *failingProfileIDGenerator) NewID() (string, error) {
	generator.calls++
	return "", errors.New("ID source unavailable")
}
