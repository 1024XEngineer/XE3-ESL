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

	coachingprofile "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/profile"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const repositoryUserID = "10000000-0000-4000-8000-000000000001"
const deletingBeforeInsertUserID = "10000000-0000-4000-8000-000000000002"

func TestRepositoryConcurrentFirstInsertHasOneWinner(t *testing.T) {
	pool := profileTestDatabase(t)
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
        INSERT INTO users (id, canonical_email)
        VALUES ($1, 'profile-owner@example.test')
    `, repositoryUserID); err != nil {
		t.Fatal(err)
	}

	items := []coachingprofile.Profile{
		newFirstProfile("designer"),
		newFirstProfile("teacher"),
	}
	errorsByIndex := make([]error, len(items))
	results := make([]coachingprofile.Profile, len(items))
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range items {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			results[index], errorsByIndex[index] = repository.Save(
				ctx,
				items[index],
				0,
			)
		}(index)
	}
	close(start)
	group.Wait()

	successes := 0
	conflicts := 0
	for index, saveErr := range errorsByIndex {
		switch {
		case saveErr == nil:
			successes++
			if results[index].Version != 1 || !results[index].ValidStored() {
				t.Fatalf("saved profile = %#v", results[index])
			}
		case errors.Is(saveErr, coachingprofile.ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("save error %d = %v", index, saveErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d errors=%v", successes, conflicts, errorsByIndex)
	}
	persisted, err := repository.Find(ctx, repositoryUserID)
	if err != nil || persisted.Version != 1 ||
		(persisted.Data.Occupation != "designer" &&
			persisted.Data.Occupation != "teacher") {
		t.Fatalf("persisted profile = %#v err=%v", persisted, err)
	}

	update := persisted
	update.MemoryEnabled = false
	update.Version = 2
	if _, err := repository.Save(ctx, update, 1); err != nil {
		t.Fatalf("update winner: %v", err)
	}
	if _, err := repository.Save(ctx, update, 1); !errors.Is(err, coachingprofile.ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestRepositoryRejectsWritesAfterOwnerStartsDeleting(t *testing.T) {
	pool := profileTestDatabase(t)
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
        INSERT INTO users (id, canonical_email)
        VALUES ($1, 'profile-deleting@example.test')
    `, repositoryUserID); err != nil {
		t.Fatal(err)
	}

	first := newFirstProfile("designer")
	saved, err := repository.Save(ctx, first, 0)
	if err != nil {
		t.Fatalf("save active owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        UPDATE users SET status = 'deleting' WHERE id = $1
    `, repositoryUserID); err != nil {
		t.Fatal(err)
	}

	next := saved
	next.Data.Occupation = "teacher"
	next.Version++
	if _, err := repository.Save(ctx, next, saved.Version); !errors.Is(
		err,
		coachingprofile.ErrVersionConflict,
	) {
		t.Fatalf("deleting owner update error = %v", err)
	}
	persisted, err := repository.Find(ctx, repositoryUserID)
	if err != nil || persisted.Data.Occupation != "designer" ||
		persisted.Version != saved.Version {
		t.Fatalf("profile changed after deleting: %#v, %v", persisted, err)
	}

	if _, err := pool.Exec(ctx, `
        INSERT INTO users (id, canonical_email, status)
        VALUES ($1, 'profile-already-deleting@example.test', 'deleting')
    `, deletingBeforeInsertUserID); err != nil {
		t.Fatal(err)
	}
	firstForDeletingOwner := newFirstProfile("teacher")
	firstForDeletingOwner.UserID = deletingBeforeInsertUserID
	if _, err := repository.Save(
		ctx,
		firstForDeletingOwner,
		0,
	); !errors.Is(err, coachingprofile.ErrVersionConflict) {
		t.Fatalf("deleting owner first insert error = %v", err)
	}
}

func newFirstProfile(occupation string) coachingprofile.Profile {
	return coachingprofile.Profile{
		UserID:        repositoryUserID,
		MemoryEnabled: true,
		Data:          coachingprofile.Data{Occupation: occupation},
		FieldSources: map[coachingprofile.Field]coachingprofile.FieldSource{
			coachingprofile.FieldOccupation: {
				Type:       coachingprofile.SourceUserSetting,
				RecordedAt: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC),
			},
		},
		Version: 1,
	}
}

func profileTestDatabase(t *testing.T) *pgxpool.Pool {
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
	schema := "coaching_profile_" + hex.EncodeToString(suffix[:])
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
	baseline, err := migrations.Files.ReadFile("000001_clean_baseline.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(baseline)); err != nil {
		t.Fatalf("apply clean baseline: %v", err)
	}
	return pool
}
