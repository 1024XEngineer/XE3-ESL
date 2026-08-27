package presentation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const serviceUserID = "10000000-0000-4000-8000-000000000001"

var serviceActor = requestcontext.Actor{
	UserID: serviceUserID, SessionID: "session-1",
}

func TestGetPreferenceReturnsLogicalCatalogDefaults(t *testing.T) {
	repository := &repositoryStub{catalog: validCatalog(), findErr: ErrNotFound}
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	preference, err := service.GetPreference(context.Background(), serviceActor)
	if err != nil || preference.UserID != serviceUserID ||
		preference.AvatarOptionID != "avatar_lisa" ||
		preference.VoiceOptionID != "voice_ava" || preference.Version != 0 ||
		!preference.ValidLogical() {
		t.Fatalf("preference=%#v err=%v", preference, err)
	}
}

func TestUpdatePreferenceValidatesEnabledCatalogAndVersion(t *testing.T) {
	repository := &repositoryStub{
		catalog: validCatalog(),
		preference: Preference{
			UserID: serviceUserID, AvatarOptionID: "avatar_lisa",
			VoiceOptionID: "voice_ava", Version: 2,
			CreatedAt: time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC),
		},
	}
	service, _ := NewService(repository)

	_, err := service.UpdatePreference(
		context.Background(),
		serviceActor,
		UpdateCommand{
			AvatarOptionID:  "avatar_unknown",
			VoiceOptionID:   "voice_ava",
			ExpectedVersion: 2,
		},
	)
	if !errors.Is(err, ErrInvalidRequest) || repository.saveCalls != 0 {
		t.Fatalf("unknown option err=%v saves=%d", err, repository.saveCalls)
	}

	_, err = service.UpdatePreference(
		context.Background(),
		serviceActor,
		UpdateCommand{
			AvatarOptionID:  "avatar_nathan",
			VoiceOptionID:   "voice_john",
			ExpectedVersion: 1,
		},
	)
	if !errors.Is(err, ErrVersionConflict) || repository.saveCalls != 0 {
		t.Fatalf("stale version err=%v saves=%d", err, repository.saveCalls)
	}
}

func TestUpdatePreferenceCommitsBothSelections(t *testing.T) {
	repository := &repositoryStub{catalog: validCatalog(), findErr: ErrNotFound}
	service, _ := NewService(repository)
	preference, err := service.UpdatePreference(
		context.Background(),
		serviceActor,
		UpdateCommand{
			AvatarOptionID:  "avatar_nathan",
			VoiceOptionID:   "voice_john",
			ExpectedVersion: 0,
		},
	)
	if err != nil || repository.saveCalls != 1 ||
		preference.AvatarOptionID != "avatar_nathan" ||
		preference.VoiceOptionID != "voice_john" || preference.Version != 1 {
		t.Fatalf("preference=%#v saves=%d err=%v", preference, repository.saveCalls, err)
	}
}

func validCatalog() Catalog {
	return Catalog{
		Avatars: []AvatarOption{
			{
				ID: "avatar_lisa", DisplayName: "莉萨", Description: "亲切、开朗",
				PreviewAssetKey: "coach-avatar-lisa", Provider: "spatialreal",
				ProviderAvatarID: "avatar-provider-lisa", BindingVersion: 1,
				SortOrder: 10, Default: true,
			},
			{
				ID: "avatar_nathan", DisplayName: "内森", Description: "温暖、沉稳",
				PreviewAssetKey: "coach-avatar-nathan", Provider: "spatialreal",
				ProviderAvatarID: "avatar-provider-nathan", BindingVersion: 1,
				SortOrder: 20,
			},
		},
		Voices: []VoiceOption{
			{
				ID: "voice_ava", DisplayName: "艾娃",
				Description: "清晰自然 · 美式英语 · 女声", Locale: "en-US",
				Gender: "female", Provider: "qianwen",
				ProviderModel:   "qwen-audio-3.0-tts-flash",
				ProviderVoiceID: "loongeva_v3.6", BindingVersion: 1,
				SortOrder: 10, Default: true,
			},
			{
				ID: "voice_john", DisplayName: "约翰",
				Description: "温暖沉稳 · 美式英语 · 男声", Locale: "en-US",
				Gender: "male", Provider: "qianwen",
				ProviderModel:   "qwen-audio-3.0-tts-flash",
				ProviderVoiceID: "loongjohn", BindingVersion: 1, SortOrder: 20,
			},
		},
		DefaultAvatarOptionID: "avatar_lisa",
		DefaultVoiceOptionID:  "voice_ava",
	}
}

type repositoryStub struct {
	catalog    Catalog
	preference Preference
	findErr    error
	saveErr    error
	saveCalls  int
}

func (repository *repositoryStub) Catalog(context.Context) (Catalog, error) {
	return repository.catalog, nil
}

func (repository *repositoryStub) FindPreference(
	context.Context,
	string,
) (Preference, error) {
	return repository.preference, repository.findErr
}

func (repository *repositoryStub) SavePreference(
	_ context.Context,
	preference Preference,
	_ int64,
) (Preference, error) {
	repository.saveCalls++
	if repository.saveErr != nil {
		return Preference{}, repository.saveErr
	}
	preference.CreatedAt = time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	preference.UpdatedAt = preference.CreatedAt
	return preference, nil
}
