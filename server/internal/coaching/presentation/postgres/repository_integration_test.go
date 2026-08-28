package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	repositoryUserA = "10000000-0000-4000-8000-000000000001"
	repositoryUserB = "10000000-0000-4000-8000-000000000002"
)

func TestCatalogReturnsEnabledSeedsWithoutLosingBindings(t *testing.T) {
	pool := presentationTestDatabase(t)
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := repository.Catalog(context.Background())
	if err != nil || !catalog.Valid() || len(catalog.Avatars) != 2 ||
		len(catalog.Voices) != 9 ||
		catalog.DefaultAvatarOptionID != "avatar_lisa" ||
		catalog.DefaultVoiceOptionID != "voice_ava" ||
		catalog.Avatars[0].ProviderAvatarID == "" ||
		catalog.Voices[0].ProviderVoiceID != "loongeva_v3.6" ||
		catalog.Voices[2].ID != "voice_mary" ||
		catalog.Voices[2].Locale != "en-GB" ||
		catalog.Voices[2].ProviderVoiceID != "loongmary" ||
		catalog.Voices[8].ID != "voice_ivy" ||
		catalog.Voices[8].ProviderVoiceID !=
			"qwen-audio-3.0-tts-flash-loongivyhu" {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
}

func TestResolveSelectionUsesDefaultsThenSavedPreference(t *testing.T) {
	pool := presentationTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'presentation-resolve@example.test')`, repositoryUserA); err != nil {
		t.Fatal(err)
	}
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}

	defaults, err := repository.ResolveSelection(ctx, repositoryUserA)
	if err != nil || defaults.Avatar.ID != "avatar_lisa" ||
		defaults.Voice.ID != "voice_ava" || !defaults.Valid() {
		t.Fatalf("defaults=%#v err=%v", defaults, err)
	}
	if _, err := repository.SavePreference(ctx, presentation.Preference{
		UserID: repositoryUserA, AvatarOptionID: "avatar_nathan",
		VoiceOptionID: "voice_john", Version: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}

	saved, err := repository.ResolveSelection(ctx, repositoryUserA)
	if err != nil || saved.Avatar.ID != "avatar_nathan" ||
		saved.Voice.ID != "voice_john" || !saved.Valid() {
		t.Fatalf("saved=%#v err=%v", saved, err)
	}
}

func TestPreferencesAreUserIsolatedAndOptimisticallyLocked(t *testing.T) {
	pool := presentationTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO users (id, canonical_email) VALUES
    ($1, 'presentation-a@example.test'),
    ($2, 'presentation-b@example.test')`, repositoryUserA, repositoryUserB); err != nil {
		t.Fatal(err)
	}
	repository, _ := New(pool)
	savedA, err := repository.SavePreference(ctx, presentation.Preference{
		UserID: repositoryUserA, AvatarOptionID: "avatar_lisa",
		VoiceOptionID: "voice_ava", Version: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	savedB, err := repository.SavePreference(ctx, presentation.Preference{
		UserID: repositoryUserB, AvatarOptionID: "avatar_nathan",
		VoiceOptionID: "voice_john", Version: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if savedA.UserID == savedB.UserID ||
		savedA.AvatarOptionID != "avatar_lisa" ||
		savedB.AvatarOptionID != "avatar_nathan" {
		t.Fatalf("savedA=%#v savedB=%#v", savedA, savedB)
	}

	updatedA, err := repository.SavePreference(ctx, presentation.Preference{
		UserID: repositoryUserA, AvatarOptionID: "avatar_nathan",
		VoiceOptionID: "voice_john", Version: 2,
	}, 1)
	if err != nil || updatedA.Version != 2 {
		t.Fatalf("updated=%#v err=%v", updatedA, err)
	}
	_, err = repository.SavePreference(ctx, presentation.Preference{
		UserID: repositoryUserA, AvatarOptionID: "avatar_lisa",
		VoiceOptionID: "voice_ava", Version: 2,
	}, 1)
	if !errors.Is(err, presentation.ErrVersionConflict) {
		t.Fatalf("stale update err=%v", err)
	}
	persistedB, err := repository.FindPreference(ctx, repositoryUserB)
	if err != nil || persistedB.AvatarOptionID != "avatar_nathan" ||
		persistedB.VoiceOptionID != "voice_john" || persistedB.Version != 1 {
		t.Fatalf("user B changed: %#v err=%v", persistedB, err)
	}
}

func TestConcurrentFirstPreferenceInsertHasOneWinner(t *testing.T) {
	pool := presentationTestDatabase(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'presentation-race@example.test')`, repositoryUserA); err != nil {
		t.Fatal(err)
	}
	repository, _ := New(pool)
	preferences := []presentation.Preference{
		{
			UserID: repositoryUserA, AvatarOptionID: "avatar_lisa",
			VoiceOptionID: "voice_ava", Version: 1,
		},
		{
			UserID: repositoryUserA, AvatarOptionID: "avatar_nathan",
			VoiceOptionID: "voice_john", Version: 1,
		},
	}
	errorsByIndex := make([]error, len(preferences))
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range preferences {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, errorsByIndex[index] = repository.SavePreference(
				ctx, preferences[index], 0,
			)
		}(index)
	}
	close(start)
	group.Wait()
	successes := 0
	conflicts := 0
	for _, saveErr := range errorsByIndex {
		switch {
		case saveErr == nil:
			successes++
		case errors.Is(saveErr, presentation.ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected save error: %v", saveErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d errors=%v", successes, conflicts, errorsByIndex)
	}
}

func presentationTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "coach_presentation_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
	})
	for _, name := range []string{
		"000001_clean_baseline.up.sql",
		"000010_coach_presentation_preferences.up.sql",
		"000011_coach_presentation_runtime.up.sql",
		"000013_lisa_avatar_asset.up.sql",
		"000014_expand_coach_voice_catalog.up.sql",
	} {
		migration, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}
