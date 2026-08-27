package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
)

const (
	contextTestUserID        = "61000000-0000-4000-8000-000000000001"
	contextTestActorSession  = "62000000-0000-4000-8000-000000000001"
	contextTestPlanID        = "63000000-0000-4000-8000-000000000001"
	contextTestSessionA      = "64000000-0000-4000-8000-000000000001"
	contextTestSessionB      = "64000000-0000-4000-8000-000000000002"
	contextTestSessionReplay = "64000000-0000-4000-8000-000000000003"
)

func TestCreateSessionReplaysOnlyTheSameIdempotencyIntent(t *testing.T) {
	pool := practiceContextTestDatabase(t)
	seedPracticeContextOwnerAndPlan(t, pool)
	repository, err := New(
		pool,
		contextCompletionScheduler{},
		contextTurnFeedbackScheduler{},
		contextIELTSProfileScheduler{},
		contextIDGenerator{},
	)
	if err != nil {
		t.Fatal(err)
	}
	actor := practice.Actor{
		UserID:    contextTestUserID,
		SessionID: contextTestActorSession,
	}
	fingerprint := sha256.Sum256([]byte("expected-plan-version:1"))

	firstCommand := contextCreateSessionCommand(
		contextTestSessionA,
		"session-create-a",
		fingerprint,
	)
	first, replayed, err := repository.CreateSession(
		context.Background(), actor, firstCommand,
	)
	if err != nil || replayed || first.Session.ID != contextTestSessionA {
		t.Fatalf("first CreateSession = %#v, replayed=%t, err=%v", first, replayed, err)
	}
	if first.Snapshot.Presentation.Avatar.OptionID != "avatar_lisa" ||
		first.Snapshot.Presentation.Voice.OptionID != "voice_ava" {
		t.Fatalf("default presentation snapshot = %#v", first.Snapshot.Presentation)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO user_coach_presentation_preferences
(user_id,avatar_option_id,voice_option_id,version)
VALUES ($1,'avatar_nathan','voice_john',1)`, contextTestUserID); err != nil {
		t.Fatalf("save changed presentation preference: %v", err)
	}

	replayCommand := contextCreateSessionCommand(
		contextTestSessionReplay,
		firstCommand.ClientRequestID,
		fingerprint,
	)
	replay, replayed, err := repository.CreateSession(
		context.Background(), actor, replayCommand,
	)
	if err != nil || !replayed || replay.Session.ID != contextTestSessionA {
		t.Fatalf("replayed CreateSession = %#v, replayed=%t, err=%v", replay, replayed, err)
	}
	if replay.Snapshot.Presentation != first.Snapshot.Presentation {
		t.Fatalf("replayed presentation changed: first=%#v replay=%#v", first.Snapshot.Presentation, replay.Snapshot.Presentation)
	}

	changedFingerprint := sha256.Sum256([]byte("expected-plan-version:2"))
	conflictCommand := contextCreateSessionCommand(
		contextTestSessionReplay,
		firstCommand.ClientRequestID,
		changedFingerprint,
	)
	if _, _, err := repository.CreateSession(
		context.Background(), actor, conflictCommand,
	); !errors.Is(err, practice.ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency payload error = %v", err)
	}

	secondCommand := contextCreateSessionCommand(
		contextTestSessionB,
		"session-create-b",
		fingerprint,
	)
	second, replayed, err := repository.CreateSession(
		context.Background(), actor, secondCommand,
	)
	if err != nil || replayed || second.Session.ID != contextTestSessionB {
		t.Fatalf("second CreateSession = %#v, replayed=%t, err=%v", second, replayed, err)
	}
	if second.Snapshot.Presentation.Avatar.OptionID != "avatar_nathan" ||
		second.Snapshot.Presentation.Voice.OptionID != "voice_john" {
		t.Fatalf("changed presentation snapshot = %#v", second.Snapshot.Presentation)
	}
	if second.Session.PlanID != first.Session.PlanID {
		t.Fatalf("same Plan created Sessions for different Plans: %#v, %#v", first.Session, second.Session)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM practice_sessions WHERE user_id=$1 AND plan_id=$2`, contextTestUserID, contextTestPlanID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("same Plan Session count = %d, want 2", count)
	}
}

func contextCreateSessionCommand(
	sessionID string,
	clientRequestID string,
	fingerprint [sha256.Size]byte,
) practice.CreateSessionCommand {
	selection := practice.SceneSelection{
		Scene: practice.SceneDefinition{
			ID:         "interview-scene",
			Experience: practice.PracticeExperienceInterview,
			Category:   practice.SceneCategory("INTERVIEW_PROFESSIONAL"),
			Name:       "Interview",
			Version:    1,
			Status:     practice.SceneStatusActive,
			PracticeOptions: []practice.PracticeOption{{
				ID:                  "full-simulation",
				Mode:                practice.PracticeModeFullSimulation,
				EvaluationPolicyRef: "interview.evaluation.v1",
			}},
		},
		PracticeOptionID: "full-simulation",
	}
	return practice.CreateSessionCommand{
		SessionID:   sessionID,
		PlanID:      contextTestPlanID,
		PlanVersion: 1,
		Snapshot: practice.SessionSnapshot{
			SessionID:      sessionID,
			PlanVersion:    1,
			Experience:     practice.PracticeExperienceInterview,
			Category:       practice.SceneCategory("INTERVIEW_PROFESSIONAL"),
			PracticeMode:   practice.PracticeModeFullSimulation,
			SceneSelection: selection,
			Participants: []practice.Participant{{
				ID:        "65000000-0000-4000-8000-000000000001",
				SessionID: sessionID,
				Role:      "LEARNER",
				SubjectRef: practice.SubjectRef{
					Namespace: "speakup.user",
					SubjectID: contextTestUserID,
				},
				Order: 1,
			}},
		},
		ClientRequestID:    clientRequestID,
		RequestFingerprint: fingerprint,
	}
}

func seedPracticeContextOwnerAndPlan(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,canonical_email) VALUES ($1,$2)`, contextTestUserID, "practice-context@example.com"); err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256.Sum256([]byte("plan-create"))
	if _, err := pool.Exec(ctx, `INSERT INTO practice_plans (
plan_id,user_id,preparation_snapshot,scene_selection,session_policy,
practice_objectives,practice_experience,status,version,
initial_client_request_id,initial_request_fingerprint)
VALUES ($1,$2,'{}','{}','{}','[{}]','INTERVIEW','ready',1,$3,$4)`,
		contextTestPlanID, contextTestUserID, "plan-create-context", fingerprint[:]); err != nil {
		t.Fatal(err)
	}
}

type contextCompletionScheduler struct{}

func (contextCompletionScheduler) ScheduleCompletedSession(
	context.Context,
	pgx.Tx,
	practice.SessionEvidence,
) error {
	return nil
}

type contextTurnFeedbackScheduler struct{}

func (contextTurnFeedbackScheduler) ScheduleConfirmedTurn(
	context.Context,
	pgx.Tx,
	practice.TurnFeedbackEvidence,
) error {
	return nil
}

type contextIELTSProfileScheduler struct{}

func (contextIELTSProfileScheduler) ScheduleCompletedPart(
	context.Context,
	pgx.Tx,
	practice.IELTSPartProfileEvidence,
) error {
	return nil
}

type contextIDGenerator struct{}

func (contextIDGenerator) NewID() (string, error) {
	return "66000000-0000-4000-8000-000000000001", nil
}

func practiceContextTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
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
		t.Fatal(err)
	}
	schema := "practice_context_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})
	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()
	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return pool
}
