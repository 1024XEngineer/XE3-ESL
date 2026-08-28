package migration

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var userBehaviorViews = []string{
	"user_behavior_current_nonterminal_sessions",
	"user_behavior_daily_early_end",
	"user_behavior_daily_feature_usage",
	"user_behavior_daily_repractice",
	"user_behavior_daily_retention",
	"user_behavior_daily_session_funnel",
	"user_behavior_daily_time_to_first_effective_turn",
}

var userBehaviorIndexes = []string{
	"user_behavior_confirmed_turns_day_idx",
	"user_behavior_nonterminal_sessions_updated_idx",
	"user_behavior_ready_session_reports_idx",
	"user_behavior_sessions_created_day_idx",
}

func TestUserBehaviorViewsAreAnonymousAccurateAndReadOnly(t *testing.T) {
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
		t.Fatalf("connect user behavior database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err := database.Exec(context.Background(), `
ALTER TABLE evaluations DROP CONSTRAINT evaluations_source_key
`); err != nil {
		t.Fatalf("allow anomalous duplicate report fixture: %v", err)
	}

	baseDay := time.Now().UTC().Truncate(24 * time.Hour).Add(-10 * 24 * time.Hour)
	now := time.Now().UTC()
	seedUserBehaviorFacts(t, database, baseDay, now)

	assertUserBehaviorDatabaseContract(t, admin, database, schema)
	assertUserBehaviorFacts(t, database, baseDay)
	assertUserBehaviorIndexesUsed(t, database, baseDay)
	if _, err := database.Exec(context.Background(), `
DELETE FROM evaluations
WHERE id = '80000000-0000-4000-8000-000000001100';
ALTER TABLE evaluations
ADD CONSTRAINT evaluations_source_key UNIQUE (user_id, kind, source_id);
`); err != nil {
		t.Fatalf("restore evaluation source uniqueness: %v", err)
	}
	assertUserBehaviorReaderPrivileges(t, admin, database, schema)

	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("user behavior DownOne = %t, %v", changed, downErr)
	}
	assertUserBehaviorObjectCount(t, admin, schema, 0, 0)
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("user behavior second Up = %t, %v", changed, upErr)
	}
	assertUserBehaviorDatabaseContract(t, admin, database, schema)
}

func seedUserBehaviorFacts(
	t *testing.T,
	database *pgx.Conn,
	baseDay time.Time,
	now time.Time,
) {
	t.Helper()
	_, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email, created_at, updated_at) VALUES
('10000000-0000-4000-8000-000000001101', 'behavior-a@example.com', $1::timestamptz + interval '1 hour', $1::timestamptz + interval '1 hour'),
('10000000-0000-4000-8000-000000001102', 'behavior-b@example.com', $1::timestamptz + interval '90 minutes', $1::timestamptz + interval '90 minutes'),
('10000000-0000-4000-8000-000000001103', 'behavior-c@example.com', $2::timestamptz - interval '30 minutes', $2::timestamptz - interval '30 minutes'),
('10000000-0000-4000-8000-000000001104', 'behavior-d@example.com', $1::timestamptz + interval '105 minutes', $1::timestamptz + interval '105 minutes');

INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint,
    created_at, updated_at
) VALUES
(
    '30000000-0000-4000-8000-000000001101',
    '10000000-0000-4000-8000-000000001101',
    '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'INTERVIEW', 'ready', 'behavior-plan-a-request',
    decode(repeat('11', 32), 'hex'), $1::timestamptz + interval '1 hour', $1::timestamptz + interval '1 hour'
),
(
    '30000000-0000-4000-8000-000000001102',
    '10000000-0000-4000-8000-000000001102',
    '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'LEGACY_EXPERIENCE', 'ready', 'behavior-plan-b-request',
    decode(repeat('12', 32), 'hex'), $1::timestamptz + interval '90 minutes', $1::timestamptz + interval '90 minutes'
),
(
    '30000000-0000-4000-8000-000000001103',
    '10000000-0000-4000-8000-000000001103',
    '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'WORKPLACE', 'ready', 'behavior-plan-c-request',
    decode(repeat('13', 32), 'hex'), $2::timestamptz - interval '30 minutes', $2::timestamptz - interval '30 minutes'
);

INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    effective_turns, plan_snapshot, participants, presentation_snapshot,
    initial_client_request_id, initial_request_fingerprint,
    started_at, ended_at, end_reason, created_at, updated_at
) VALUES
(
    '40000000-0000-4000-8000-000000001101',
    '10000000-0000-4000-8000-000000001101',
    '30000000-0000-4000-8000-000000001101', 1,
    'INTERVIEW', 'INTERVIEW_BEHAVIORAL', 'FOCUS', 'interview.v1',
    'completed', 1, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-a1', decode(repeat('21', 32), 'hex'),
    $1::timestamptz + interval '125 minutes', $1::timestamptz + interval '150 minutes', 'completed',
    $1::timestamptz + interval '120 minutes', $1::timestamptz + interval '150 minutes'
),
(
    '40000000-0000-4000-8000-000000001102',
    '10000000-0000-4000-8000-000000001102',
    '30000000-0000-4000-8000-000000001102', 1,
    'LEGACY_EXPERIENCE', 'LEGACY_SCENE', 'LEGACY_MODE', 'legacy.v1',
    'completed', 2, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-b1', decode(repeat('22', 32), 'hex'),
    $1::timestamptz + interval '190 minutes', $1::timestamptz + interval '240 minutes', 'completed',
    $1::timestamptz + interval '180 minutes', $1::timestamptz + interval '240 minutes'
),
(
    '40000000-0000-4000-8000-000000001103',
    '10000000-0000-4000-8000-000000001101',
    '30000000-0000-4000-8000-000000001101', 1,
    'IELTS_SPEAKING', 'IELTS_SPEAKING', 'PART_1', 'ielts.v1',
    'ended_early', 1, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-a2', decode(repeat('23', 32), 'hex'),
    $1::timestamptz + interval '1 day 125 minutes', $1::timestamptz + interval '1 day 140 minutes',
    'user_ended', $1::timestamptz + interval '1 day 120 minutes',
    $1::timestamptz + interval '1 day 140 minutes'
),
(
    '40000000-0000-4000-8000-000000001104',
    '10000000-0000-4000-8000-000000001102',
    '30000000-0000-4000-8000-000000001102', 1,
    'LEGACY_EXPERIENCE', 'LEGACY_SCENE', 'LEGACY_MODE', 'legacy.v1',
    'ended_early', 6, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-b2', decode(repeat('24', 32), 'hex'),
    $1::timestamptz + interval '1 day 185 minutes', $1::timestamptz + interval '1 day 200 minutes',
    'user_ended', $1::timestamptz + interval '1 day 180 minutes',
    $1::timestamptz + interval '1 day 200 minutes'
),
(
    '40000000-0000-4000-8000-000000001105',
    '10000000-0000-4000-8000-000000001101',
    '30000000-0000-4000-8000-000000001101', 1,
    'INTERVIEW', 'INTERVIEW_RECRUITER', 'FULL_SIMULATION', 'interview.v1',
    'starting', 0, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-a-stale', decode(repeat('25', 32), 'hex'),
    NULL, NULL, NULL, $1::timestamptz + interval '2 days', $1::timestamptz + interval '2 days'
),
(
    '40000000-0000-4000-8000-000000001106',
    '10000000-0000-4000-8000-000000001101',
    '30000000-0000-4000-8000-000000001101', 1,
    'LIFE_AND_TRAVEL', 'LIFE_TRAVEL', 'FOCUS', 'life.v1',
    'completed', 1, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-a3', decode(repeat('26', 32), 'hex'),
    $1::timestamptz + interval '7 days 125 minutes', $1::timestamptz + interval '7 days 150 minutes',
    'completed', $1::timestamptz + interval '7 days 120 minutes',
    $1::timestamptz + interval '7 days 150 minutes'
),
(
    '40000000-0000-4000-8000-000000001107',
    '10000000-0000-4000-8000-000000001102',
    '30000000-0000-4000-8000-000000001102', 1,
    'WORKPLACE', 'WORKPLACE_GENERAL', 'FOCUS', 'workplace.v1',
    'in_progress', 0, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-b-recent', decode(repeat('27', 32), 'hex'),
    $2::timestamptz - interval '150 minutes', NULL, NULL,
    $2::timestamptz - interval '3 hours', $2::timestamptz - interval '2 hours'
),
(
    '40000000-0000-4000-8000-000000001108',
    '10000000-0000-4000-8000-000000001102',
    '30000000-0000-4000-8000-000000001102', 1,
    'WORKPLACE', 'WORKPLACE_GENERAL', 'FOCUS', 'workplace.v1',
    'paused', 0, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-b-paused', decode(repeat('28', 32), 'hex'),
    $2::timestamptz - interval '60 hours', NULL, NULL,
    $2::timestamptz - interval '3 days', $2::timestamptz - interval '2 days'
),
(
    '40000000-0000-4000-8000-000000001109',
    '10000000-0000-4000-8000-000000001103',
    '30000000-0000-4000-8000-000000001103', 1,
    'WORKPLACE', 'WORKPLACE_GENERAL', 'FULL_SIMULATION', 'workplace.v1',
    'in_progress', 1, '{}'::jsonb, '[{}]'::jsonb,
    '{"schema_version":1,"avatar":{},"voice":{}}'::jsonb,
    'behavior-session-c1', decode(repeat('29', 32), 'hex'),
    $2::timestamptz - interval '15 minutes', NULL, NULL,
    $2::timestamptz - interval '20 minutes', $2::timestamptz - interval '10 minutes'
);

INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence,
    created_at, updated_at
) VALUES
('50000000-0000-4000-8000-000000001101', '40000000-0000-4000-8000-000000001101', 'objective-a1', 'prompt', 'A1', 'coach', ARRAY['learner'], 1, $1::timestamptz + interval '125 minutes', $1::timestamptz + interval '125 minutes'),
('50000000-0000-4000-8000-000000001102', '40000000-0000-4000-8000-000000001102', 'objective-b1', 'prompt', 'B1', 'coach', ARRAY['learner'], 1, $1::timestamptz + interval '182 minutes', $1::timestamptz + interval '182 minutes'),
('50000000-0000-4000-8000-000000001110', '40000000-0000-4000-8000-000000001102', 'objective-b2', 'prompt', 'B2', 'coach', ARRAY['learner'], 2, $1::timestamptz + interval '195 minutes', $1::timestamptz + interval '195 minutes'),
('50000000-0000-4000-8000-000000001103', '40000000-0000-4000-8000-000000001103', 'objective-a2', 'prompt', 'A2', 'coach', ARRAY['learner'], 1, $1::timestamptz + interval '1 day 125 minutes', $1::timestamptz + interval '1 day 125 minutes'),
('50000000-0000-4000-8000-000000001106', '40000000-0000-4000-8000-000000001106', 'objective-a3', 'prompt', 'A3', 'coach', ARRAY['learner'], 1, $1::timestamptz + interval '7 days 125 minutes', $1::timestamptz + interval '7 days 125 minutes'),
('50000000-0000-4000-8000-000000001109', '40000000-0000-4000-8000-000000001109', 'objective-c1', 'prompt', 'C1', 'coach', ARRAY['learner'], 1, $2::timestamptz - interval '15 minutes', $2::timestamptz - interval '15 minutes');

INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, original_turn_id, client_request_id,
    counts_toward_turn_limit, candidate_id, transcript_id, evidence_version,
    transcript, interaction_mode, confirmed_at, created_at, updated_at
) VALUES
(
    '60000000-0000-4000-8000-000000001101',
    '40000000-0000-4000-8000-000000001101',
    '50000000-0000-4000-8000-000000001101', 'learner', 1,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001101', 'behavior-transcript-a1', 1,
    'private answer a1', 'TEXT', $1::timestamptz + interval '130 minutes',
    $1::timestamptz + interval '126 minutes', $1::timestamptz + interval '130 minutes'
),
(
    '60000000-0000-4000-8000-000000001102',
    '40000000-0000-4000-8000-000000001101',
    '50000000-0000-4000-8000-000000001101', 'learner', 2,
    'RETRY', 'confirmed', '60000000-0000-4000-8000-000000001101',
    'behavior-retry-confirmed', false,
    '70000000-0000-4000-8000-000000001102', 'behavior-transcript-retry', 1,
    'private retry answer', 'PUSH_TO_TALK', $1::timestamptz + interval '140 minutes',
    $1::timestamptz + interval '135 minutes', $1::timestamptz + interval '140 minutes'
),
(
    '60000000-0000-4000-8000-000000001103',
    '40000000-0000-4000-8000-000000001101',
    '50000000-0000-4000-8000-000000001101', 'learner', 3,
    'RETRY', 'failed', '60000000-0000-4000-8000-000000001101',
    'behavior-retry-failed', false, NULL, NULL, NULL, NULL, NULL, NULL,
    $1::timestamptz + interval '141 minutes', $1::timestamptz + interval '142 minutes'
),
(
    '60000000-0000-4000-8000-000000001104',
    '40000000-0000-4000-8000-000000001101',
    '50000000-0000-4000-8000-000000001101', 'learner', 4,
    'RETRY', 'answering', '60000000-0000-4000-8000-000000001101',
    'behavior-retry-stale', false, NULL, NULL, NULL, NULL, NULL, NULL,
    $1::timestamptz + interval '143 minutes', $2::timestamptz - interval '2 days'
),
(
    '60000000-0000-4000-8000-000000001105',
    '40000000-0000-4000-8000-000000001102',
    '50000000-0000-4000-8000-000000001102', 'learner', 1,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001105', 'behavior-transcript-b1', 1,
    'private invalid-time answer', NULL, $1::timestamptz + interval '185 minutes',
    $1::timestamptz + interval '182 minutes', $1::timestamptz + interval '185 minutes'
),
(
    '60000000-0000-4000-8000-000000001110',
    '40000000-0000-4000-8000-000000001102',
    '50000000-0000-4000-8000-000000001110', 'learner', 2,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001110', 'behavior-transcript-b2', 1,
    'private valid-time answer', NULL, $1::timestamptz + interval '200 minutes',
    $1::timestamptz + interval '195 minutes', $1::timestamptz + interval '200 minutes'
),
(
    '60000000-0000-4000-8000-000000001106',
    '40000000-0000-4000-8000-000000001103',
    '50000000-0000-4000-8000-000000001103', 'learner', 1,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001106', 'behavior-transcript-a2', 1,
    'private answer a2', 'TEXT', $1::timestamptz + interval '1 day 130 minutes',
    $1::timestamptz + interval '1 day 126 minutes', $1::timestamptz + interval '1 day 130 minutes'
),
(
    '60000000-0000-4000-8000-000000001107',
    '40000000-0000-4000-8000-000000001106',
    '50000000-0000-4000-8000-000000001106', 'learner', 1,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001107', 'behavior-transcript-a3', 1,
    'private answer a3', 'PUSH_TO_TALK', $1::timestamptz + interval '7 days 130 minutes',
    $1::timestamptz + interval '7 days 126 minutes', $1::timestamptz + interval '7 days 130 minutes'
),
(
    '60000000-0000-4000-8000-000000001109',
    '40000000-0000-4000-8000-000000001109',
    '50000000-0000-4000-8000-000000001109', 'learner', 1,
    'EFFECTIVE', 'confirmed', NULL, NULL, true,
    '70000000-0000-4000-8000-000000001109', 'behavior-transcript-c1', 1,
    'private answer c1', 'TEXT', $2::timestamptz - interval '10 minutes',
    $2::timestamptz - interval '12 minutes', $2::timestamptz - interval '10 minutes'
);

INSERT INTO evaluations (
    id, user_id, kind, source_id, context_id, status, input_snapshot, input_hash,
    config_lineage, config_hash, result, attempt_count,
    created_at, updated_at, started_at, finished_at
) VALUES
(
    '80000000-0000-4000-8000-000000001100',
    '10000000-0000-4000-8000-000000001101', 'SESSION_REPORT',
    '40000000-0000-4000-8000-000000001101',
    '40000000-0000-4000-8000-000000001101', 'READY', '{}'::json,
    decode(repeat('33', 32), 'hex'), '{}'::json,
    decode(repeat('34', 32), 'hex'), '{}'::jsonb, 1,
    $1::timestamptz + interval '130 minutes', $1::timestamptz + interval '140 minutes',
    $1::timestamptz + interval '131 minutes', $1::timestamptz + interval '140 minutes'
),
(
    '80000000-0000-4000-8000-000000001101',
    '10000000-0000-4000-8000-000000001101', 'SESSION_REPORT',
    '40000000-0000-4000-8000-000000001101',
    '40000000-0000-4000-8000-000000001101', 'READY', '{}'::json,
    decode(repeat('31', 32), 'hex'), '{}'::json,
    decode(repeat('32', 32), 'hex'), '{}'::jsonb, 1,
    $1::timestamptz + interval '151 minutes', $1::timestamptz + interval '160 minutes',
    $1::timestamptz + interval '152 minutes', $1::timestamptz + interval '160 minutes'
);
`, baseDay, now)
	if err != nil {
		t.Fatalf("seed user behavior facts: %v", err)
	}
}

func assertUserBehaviorDatabaseContract(
	t *testing.T,
	admin *pgx.Conn,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	rows, err := admin.Query(context.Background(), `
SELECT table_name
FROM information_schema.views
WHERE table_schema = $1 AND table_name LIKE 'user_behavior_%'
ORDER BY table_name
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	views, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(views) != fmt.Sprint(userBehaviorViews) {
		t.Fatalf("user behavior views = %v, want %v", views, userBehaviorViews)
	}

	rows, err = admin.Query(context.Background(), `
SELECT indexname
FROM pg_indexes
WHERE schemaname = $1 AND indexname LIKE 'user_behavior_%'
ORDER BY indexname
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(indexes) != fmt.Sprint(userBehaviorIndexes) {
		t.Fatalf("user behavior indexes = %v, want %v", indexes, userBehaviorIndexes)
	}

	var insecureViews int
	if err := admin.QueryRow(context.Background(), `
SELECT count(*)
FROM pg_class AS relation
JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = $1
  AND relation.relname LIKE 'user_behavior_%'
  AND relation.relkind = 'v'
  AND NOT ('security_barrier=true' = ANY(relation.reloptions))
`, schema).Scan(&insecureViews); err != nil {
		t.Fatal(err)
	}
	if insecureViews != 0 {
		t.Fatalf("user behavior views without security_barrier = %d", insecureViews)
	}

	rows, err = admin.Query(context.Background(), `
SELECT table_name, column_name, data_type
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name LIKE 'user_behavior_%'
  AND (
      data_type = 'uuid'
      OR column_name ~ '(^|_)(user|account|session|turn|evaluation)_?id($|_)'
      OR column_name ~ '(email|name|content|transcript|audio|prompt|feedback)'
  )
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	unsafeColumns, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		Table string
		Name  string
		Type  string
	}])
	if err != nil {
		t.Fatal(err)
	}
	if len(unsafeColumns) != 0 {
		t.Fatalf("unsafe user behavior columns = %#v", unsafeColumns)
	}

	var publicGrants int
	if err := admin.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.role_table_grants
WHERE table_schema = $1
  AND table_name LIKE 'user_behavior_%'
  AND grantee = 'PUBLIC'
`, schema).Scan(&publicGrants); err != nil {
		t.Fatal(err)
	}
	if publicGrants != 0 {
		t.Fatalf("PUBLIC user behavior grants = %d", publicGrants)
	}

	if _, err := database.Exec(context.Background(), "SET TIME ZONE 'Asia/Shanghai'"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.Exec(context.Background(), "SET TIME ZONE 'UTC'") }()
	var day time.Time
	if err := database.QueryRow(context.Background(), `
SELECT min(day_utc) FROM user_behavior_daily_session_funnel
`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if day.UTC().Hour() != 0 {
		t.Fatalf("UTC bucket = %s", day)
	}
}

func assertUserBehaviorFacts(t *testing.T, database *pgx.Conn, baseDay time.Time) {
	t.Helper()
	var created, started, first, completed, ready, endedEarly int64
	var startedRate, firstRate, completedRate, readyRate, earlyRate float64
	if err := database.QueryRow(context.Background(), `
SELECT created_sessions, started_sessions, first_effective_confirmed_sessions,
       completed_sessions, ready_session_report_sessions, ended_early_sessions,
       started_from_created_rate, first_effective_from_started_rate,
       completed_from_first_effective_rate, ready_report_from_completed_rate,
       ended_early_from_started_rate
FROM user_behavior_daily_session_funnel
WHERE day_utc = $1
`, baseDay).Scan(&created, &started, &first, &completed, &ready, &endedEarly,
		&startedRate, &firstRate, &completedRate, &readyRate, &earlyRate); err != nil {
		t.Fatal(err)
	}
	if created != 2 || started != 2 || first != 2 || completed != 2 ||
		ready != 1 || endedEarly != 0 || startedRate != 1 || firstRate != 1 ||
		completedRate != 1 || readyRate != 0.5 || earlyRate != 0 {
		t.Fatalf("funnel = %d %d %d %d %d %d %.2f %.2f %.2f %.2f %.2f",
			created, started, first, completed, ready, endedEarly, startedRate,
			firstRate, completedRate, readyRate, earlyRate)
	}

	var firstFromStarted, completedFromFirst, reportFromCompleted, earlyFromStarted *float64
	if err := database.QueryRow(context.Background(), `
SELECT first_effective_from_started_rate, completed_from_first_effective_rate,
       ready_report_from_completed_rate, ended_early_from_started_rate
FROM user_behavior_daily_session_funnel
WHERE day_utc = $1::timestamptz + interval '2 days'
`, baseDay).Scan(&firstFromStarted, &completedFromFirst, &reportFromCompleted,
		&earlyFromStarted); err != nil {
		t.Fatal(err)
	}
	if firstFromStarted != nil || completedFromFirst != nil ||
		reportFromCompleted != nil || earlyFromStarted != nil {
		t.Fatalf("zero-denominator funnel rates = %v %v %v %v",
			firstFromStarted, completedFromFirst, reportFromCompleted, earlyFromStarted)
	}

	assertTTFV := func(scope string, eligible, reached int64, rate, p50, p95 float64) {
		t.Helper()
		var gotEligible, gotReached int64
		var gotRate, gotP50, gotP95 float64
		if err := database.QueryRow(context.Background(), `
SELECT eligible_subjects, reached_subjects, reach_rate, p50_seconds, p95_seconds
FROM user_behavior_daily_time_to_first_effective_turn
WHERE day_utc = $1 AND scope = $2
`, baseDay, scope).Scan(&gotEligible, &gotReached, &gotRate, &gotP50, &gotP95); err != nil {
			t.Fatal(err)
		}
		if gotEligible != eligible || gotReached != reached ||
			math.Abs(gotRate-rate) > 1e-12 || gotP50 != p50 || gotP95 != p95 {
			t.Fatalf("%s TTFV = %d %d %.4f %.2f %.2f",
				scope, gotEligible, gotReached, gotRate, gotP50, gotP95)
		}
	}
	assertTTFV("ACCOUNT", 3, 2, 2.0/3.0, 5400, 6480)
	assertTTFV("SESSION", 2, 2, 1, 900, 1170)

	var cohortUsers int64
	var d1Users, d7Users *int64
	var d1Rate, d7Rate *float64
	if err := database.QueryRow(context.Background(), `
SELECT cohort_users, d1_returned_users, d1_retention_rate,
       d7_returned_users, d7_retention_rate
FROM user_behavior_daily_retention
WHERE cohort_day_utc = $1
`, baseDay).Scan(&cohortUsers, &d1Users, &d1Rate, &d7Users, &d7Rate); err != nil {
		t.Fatal(err)
	}
	if cohortUsers != 2 || d1Users == nil || *d1Users != 1 ||
		d1Rate == nil || *d1Rate != 0.5 || d7Users == nil || *d7Users != 1 ||
		d7Rate == nil || *d7Rate != 0.5 {
		t.Fatalf("mature retention = %d %v %v %v %v",
			cohortUsers, d1Users, d1Rate, d7Users, d7Rate)
	}
	if err := database.QueryRow(context.Background(), `
SELECT d1_returned_users, d1_retention_rate, d7_returned_users, d7_retention_rate
FROM user_behavior_daily_retention
ORDER BY cohort_day_utc DESC
LIMIT 1
`).Scan(&d1Users, &d1Rate, &d7Users, &d7Rate); err != nil {
		t.Fatal(err)
	}
	if d1Users != nil || d1Rate != nil || d7Users != nil || d7Rate != nil {
		t.Fatalf("immature retention = %v %v %v %v", d1Users, d1Rate, d7Users, d7Rate)
	}

	var retryCreated, retryConfirmed, retryFailed, retryStale int64
	var retryConfirmedRate, retryFailedRate, retryStaleRate float64
	if err := database.QueryRow(context.Background(), `
SELECT created_retry_turns, confirmed_retry_turns, failed_retry_turns,
       stale_retry_turns, confirmation_rate, failure_rate, stale_rate
FROM user_behavior_daily_repractice
WHERE day_utc = $1
`, baseDay).Scan(&retryCreated, &retryConfirmed, &retryFailed, &retryStale,
		&retryConfirmedRate, &retryFailedRate, &retryStaleRate); err != nil {
		t.Fatal(err)
	}
	if retryCreated != 3 || retryConfirmed != 1 || retryFailed != 1 || retryStale != 1 ||
		math.Abs(retryConfirmedRate-1.0/3.0) > 1e-12 ||
		math.Abs(retryFailedRate-1.0/3.0) > 1e-12 ||
		math.Abs(retryStaleRate-1.0/3.0) > 1e-12 {
		t.Fatalf("repractice = %d %d %d %d %.4f %.4f %.4f",
			retryCreated, retryConfirmed, retryFailed, retryStale,
			retryConfirmedRate, retryFailedRate, retryStaleRate)
	}

	rows, err := database.Query(context.Background(), `
SELECT effective_turn_depth, ended_early_sessions, ended_early_share
FROM user_behavior_daily_early_end
WHERE day_utc = $1::timestamptz + interval '1 day'
ORDER BY effective_turn_depth
`, baseDay)
	if err != nil {
		t.Fatal(err)
	}
	type earlyEnd struct {
		Depth    string
		Sessions int64
		Share    float64
	}
	earlyEnds, err := pgx.CollectRows(rows, pgx.RowToStructByPos[earlyEnd])
	if err != nil {
		t.Fatal(err)
	}
	wantEarlyEnds := []earlyEnd{{"1", 1, 0.5}, {"5_PLUS", 1, 0.5}}
	if fmt.Sprint(earlyEnds) != fmt.Sprint(wantEarlyEnds) {
		t.Fatalf("early ends = %#v, want %#v", earlyEnds, wantEarlyEnds)
	}

	rows, err = database.Query(context.Background(), `
SELECT session_status, staleness_bucket, sessions
FROM user_behavior_current_nonterminal_sessions
ORDER BY session_status, staleness_bucket
`)
	if err != nil {
		t.Fatal(err)
	}
	type nonterminal struct {
		Status   string
		Bucket   string
		Sessions int64
	}
	nonterminals, err := pgx.CollectRows(rows, pgx.RowToStructByPos[nonterminal])
	if err != nil {
		t.Fatal(err)
	}
	wantNonterminals := []nonterminal{
		{"IN_PROGRESS", "1H_TO_24H", 1},
		{"IN_PROGRESS", "UNDER_1H", 1},
		{"PAUSED", "1D_TO_7D", 1},
		{"STARTING", "7D_PLUS", 1},
	}
	if fmt.Sprint(nonterminals) != fmt.Sprint(wantNonterminals) {
		t.Fatalf("nonterminal sessions = %#v, want %#v", nonterminals, wantNonterminals)
	}

	var activeUsers, sessions, turns int64
	if err := database.QueryRow(context.Background(), `
SELECT active_users, sessions, confirmed_turns
FROM user_behavior_daily_feature_usage
WHERE day_utc = $1
  AND feature_kind = 'PRACTICE_EXPERIENCE'
  AND feature_value = 'INTERVIEW'
`, baseDay).Scan(&activeUsers, &sessions, &turns); err != nil {
		t.Fatal(err)
	}
	if activeUsers != 1 || sessions != 1 || turns != 2 {
		t.Fatalf("known feature usage = %d %d %d", activeUsers, sessions, turns)
	}
	rows, err = database.Query(context.Background(), `
SELECT feature_kind, active_users, sessions, confirmed_turns
FROM user_behavior_daily_feature_usage
WHERE day_utc = $1 AND feature_value = 'UNKNOWN'
ORDER BY feature_kind
`, baseDay)
	if err != nil {
		t.Fatal(err)
	}
	type unknownFeature struct {
		Kind        string
		ActiveUsers int64
		Sessions    int64
		Turns       int64
	}
	unknownFeatures, err := pgx.CollectRows(rows, pgx.RowToStructByPos[unknownFeature])
	if err != nil {
		t.Fatal(err)
	}
	if len(unknownFeatures) != 4 {
		t.Fatalf("unknown feature rows = %#v", unknownFeatures)
	}
	for _, feature := range unknownFeatures {
		if feature.ActiveUsers != 1 || feature.Sessions != 1 || feature.Turns != 2 {
			t.Fatalf("unknown feature = %#v", feature)
		}
	}
}

func assertUserBehaviorReaderPrivileges(
	t *testing.T,
	admin *pgx.Conn,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	role := fmt.Sprintf("user_behavior_reader_test_%d", time.Now().UnixNano())
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
	for _, view := range userBehaviorViews {
		qualified := pgx.Identifier{schema, view}.Sanitize()
		if _, err := admin.Exec(context.Background(),
			"GRANT SELECT ON "+qualified+" TO "+roleIdentifier); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := database.Exec(context.Background(), "SET ROLE "+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.Exec(context.Background(), "RESET ROLE") }()
	qualifiedView := pgx.Identifier{schema, userBehaviorViews[0]}.Sanitize()
	if _, err := database.Exec(context.Background(),
		"SELECT count(*) FROM "+qualifiedView); err != nil {
		t.Fatalf("reader SELECT aggregate view: %v", err)
	}
	qualifiedTable := pgx.Identifier{schema, "practice_sessions"}.Sanitize()
	_, rawErr := database.Exec(context.Background(),
		"SELECT count(*) FROM "+qualifiedTable)
	if rawErr == nil || !strings.Contains(rawErr.Error(), "permission denied") {
		t.Fatalf("reader raw SELECT error = %v", rawErr)
	}
}

func assertUserBehaviorIndexesUsed(
	t *testing.T,
	database *pgx.Conn,
	baseDay time.Time,
) {
	t.Helper()
	if _, err := database.Exec(context.Background(), "SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.Exec(context.Background(), "RESET enable_seqscan") }()

	assertPlanUses := func(index, query string, arguments ...any) {
		t.Helper()
		rows, err := database.Query(
			context.Background(),
			"EXPLAIN (COSTS OFF) "+query,
			arguments...,
		)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if joined := strings.Join(plan, "\n"); !strings.Contains(joined, index) {
			t.Fatalf("plan does not use %s:\n%s", index, joined)
		}
	}

	assertPlanUses(
		"user_behavior_sessions_created_day_idx",
		`SELECT * FROM user_behavior_daily_session_funnel
         WHERE day_utc = $1`,
		baseDay,
	)
	assertPlanUses(
		"user_behavior_confirmed_turns_day_idx",
		`SELECT count(*) FROM practice_turns
         WHERE status = 'confirmed'
           AND date_trunc('day', confirmed_at, 'UTC') = $1`,
		baseDay,
	)
	assertPlanUses(
		"user_behavior_ready_session_reports_idx",
		`SELECT * FROM user_behavior_daily_session_funnel
         WHERE day_utc = $1`,
		baseDay,
	)
	assertPlanUses(
		"user_behavior_nonterminal_sessions_updated_idx",
		`SELECT * FROM user_behavior_current_nonterminal_sessions
         WHERE staleness_bucket = '7D_PLUS'`,
	)
}

func assertUserBehaviorObjectCount(
	t *testing.T,
	database *pgx.Conn,
	schema string,
	wantViews int,
	wantIndexes int,
) {
	t.Helper()
	var views, indexes int
	if err := database.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.views
WHERE table_schema = $1 AND table_name LIKE 'user_behavior_%'
`, schema).Scan(&views); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(context.Background(), `
SELECT count(*)
FROM pg_indexes
WHERE schemaname = $1 AND indexname LIKE 'user_behavior_%'
`, schema).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if views != wantViews || indexes != wantIndexes {
		t.Fatalf("user behavior objects = %d views, %d indexes; want %d, %d",
			views, indexes, wantViews, wantIndexes)
	}
}
