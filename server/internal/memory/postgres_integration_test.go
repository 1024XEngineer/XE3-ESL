package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	integrationUserA    = "a0000000-0000-4000-8000-000000000001"
	integrationUserB    = "b0000000-0000-4000-8000-000000000001"
	integrationSessionA = "a1000000-0000-4000-8000-000000000001"
	integrationSessionB = "b1000000-0000-4000-8000-000000000001"
	integrationMatterA  = "a2000000-0000-4000-8000-000000000001"
	integrationMatterB  = "b2000000-0000-4000-8000-000000000001"
)

func TestPostgresRepositoryLifecycleIsolationAndDeletionFence(
	t *testing.T,
) {
	database := newMemoryTestDatabase(t)
	repository, err := NewPostgresRepository(
		database,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("NewPostgresRepository: %v", err)
	}
	ctx := context.Background()
	actorA := requestcontext.Actor{
		UserID:    integrationUserA,
		SessionID: integrationSessionA,
	}
	actorB := requestcontext.Actor{
		UserID:    integrationUserB,
		SessionID: integrationSessionB,
	}

	userMemory, err := repository.Create(
		ctx,
		actorA,
		createCommand("career.role", "Java backend engineer"),
	)
	if err != nil {
		t.Fatalf("Create user Memory: %v", err)
	}
	if !userMemory.Valid() || userMemory.Version != 1 {
		t.Fatalf("created Memory = %#v", userMemory)
	}
	if _, err := repository.Create(
		ctx,
		actorA,
		createCommand("career.role", "Platform engineer"),
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active Memory error = %v", err)
	}
	if _, err := repository.Find(
		ctx,
		actorB,
		userMemory.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Find error = %v", err)
	}

	sources, err := repository.ListSources(ctx, actorA, userMemory.ID)
	if err != nil {
		t.Fatalf("ListSources after Create: %v", err)
	}
	if len(sources) != 1 || sources[0].MemoryID != userMemory.ID {
		t.Fatalf("sources after Create = %#v", sources)
	}

	updateSource := evidence(SourceAgentRun, "run-1", 1, "same-source")
	updated, err := repository.Update(ctx, actorA, UpdateCommand{
		MemoryID:        userMemory.ID,
		ExpectedVersion: 1,
		Content:         "Senior Java backend engineer",
		PolicyVersion:   "memory-v1",
		Source:          updateSource,
	})
	if err != nil {
		t.Fatalf("Update Memory: %v", err)
	}
	if updated.Version != 2 ||
		updated.Content != "Senior Java backend engineer" {
		t.Fatalf("updated Memory = %#v", updated)
	}
	updated, err = repository.Update(ctx, actorA, UpdateCommand{
		MemoryID:        userMemory.ID,
		ExpectedVersion: 2,
		Content:         "Senior Java platform engineer",
		PolicyVersion:   "memory-v1",
		Source:          updateSource,
	})
	if err != nil {
		t.Fatalf("Update Memory with duplicate source: %v", err)
	}
	sources, err = repository.ListSources(ctx, actorA, userMemory.ID)
	if err != nil {
		t.Fatalf("ListSources after duplicate source: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("source count = %d, want 2", len(sources))
	}
	if _, err := repository.Update(ctx, actorA, UpdateCommand{
		MemoryID:        userMemory.ID,
		ExpectedVersion: 1,
		Content:         "stale update",
		PolicyVersion:   "memory-v1",
		Source:          evidence(SourceAgentRun, "run-2", 1, "stale"),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Update error = %v", err)
	}

	matterCommand := createCommand("goal.current", "Prepare for PM interview")
	matterCommand.Type = TypeGoal
	matterCommand.Scope = ScopeMatter
	matterCommand.MatterID = integrationMatterA
	matterMemory, err := repository.Create(ctx, actorA, matterCommand)
	if err != nil {
		t.Fatalf("Create matter Memory: %v", err)
	}
	foreignMatterCommand := matterCommand
	foreignMatterCommand.CanonicalKey = "goal.foreign"
	foreignMatterCommand.MatterID = integrationMatterB
	if _, err := repository.Create(
		ctx,
		actorA,
		foreignMatterCommand,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Matter Create error = %v", err)
	}

	userItems, err := repository.ListActive(ctx, actorA, ScopeFilter{
		Scope: ScopeUser,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListActive user: %v", err)
	}
	if len(userItems) != 1 || userItems[0].ID != userMemory.ID {
		t.Fatalf("user active Memories = %#v", userItems)
	}
	matterItems, err := repository.ListActive(ctx, actorA, ScopeFilter{
		Scope:    ScopeMatter,
		MatterID: integrationMatterA,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListActive matter: %v", err)
	}
	if len(matterItems) != 1 || matterItems[0].ID != matterMemory.ID {
		t.Fatalf("matter active Memories = %#v", matterItems)
	}

	if _, err := database.Exec(ctx, `
UPDATE agent_memories
SET expires_at = created_at + INTERVAL '1 microsecond'
WHERE id = $1`,
		matterMemory.ID,
	); err != nil {
		t.Fatalf("expire matter Memory: %v", err)
	}
	matterItems, err = repository.ListActive(ctx, actorA, ScopeFilter{
		Scope:    ScopeMatter,
		MatterID: integrationMatterA,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListActive expired matter: %v", err)
	}
	if len(matterItems) != 0 {
		t.Fatalf("expired matter Memories = %#v", matterItems)
	}

	inactive, err := repository.Inactivate(ctx, actorA, InactivateCommand{
		MemoryID:        userMemory.ID,
		ExpectedVersion: updated.Version,
		Source: evidence(
			SourceAgentMessage,
			"message-correction-1",
			1,
			"correction",
		),
	})
	if err != nil {
		t.Fatalf("Inactivate Memory: %v", err)
	}
	if inactive.Status != StatusInactive ||
		inactive.InactivatedAt == nil ||
		inactive.Version != updated.Version+1 {
		t.Fatalf("inactive Memory = %#v", inactive)
	}
	userItems, err = repository.ListActive(ctx, actorA, ScopeFilter{
		Scope: ScopeUser,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListActive after Inactivate: %v", err)
	}
	if len(userItems) != 0 {
		t.Fatalf("inactive Memory returned by ListActive: %#v", userItems)
	}
	if err := repository.Delete(
		ctx,
		actorB,
		userMemory.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Delete error = %v", err)
	}
	if err := repository.Delete(ctx, actorA, userMemory.ID); err != nil {
		t.Fatalf("Delete Memory: %v", err)
	}
	if _, err := repository.Find(
		ctx,
		actorA,
		userMemory.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find deleted Memory error = %v", err)
	}

	deletionMemory, err := repository.Create(
		ctx,
		actorA,
		createCommand("interest.running", "Enjoys running"),
	)
	if err != nil {
		t.Fatalf("Create deletion Memory: %v", err)
	}
	if _, err := database.Exec(ctx, `
UPDATE identity_users
SET account_status = 'deleting', updated_at = transaction_timestamp()
WHERE id = $1`,
		integrationUserA,
	); err != nil {
		t.Fatalf("mark owner deleting: %v", err)
	}
	if err := repository.DeleteOwnerData(ctx, DeleteOwnerCommand{
		UserID:     integrationUserA,
		Generation: 2,
	}); err != nil {
		t.Fatalf("DeleteOwnerData: %v", err)
	}
	if err := repository.DeleteOwnerData(ctx, DeleteOwnerCommand{
		UserID:     integrationUserA,
		Generation: 2,
	}); err != nil {
		t.Fatalf("repeat DeleteOwnerData: %v", err)
	}
	if err := repository.DeleteOwnerData(ctx, DeleteOwnerCommand{
		UserID:     integrationUserA,
		Generation: 1,
	}); !errors.Is(err, ErrDeletionGeneration) {
		t.Fatalf("stale DeleteOwnerData error = %v", err)
	}
	var memoryCount int
	if err := database.QueryRow(ctx, `
SELECT count(*) FROM agent_memories WHERE owner_user_id = $1`,
		integrationUserA,
	).Scan(&memoryCount); err != nil {
		t.Fatalf("count deleted Memories: %v", err)
	}
	if memoryCount != 0 {
		t.Fatalf("deleted owner Memory count = %d", memoryCount)
	}
	if _, err := repository.Create(
		ctx,
		actorA,
		createCommand("late.write", "must be rejected"),
	); !errors.Is(err, ErrAccountDeleted) {
		t.Fatalf("late Create error = %v", err)
	}
	if _, err := repository.Find(
		ctx,
		actorA,
		deletionMemory.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find owner-deleted Memory error = %v", err)
	}

	if _, err := database.Exec(
		ctx,
		`DELETE FROM matters WHERE owner_user_id = $1`,
		integrationUserA,
	); err != nil {
		t.Fatalf("delete owner Matters: %v", err)
	}
	if _, err := database.Exec(
		ctx,
		`DELETE FROM identity_users WHERE id = $1`,
		integrationUserA,
	); err != nil {
		t.Fatalf("physically delete Identity owner: %v", err)
	}
	if err := repository.DeleteOwnerData(ctx, DeleteOwnerCommand{
		UserID:     integrationUserA,
		Generation: 3,
	}); err != nil {
		t.Fatalf("DeleteOwnerData after Identity removal: %v", err)
	}
	var fenceGeneration int64
	if err := database.QueryRow(ctx, `
SELECT deletion_generation
FROM agent_memory_deletion_fences
WHERE owner_user_id = $1`,
		integrationUserA,
	).Scan(&fenceGeneration); err != nil {
		t.Fatalf("read deletion fence: %v", err)
	}
	if fenceGeneration != 3 {
		t.Fatalf("deletion generation = %d, want 3", fenceGeneration)
	}
}

func createCommand(key string, content string) CreateCommand {
	return CreateCommand{
		Type:          TypeProfile,
		CanonicalKey:  key,
		Content:       content,
		Scope:         ScopeUser,
		PolicyVersion: "memory-v1",
		Source: evidence(
			SourceAgentMessage,
			"message-"+key,
			1,
			key+":"+content,
		),
	}
}

func evidence(
	sourceType SourceType,
	sourceID string,
	version int64,
	content string,
) SourceInput {
	return SourceInput{
		Type:     sourceType,
		SourceID: sourceID,
		Version:  version,
		Checksum: sha256.Sum256([]byte(content)),
	}
}

func newMemoryTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	admin, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal("connect to TEST_DATABASE_URL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "agent_memory_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+identifier,
	); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()

	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal("parse scoped pool config")
	}
	pool, err := pgxpool.NewWithConfig(
		context.Background(),
		poolConfig,
	)
	if err != nil {
		t.Fatal("open scoped pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal("ping scoped pool")
	}
	for _, user := range []struct {
		id    string
		email string
	}{
		{id: integrationUserA, email: "memory-a@example.com"},
		{id: integrationUserB, email: "memory-b@example.com"},
	} {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO identity_users (id, canonical_email)
VALUES ($1, $2)`,
			user.id,
			user.email,
		); err != nil {
			t.Fatalf("insert Identity user: %v", err)
		}
	}
	for _, item := range []struct {
		id      string
		ownerID string
		title   string
	}{
		{
			id:      integrationMatterA,
			ownerID: integrationUserA,
			title:   "User A interview",
		},
		{
			id:      integrationMatterB,
			ownerID: integrationUserB,
			title:   "User B interview",
		},
	} {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO matters (id, owner_user_id, title)
VALUES ($1, $2, $3)`,
			item.id,
			item.ownerID,
			item.title,
		); err != nil {
			t.Fatalf("insert Matter: %v", err)
		}
	}
	return pool
}
