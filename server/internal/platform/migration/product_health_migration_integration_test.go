package migration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var productHealthViews = []string{
	"product_health_daily_artifact_coverage",
	"product_health_daily_evaluation_health",
	"product_health_daily_practice_activity",
	"product_health_daily_scoreability",
	"product_health_daily_session_outcomes",
}

func TestProductHealthViewsAreAnonymousAccurateAndReadOnly(t *testing.T) {
	config, admin, schema := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("initial Up = %t, %v", changed, upErr)
	}

	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect product health database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	seedProductHealthFacts(t, database)

	assertProductHealthViewSet(t, admin, schema)
	assertProductHealthViewHasNoIdentifiers(t, admin, schema)
	assertProductHealthFacts(t, database)
	assertProductHealthReaderPrivileges(t, admin, database, schema)

	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("presentation runtime DownOne = %t, %v", changed, downErr)
	}
	assertProductHealthViewSet(t, admin, schema)
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("coach presentation DownOne = %t, %v", changed, downErr)
	}
	assertProductHealthViewSet(t, admin, schema)
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("IELTS profile DownOne = %t, %v", changed, downErr)
	}
	assertProductHealthViewSet(t, admin, schema)
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("product health DownOne = %t, %v", changed, downErr)
	}
	assertProductHealthViewCount(t, admin, schema, 0)
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("product health second Up = %t, %v", changed, upErr)
	}
	assertProductHealthViewSet(t, admin, schema)
}

func seedProductHealthFacts(t *testing.T, database *pgx.Conn) {
	t.Helper()
	const (
		userID        = "10000000-0000-4000-8000-000000000919"
		planID        = "30000000-0000-4000-8000-000000000919"
		completedID   = "40000000-0000-4000-8000-000000000919"
		endedEarlyID  = "40000000-0000-4000-8000-000000000920"
		questionA     = "50000000-0000-4000-8000-000000000919"
		questionB     = "50000000-0000-4000-8000-000000000920"
		questionC     = "50000000-0000-4000-8000-000000000921"
		effectiveTurn = "60000000-0000-4000-8000-000000000919"
		retryTurn     = "60000000-0000-4000-8000-000000000920"
		legacyTurn    = "60000000-0000-4000-8000-000000000921"
		unknownTurn   = "60000000-0000-4000-8000-000000000922"
	)
	_, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'product-health@example.com');

INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $2, $1, '{}'::jsonb, '{}'::jsonb,
    '{"speech_feedback_allowed":true}'::jsonb, '[{}]'::jsonb,
    'conversation', 'ready', 'request-product-health-plan',
    decode(repeat('11', 32), 'hex')
);

INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    effective_turns, plan_snapshot, participants, presentation_snapshot, initial_client_request_id,
    initial_request_fingerprint, started_at, ended_at, end_reason,
    created_at, updated_at
) VALUES
(
    $3, $1, $2, 1, 'conversation', 'general', 'voice',
    'general.evaluation.v1', 'completed', 2,
    '{"session_policy":{"speech_feedback_allowed":true}}'::jsonb,
    '[{}]'::jsonb, '{"schema_version":1,"avatar":{"option_id":"avatar_lisa","provider":"spatialreal","provider_profile":"spatialreal_default","provider_avatar_id":"avatar","binding_version":1},"voice":{"option_id":"voice_ava","provider":"qianwen","provider_profile":"qianwen_default","provider_model":"model","provider_voice_id":"voice","locale":"en-US","binding_version":1}}'::jsonb, 'request-product-health-completed',
    decode(repeat('12', 32), 'hex'),
    '2026-08-23 00:10:00+00', '2026-08-23 01:00:00+00', 'completed',
    '2026-08-23 00:00:00+00', '2026-08-23 01:00:00+00'
),
(
    $4, $1, $2, 1, 'conversation', 'general', 'voice',
    'general.evaluation.v1', 'ended_early', 1, '{}'::jsonb,
    '[{}]'::jsonb, '{"schema_version":1,"avatar":{"option_id":"avatar_lisa","provider":"spatialreal","provider_profile":"spatialreal_default","provider_avatar_id":"avatar","binding_version":1},"voice":{"option_id":"voice_ava","provider":"qianwen","provider_profile":"qianwen_default","provider_model":"model","provider_voice_id":"voice","locale":"en-US","binding_version":1}}'::jsonb, 'request-product-health-ended',
    decode(repeat('13', 32), 'hex'),
    '2026-08-23 02:10:00+00', '2026-08-23 02:30:00+00', 'user_ended',
    '2026-08-23 02:00:00+00', '2026-08-23 02:30:00+00'
);

INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence
) VALUES
($5, $3, 'objective-a', 'prompt', 'Question A', 'coach', ARRAY['learner'], 1),
($6, $3, 'objective-b', 'prompt', 'Question B', 'coach', ARRAY['learner'], 2),
($7, $4, 'objective-c', 'prompt', 'Question C', 'coach', ARRAY['learner'], 1);

INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, original_turn_id, client_request_id,
    counts_toward_turn_limit, candidate_id, transcript_id, evidence_version,
    transcript, interaction_mode, confirmed_at, created_at, updated_at
) VALUES
(
    $8, $3, $5, 'learner', 1, 'EFFECTIVE', 'confirmed', NULL, NULL, true,
    gen_random_uuid(), 'transcript-product-health-a', 1, 'Answer A', 'TEXT',
    '2026-08-23 00:20:00+00', '2026-08-23 00:15:00+00',
    '2026-08-23 00:20:00+00'
),
(
    $9, $3, $5, 'learner', 2, 'RETRY', 'confirmed', $8,
    'request-product-health-retry', false, gen_random_uuid(),
    'transcript-product-health-retry', 1, 'Answer retry', 'PUSH_TO_TALK',
    '2026-08-23 00:30:00+00', '2026-08-23 00:25:00+00',
    '2026-08-23 00:30:00+00'
),
(
    $10, $3, $6, 'learner', 3, 'EFFECTIVE', 'confirmed', NULL, NULL, true,
    gen_random_uuid(), 'transcript-product-health-b', 1, 'Answer B', 'LEGACY',
    '2026-08-23 00:40:00+00', '2026-08-23 00:35:00+00',
    '2026-08-23 00:40:00+00'
),
(
    $11, $4, $7, 'learner', 1, 'EFFECTIVE', 'confirmed', NULL, NULL, true,
    gen_random_uuid(), 'transcript-product-health-c', 1, 'Answer C', NULL,
    '2026-08-23 02:20:00+00', '2026-08-23 02:15:00+00',
    '2026-08-23 02:20:00+00'
);

INSERT INTO evaluations (
    user_id, kind, source_id, context_id, status, input_snapshot, input_hash,
    config_lineage, config_hash, result, error, attempt_count, created_at,
    updated_at, started_at, finished_at
) VALUES
(
    $1, 'PRACTICE_TURN_FEEDBACK', $8, $3, 'READY', '{}'::json,
    decode(repeat('21', 32), 'hex'), '{}'::json,
    decode(repeat('22', 32), 'hex'),
    '{"scoreability_status":"INSUFFICIENT"}'::jsonb, NULL, 1,
    '2026-08-23 00:20:00+00', '2026-08-23 00:20:30+00',
    '2026-08-23 00:20:10+00', '2026-08-23 00:20:30+00'
),
(
    $1, 'PRACTICE_TURN_FEEDBACK', $9, $3, 'FAILED', '{}'::json,
    decode(repeat('23', 32), 'hex'), '{}'::json,
    decode(repeat('24', 32), 'hex'), NULL,
    '{"code":"PROVIDER_FAILED","retryable":false,"message":"failed"}'::jsonb,
    2,
    '2026-08-23 00:30:00+00', '2026-08-23 00:31:00+00',
    '2026-08-23 00:30:20+00', '2026-08-23 00:31:00+00'
),
(
    $1, 'SESSION_REPORT', $3, $3, 'READY', '{}'::json,
    decode(repeat('25', 32), 'hex'), '{}'::json,
    decode(repeat('26', 32), 'hex'),
    '{"scoreability_status":"PROVISIONAL"}'::jsonb, NULL, 1,
    '2026-08-23 01:00:00+00', '2026-08-23 01:02:00+00',
    '2026-08-23 01:00:30+00', '2026-08-23 01:02:00+00'
),
(
    $1, 'AGENT_MESSAGE_FEEDBACK', gen_random_uuid(), gen_random_uuid(),
    'READY', '{}'::json, decode(repeat('27', 32), 'hex'), '{}'::json,
    decode(repeat('28', 32), 'hex'), '{}'::jsonb, NULL, 1,
    '2026-08-23 03:00:00+00', '2026-08-23 03:00:20+00',
    '2026-08-23 03:00:05+00', '2026-08-23 03:00:20+00'
);
`, userID, planID, completedID, endedEarlyID, questionA, questionB, questionC,
		effectiveTurn, retryTurn, legacyTurn, unknownTurn)
	if err != nil {
		t.Fatalf("seed product health facts: %v", err)
	}
}

func assertProductHealthViewSet(t *testing.T, database *pgx.Conn, schema string) {
	t.Helper()
	rows, err := database.Query(context.Background(), `
SELECT table_name
FROM information_schema.views
WHERE table_schema = $1 AND table_name LIKE 'product_health_%'
ORDER BY table_name
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(productHealthViews) {
		t.Fatalf("product health views = %v, want %v", got, productHealthViews)
	}
}

func assertProductHealthViewCount(
	t *testing.T,
	database *pgx.Conn,
	schema string,
	want int,
) {
	t.Helper()
	var got int
	if err := database.QueryRow(context.Background(), `
SELECT count(*) FROM information_schema.views
WHERE table_schema = $1 AND table_name LIKE 'product_health_%'
`, schema).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("product health view count = %d, want %d", got, want)
	}
}

func assertProductHealthViewHasNoIdentifiers(
	t *testing.T,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	rows, err := database.Query(context.Background(), `
SELECT table_name, column_name, data_type
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name LIKE 'product_health_%'
  AND (
      data_type = 'uuid'
      OR column_name ~ '(^|_)(user|session|turn|evaluation)_?id($|_)'
  )
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	identifiers, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		Table string
		Name  string
		Type  string
	}])
	if err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 0 {
		t.Fatalf("product health identifier columns = %#v", identifiers)
	}
}

func assertProductHealthFacts(t *testing.T, database *pgx.Conn) {
	t.Helper()
	var activeUsers, sessions, turns, effective, retries int64
	var retryTurnShare, retrySessionShare float64
	var textTurns, voiceTurns, unknownTurns int64
	err := database.QueryRow(context.Background(), `
SELECT active_practice_users, sessions_with_confirmed_turn, confirmed_turns,
       effective_confirmed_turns, retry_confirmed_turns, retry_turn_share,
       retry_session_share, text_confirmed_turns, voice_confirmed_turns,
       unknown_mode_confirmed_turns
FROM product_health_daily_practice_activity
`).Scan(&activeUsers, &sessions, &turns, &effective, &retries,
		&retryTurnShare, &retrySessionShare, &textTurns, &voiceTurns, &unknownTurns)
	if err != nil {
		t.Fatal(err)
	}
	if activeUsers != 1 || sessions != 2 || turns != 4 || effective != 3 ||
		retries != 1 || retryTurnShare != 0.25 || retrySessionShare != 0.5 ||
		textTurns != 1 || voiceTurns != 1 || unknownTurns != 2 {
		t.Fatalf("practice activity = %d %d %d %d %d %.2f %.2f %d %d %d",
			activeUsers, sessions, turns, effective, retries, retryTurnShare,
			retrySessionShare, textTurns, voiceTurns, unknownTurns)
	}

	var completed, endedEarly, terminal int64
	var completionRate, endedEarlyRate float64
	if err := database.QueryRow(context.Background(), `
SELECT completed_sessions, ended_early_sessions, terminal_sessions,
       completion_rate, ended_early_rate
FROM product_health_daily_session_outcomes
`).Scan(&completed, &endedEarly, &terminal, &completionRate, &endedEarlyRate); err != nil {
		t.Fatal(err)
	}
	if completed != 1 || endedEarly != 1 || terminal != 2 ||
		completionRate != 0.5 || endedEarlyRate != 0.5 {
		t.Fatalf("session outcomes = %d %d %d %.2f %.2f",
			completed, endedEarly, terminal, completionRate, endedEarlyRate)
	}

	var eligible, unknown, scheduled, missing, ready, failed int64
	var scheduledRate, readyRate float64
	if err := database.QueryRow(context.Background(), `
SELECT eligible_sources, unknown_eligibility_sources, scheduled_evaluations,
       missing_evaluations, ready_evaluations, failed_evaluations,
       scheduled_coverage_rate, ready_coverage_rate
FROM product_health_daily_artifact_coverage
WHERE artifact_kind = 'TURN_FEEDBACK'
`).Scan(&eligible, &unknown, &scheduled, &missing, &ready, &failed,
		&scheduledRate, &readyRate); err != nil {
		t.Fatal(err)
	}
	if eligible != 3 || unknown != 1 || scheduled != 2 || missing != 1 ||
		ready != 1 || failed != 1 || scheduledRate != 2.0/3.0 ||
		readyRate != 1.0/3.0 {
		t.Fatalf("feedback coverage = %d %d %d %d %d %d %.4f %.4f",
			eligible, unknown, scheduled, missing, ready, failed, scheduledRate, readyRate)
	}
	if err := database.QueryRow(context.Background(), `
SELECT eligible_sources, unknown_eligibility_sources, scheduled_evaluations,
       missing_evaluations, ready_evaluations, failed_evaluations,
       scheduled_coverage_rate, ready_coverage_rate
FROM product_health_daily_artifact_coverage
WHERE artifact_kind = 'SESSION_REPORT'
`).Scan(&eligible, &unknown, &scheduled, &missing, &ready, &failed,
		&scheduledRate, &readyRate); err != nil {
		t.Fatal(err)
	}
	if eligible != 1 || unknown != 0 || scheduled != 1 || missing != 0 ||
		ready != 1 || failed != 0 || scheduledRate != 1 || readyRate != 1 {
		t.Fatalf("report coverage = %d %d %d %d %d %d %.4f %.4f",
			eligible, unknown, scheduled, missing, ready, failed,
			scheduledRate, readyRate)
	}

	var feedbackReady, feedbackFailed, feedbackTerminal int64
	var successRate, initialP95, processingP95, totalP95 float64
	if err := database.QueryRow(context.Background(), `
SELECT ready_jobs, failed_jobs, terminal_jobs, terminal_success_rate,
       initial_queue_p95_seconds, processing_lifecycle_p95_seconds,
       total_lifecycle_p95_seconds
FROM product_health_daily_evaluation_health
WHERE evaluation_kind = 'PRACTICE_TURN_FEEDBACK'
`).Scan(&feedbackReady, &feedbackFailed, &feedbackTerminal, &successRate,
		&initialP95, &processingP95, &totalP95); err != nil {
		t.Fatal(err)
	}
	if feedbackReady != 1 || feedbackFailed != 1 || feedbackTerminal != 2 ||
		successRate != 0.5 || initialP95 != 19.5 || processingP95 != 39 ||
		totalP95 != 58.5 {
		t.Fatalf("evaluation health = %d %d %d %.2f %.2f %.2f %.2f",
			feedbackReady, feedbackFailed, feedbackTerminal, successRate,
			initialP95, processingP95, totalP95)
	}

	var insufficient, scoreabilityUnknown int64
	var insufficientShare float64
	if err := database.QueryRow(context.Background(), `
SELECT sum(insufficient_evaluations), sum(unknown_scoreability_evaluations),
       sum(insufficient_evaluations)::double precision
           / NULLIF(sum(ready_evaluations), 0)
FROM product_health_daily_scoreability
`).Scan(&insufficient, &scoreabilityUnknown, &insufficientShare); err != nil {
		t.Fatal(err)
	}
	if insufficient != 1 || scoreabilityUnknown != 1 || insufficientShare != 1.0/3.0 {
		t.Fatalf("scoreability = %d %d %.4f",
			insufficient, scoreabilityUnknown, insufficientShare)
	}

	if _, err := database.Exec(context.Background(), "SET TIME ZONE 'Asia/Shanghai'"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.Exec(context.Background(), "SET TIME ZONE 'UTC'") }()
	rows, err := database.Query(context.Background(), `
SELECT day_utc FROM product_health_daily_practice_activity
UNION ALL SELECT day_utc FROM product_health_daily_session_outcomes
UNION ALL SELECT day_utc FROM product_health_daily_artifact_coverage
UNION ALL SELECT day_utc FROM product_health_daily_evaluation_health
UNION ALL SELECT day_utc FROM product_health_daily_scoreability
`)
	if err != nil {
		t.Fatal(err)
	}
	days, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		t.Fatal(err)
	}
	wantDay := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for _, day := range days {
		if !day.Equal(wantDay) {
			t.Fatalf("UTC day = %s", day)
		}
	}
}

func assertProductHealthReaderPrivileges(
	t *testing.T,
	admin *pgx.Conn,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	role := fmt.Sprintf("product_health_reader_test_%d", time.Now().UnixNano())
	roleIdentifier := pgx.Identifier{role}.Sanitize()
	schemaIdentifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE ROLE "+roleIdentifier+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+roleIdentifier)
	})
	if _, err := admin.Exec(context.Background(),
		"GRANT USAGE ON SCHEMA "+schemaIdentifier+" TO "+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	for _, view := range productHealthViews {
		qualified := pgx.Identifier{schema, view}.Sanitize()
		if _, err := admin.Exec(context.Background(),
			"GRANT SELECT ON "+qualified+" TO "+roleIdentifier); err != nil {
			t.Fatal(err)
		}
	}

	for _, view := range productHealthViews {
		var allowed bool
		if err := admin.QueryRow(context.Background(), `
SELECT has_table_privilege($1, $2, 'SELECT')
`, role, schema+"."+view).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("reader cannot SELECT %s", view)
		}
	}
	for _, table := range []string{
		"users", "practice_sessions", "practice_turns", "evaluations",
		"evaluation_feedback_items",
	} {
		var allowed bool
		if err := admin.QueryRow(context.Background(), `
SELECT has_table_privilege($1, $2, 'SELECT')
`, role, schema+"."+table).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Fatalf("reader can SELECT raw table %s", table)
		}
	}

	if _, err := database.Exec(context.Background(), "SET ROLE "+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.Exec(context.Background(), "RESET ROLE") }()
	if _, err := database.Exec(context.Background(),
		"SELECT count(*) FROM "+pgx.Identifier{schema, productHealthViews[0]}.Sanitize()); err != nil {
		t.Fatalf("reader SELECT aggregate view: %v", err)
	}
	_, rawErr := database.Exec(context.Background(),
		"SELECT count(*) FROM "+pgx.Identifier{schema, "users"}.Sanitize())
	if rawErr == nil || !strings.Contains(rawErr.Error(), "permission denied") {
		t.Fatalf("reader raw SELECT error = %v", rawErr)
	}
}
