package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"golang.org/x/text/unicode/norm"
)

func TestNormalizeDisplayNameAppliesNFCAndPreservesEmoji(t *testing.T) {
	got, err := NormalizeDisplayName("  Cafe\u0301 👩🏽‍💻  ")
	if err != nil {
		t.Fatalf("NormalizeDisplayName() error = %v", err)
	}
	if got != "Café 👩🏽‍💻" || !norm.NFC.IsNormalString(got) {
		t.Fatalf("NormalizeDisplayName() = %q", got)
	}
}

func TestNormalizeDisplayNameRejectsUnsafeOrOversizedValues(t *testing.T) {
	for _, value := range []string{
		"",
		" \t ",
		"line\nbreak",
		"hidden\u200bname",
		"override\u202ename",
		"paragraph\u2029break",
		"12345678901234567890123456789012345678901",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := NormalizeDisplayName(value); !errors.Is(
				err,
				ErrInvalidRequest,
			) {
				t.Fatalf("NormalizeDisplayName(%q) error = %v", value, err)
			}
		})
	}
}

func TestProfileServiceUsesTrustedActorAndNormalizedPayload(t *testing.T) {
	repository := completeRepositoryStub()
	var persisted PersistProfileCommand
	repository.persistProfile = func(
		_ context.Context,
		command PersistProfileCommand,
	) (UserProfile, error) {
		persisted = command
		return UserProfile{
			UserID:         command.UserID,
			DisplayName:    command.DisplayName,
			ProfileVersion: 1,
		}, nil
	}
	service := mustService(t, repository, defaultHasherStub(), time.Time{})
	actor := requestcontext.Actor{
		UserID:    "10000000-0000-4000-8000-000000000001",
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	profile, err := service.UpdateProfile(
		context.Background(),
		actor,
		UpdateProfileCommand{
			DisplayName:    " Cafe\u0301 ",
			IdempotencyKey: "profile-request-0001",
		},
	)
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if profile.UserID != actor.UserID ||
		persisted.UserID != actor.UserID ||
		persisted.DisplayName != "Café" ||
		len(persisted.RequestDigest) != 32 {
		t.Fatalf("persisted/profile = %#v / %#v", persisted, profile)
	}
}

func TestProfileServiceMapsMissingProfileWithoutEmailFallback(t *testing.T) {
	repository := completeRepositoryStub()
	repository.findProfile = func(
		context.Context,
		string,
	) (UserProfile, error) {
		return UserProfile{}, ErrNotFound
	}
	service := mustService(t, repository, defaultHasherStub(), time.Time{})
	_, err := service.CurrentProfile(
		context.Background(),
		requestcontext.Actor{
			UserID:    "10000000-0000-4000-8000-000000000001",
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
	)
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("CurrentProfile() error = %v", err)
	}
}
