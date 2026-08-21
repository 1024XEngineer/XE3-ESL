package profile

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const testUserID = "10000000-0000-4000-8000-000000000001"

var (
	testActor = requestcontext.Actor{
		UserID: testUserID, SessionID: "session-1",
	}
	testNow = time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
)

func TestDataLimitsCountUnicodeRunes(t *testing.T) {
	data := Data{FormOfAddress: strings.Repeat("你", MaxFormOfAddressRunes)}
	if !data.Valid() {
		t.Fatal("form of address at the rune limit must be valid")
	}
	data.FormOfAddress += "好"
	if data.Valid() {
		t.Fatal("form of address above the rune limit must be rejected")
	}

	data = Data{Interests: []string{"语言学习", "Language Learning"}}
	if !data.Valid() {
		t.Fatal("distinct Unicode interests must be valid")
	}
	data.Interests = []string{"English", "english"}
	if data.Valid() {
		t.Fatal("case-insensitive duplicate interests must be rejected")
	}
}

func TestServiceGetReturnsLogicalEnabledEmptyProfileWhenMissing(t *testing.T) {
	service, err := NewService(&profileRepositoryStub{findErr: ErrNotFound}, func() time.Time {
		return testNow
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(context.Background(), testActor)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != testUserID || !got.MemoryEnabled || got.Version != 0 ||
		!got.Data.Empty() || len(got.FieldSources) != 0 {
		t.Fatalf("logical default = %#v", got)
	}
}

func TestServiceUpdatesSettingsWhileMemoryDisabled(t *testing.T) {
	repository := &profileRepositoryStub{item: storedProfile(false)}
	service, err := NewService(repository, func() time.Time { return testNow })
	if err != nil {
		t.Fatal(err)
	}
	occupation := "产品设计师"
	detail := ResponseDetailed
	updated, err := service.Update(context.Background(), testActor, UpdateCommand{
		ExpectedVersion: 1,
		Patch: DataPatch{
			Occupation:     &occupation,
			ResponseDetail: &detail,
		},
		SourceType: SourceUserSetting,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.MemoryEnabled || updated.Version != 2 ||
		updated.Data.Occupation != occupation ||
		updated.Data.ResponseDetail != detail {
		t.Fatalf("updated disabled profile = %#v", updated)
	}
	for _, field := range []Field{FieldOccupation, FieldResponseDetail} {
		source := updated.FieldSources[field]
		if source.Type != SourceUserSetting || source.MessageID != "" ||
			!source.RecordedAt.Equal(testNow) {
			t.Fatalf("source %s = %#v", field, source)
		}
	}
}

func TestServiceFirstInsertThenForgetAndClear(t *testing.T) {
	repository := &profileRepositoryStub{findErr: ErrNotFound}
	service, err := NewService(repository, func() time.Time { return testNow })
	if err != nil {
		t.Fatal(err)
	}
	occupation := "工程师"
	interests := []string{"语言学习", "跑步"}
	created, err := service.Update(context.Background(), testActor, UpdateCommand{
		ExpectedVersion: 0,
		Patch: DataPatch{
			Occupation: &occupation,
			Interests:  &interests,
		},
		SourceType:      SourceExplicitCurrentFact,
		SourceMessageID: "30000000-0000-4000-8000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Data.Occupation != occupation ||
		!slices.Equal(created.Data.Interests, interests) ||
		created.FieldSources[FieldOccupation].MessageID == "" {
		t.Fatalf("first inserted profile = %#v", created)
	}
	repository.findErr = nil

	forgotten, err := service.Update(context.Background(), testActor, UpdateCommand{
		ExpectedVersion: 1,
		ForgetFields:    []Field{FieldOccupation},
		SourceType:      SourceUserSetting,
	})
	if err != nil {
		t.Fatal(err)
	}
	if forgotten.Version != 2 || forgotten.Data.Occupation != "" ||
		forgotten.Data.Has(FieldOccupation) ||
		forgotten.FieldSources[FieldOccupation].Type != "" ||
		!slices.Equal(forgotten.Data.Interests, interests) {
		t.Fatalf("forgotten profile = %#v", forgotten)
	}

	cleared, err := service.Update(context.Background(), testActor, UpdateCommand{
		ExpectedVersion: 2,
		ClearProfile:    true,
		SourceType:      SourceUserSetting,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Version != 3 || !cleared.Data.Empty() ||
		len(cleared.FieldSources) != 0 || !cleared.MemoryEnabled {
		t.Fatalf("cleared profile = %#v", cleared)
	}
}

func TestServiceRequiresExpectedVersion(t *testing.T) {
	service, err := NewService(
		&profileRepositoryStub{item: storedProfile(true)},
		func() time.Time { return testNow },
	)
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	_, err = service.Update(context.Background(), testActor, UpdateCommand{
		ExpectedVersion: 0,
		MemoryEnabled:   &enabled,
		SourceType:      SourceUserSetting,
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("version error = %v", err)
	}
}

func storedProfile(enabled bool) Profile {
	return Profile{
		UserID: testUserID, MemoryEnabled: enabled,
		Data: Data{}, FieldSources: map[Field]FieldSource{}, Version: 1,
		CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour),
	}
}

type profileRepositoryStub struct {
	item    Profile
	findErr error
}

func (repository *profileRepositoryStub) Find(
	context.Context,
	string,
) (Profile, error) {
	if repository.findErr != nil {
		return Profile{}, repository.findErr
	}
	return cloneProfile(repository.item), nil
}

func (repository *profileRepositoryStub) Save(
	_ context.Context,
	item Profile,
	expectedVersion int64,
) (Profile, error) {
	if repository.item.Version != expectedVersion && expectedVersion != 0 {
		return Profile{}, ErrVersionConflict
	}
	if expectedVersion == 0 && repository.item.Version != 0 {
		return Profile{}, ErrVersionConflict
	}
	if expectedVersion == 0 {
		item.CreatedAt = testNow
	} else {
		item.CreatedAt = repository.item.CreatedAt
	}
	item.UpdatedAt = testNow
	repository.item = cloneProfile(item)
	return cloneProfile(item), nil
}
