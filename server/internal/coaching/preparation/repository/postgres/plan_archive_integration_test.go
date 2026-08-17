package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestArchivePlanIsOwnerScopedAndPreservesExistingSessions(t *testing.T) {
	pool := planArchiveTestDatabase(t)
	ctx := context.Background()
	owner := requestcontext.Actor{UserID: "11111111-1111-4111-8111-111111111111", SessionID: "owner-session"}
	other := requestcontext.Actor{UserID: "22222222-2222-4222-8222-222222222222", SessionID: "other-session"}
	planID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sessionID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, canonical_email) VALUES ($1,$2),($3,$4)`, owner.UserID, "plan-owner@example.com", other.UserID, "plan-other@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO practice_plans (
user_id, plan_id, preparation_snapshot, scene_selection, session_policy,
practice_objectives, practice_experience, status, initial_client_request_id,
initial_request_fingerprint)
VALUES ($1,$2,'{}','{}','{}','[{}]','INTERVIEW','ready','plan-create-1',$3)`, owner.UserID, planID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO practice_sessions (
session_id, user_id, plan_id, plan_version, practice_experience,
scene_category, practice_mode, evaluation_policy_ref, plan_snapshot,
participants, initial_client_request_id, initial_request_fingerprint)
VALUES ($1,$2,$3,1,'INTERVIEW','INTERVIEW_PROFESSIONAL','FULL_SIMULATION',
'test-policy','{}','[{}]','session-create-1',$4)`, sessionID, owner.UserID, planID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}

	repository := NewPostgresPlanRepository(pool)
	if err := repository.ArchivePlan(ctx, other, planID); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("foreign archive error=%v", err)
	}
	if err := repository.ArchivePlan(ctx, owner, planID); err != nil {
		t.Fatalf("owner archive: %v", err)
	}
	if err := repository.ArchivePlan(ctx, owner, planID); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("repeat archive error=%v", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM practice_plans WHERE plan_id=$1`, planID).Scan(&status); err != nil || status != "archived" {
		t.Fatalf("status=%q error=%v", status, err)
	}
	plans, err := repository.ListCurrentPlans(ctx, owner, scene.PracticeExperience("INTERVIEW"))
	if err != nil || len(plans) != 0 {
		t.Fatalf("listed plans=%d error=%v", len(plans), err)
	}
	if _, err := repository.ReadExecutablePlan(ctx, owner, planID, 1); !errors.Is(err, preparation.ErrPlanNotFound) {
		t.Fatalf("executable archived plan error=%v", err)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM practice_sessions WHERE session_id=$1 AND plan_id=$2`, sessionID, planID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("preserved sessions=%d error=%v", sessions, err)
	}
}

func planArchiveTestDatabase(t *testing.T) *pgxpool.Pool {
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
	schemaName := "preparation_archive_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{schemaName}.Sanitize()
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
	domainCompletionMigration, err := migrations.Files.ReadFile("000002_agent_run_domain_completion.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(domainCompletionMigration)); err != nil {
		t.Fatalf("apply Agent domain completion migration: %v", err)
	}
	archiveMigration, err := migrations.Files.ReadFile("000003_archive_practice_plans.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(archiveMigration)); err != nil {
		t.Fatalf("apply practice plan archive migration: %v", err)
	}
	return pool
}
