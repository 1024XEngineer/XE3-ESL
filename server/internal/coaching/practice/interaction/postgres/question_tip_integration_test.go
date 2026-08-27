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

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRenewQuestionTipLeaseRequiresCurrentUnexpiredFencingToken(t *testing.T) {
	pool := questionTipTestDatabase(t)
	ctx := context.Background()
	actor := practiceinteraction.Actor{
		UserID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "auth-session-1",
	}
	planID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sessionID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	questionID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	tipID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	baseNow := time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC)
	currentNow := baseNow.Add(5 * time.Second)

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, canonical_email) VALUES ($1, 'tip-lease@example.com')
	`, actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_plans (
			user_id, plan_id, preparation_snapshot, scene_selection, session_policy,
			practice_objectives, practice_experience, status, initial_client_request_id,
			initial_request_fingerprint
		) VALUES ($1,$2,'{}','{}','{}','[{}]','INTERVIEW','ready','plan-create-1',$3)
	`, actor.UserID, planID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_sessions (
			session_id, user_id, plan_id, plan_version, practice_experience,
			scene_category, practice_mode, evaluation_policy_ref, plan_snapshot,
			participants, initial_client_request_id, initial_request_fingerprint,
			created_at, updated_at
		) VALUES ($1,$2,$3,1,'INTERVIEW','INTERVIEW_PROFESSIONAL','FULL_SIMULATION',
			'test-policy','{}','[{}]','session-create-1',$4,$5,$5)
	`, sessionID, actor.UserID, planID, make([]byte, 32), baseNow); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_questions (
			question_id, session_id, objective_id, question_type, content,
			speaker_participant_id, addressee_participant_ids, sequence,
			tip_id, tip_client_request_id, tip_status, tip_fencing_token,
			tip_lease_expires_at, tip_created_at, created_at, updated_at
		) VALUES ($1,$2,'objective-1','INTERVIEW','Tell me about a result.',
			'coach',ARRAY['candidate'],1,$3,'tip-operation-1','processing',7,
			$5,$4,$4,$4)
	`, questionID, sessionID, tipID, baseNow, baseNow.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}

	repository := &Repository{pool: pool, now: func() time.Time { return currentNow }}
	command := practiceinteraction.RenewQuestionTipLeaseCommand{
		TipID: tipID, FencingToken: 7, LeaseDuration: time.Minute,
	}
	if err := repository.RenewQuestionTipLease(ctx, actor, command); err != nil {
		t.Fatalf("renew current lease: %v", err)
	}
	var leaseExpiresAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT tip_lease_expires_at FROM practice_questions WHERE question_id=$1
	`, questionID).Scan(&leaseExpiresAt); err != nil {
		t.Fatal(err)
	}
	if !leaseExpiresAt.Equal(currentNow.Add(time.Minute)) {
		t.Fatalf("lease expiry=%s", leaseExpiresAt)
	}

	stale := command
	stale.FencingToken = 6
	if err := repository.RenewQuestionTipLease(ctx, actor, stale); !errors.Is(err, practiceinteraction.ErrPersistenceConflict) {
		t.Fatalf("stale fencing error=%v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE practice_questions
		SET tip_lease_expires_at=$2, updated_at=$3
		WHERE question_id=$1
	`, questionID, baseNow.Add(4*time.Second), baseNow.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repository.RenewQuestionTipLease(ctx, actor, command); !errors.Is(err, practiceinteraction.ErrPersistenceConflict) {
		t.Fatalf("expired lease error=%v", err)
	}

	currentNow = baseNow.Add(10 * time.Second)
	if _, err := pool.Exec(ctx, `
		UPDATE practice_questions
		SET tip_lease_expires_at=$2, updated_at=$3
		WHERE question_id=$1
	`, questionID, baseNow.Add(40*time.Second), baseNow.Add(9*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteQuestionTip(
		ctx,
		actor,
		practiceinteraction.CompleteQuestionTipCommand{
			TipID: tipID, FencingToken: 7,
			Content: "I delivered the result.", Translation: "我交付了结果。",
			Provider: "qianwen", Model: "qwen-plus", ProviderRequestID: "request-1",
		},
	); err != nil {
		t.Fatalf("complete tip: %v", err)
	}
	if err := repository.RenewQuestionTipLease(ctx, actor, command); !errors.Is(err, practiceinteraction.ErrPersistenceConflict) {
		t.Fatalf("completed lease error=%v", err)
	}
}

func questionTipTestDatabase(t *testing.T) *pgxpool.Pool {
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
	schemaName := "question_tip_lease_" + hex.EncodeToString(suffix[:])
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
	for _, name := range []string{
		"000001_clean_baseline.up.sql",
		"000002_agent_run_domain_completion.up.sql",
		"000003_archive_practice_plans.up.sql",
		"000004_question_tip_translation.up.sql",
		"000005_scene_selection_source.up.sql",
		"000006_user_profile_avatar.up.sql",
		"000007_pending_practice_actions.up.sql",
		"000008_product_health_views.up.sql",
		"000009_ielts_incremental_profile.up.sql",
		"000010_unique_progressed_practice_turn.up.sql",
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
