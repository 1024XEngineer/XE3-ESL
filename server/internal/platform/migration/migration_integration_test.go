package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var cleanBaselineTables = []string{
	"agent_message_attachments",
	"agent_messages",
	"agent_runs",
	"agent_threads",
	"agent_voice_drafts",
	"auth_sessions",
	"coaching_user_profiles",
	"credentials",
	"evaluation_feedback_items",
	"evaluations",
	"interview_preparations",
	"media_assets",
	"practice_plans",
	"practice_questions",
	"practice_sessions",
	"practice_turns",
	"users",
}

func TestMigrationHistoryFreshUpDownUp(t *testing.T) {
	config, admin, schema := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	changed, err := runner.Up()
	if err != nil || !changed {
		t.Fatalf("fresh Up = %t, %v", changed, err)
	}
	assertCleanBaselineSchema(t, admin, schema)

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to archive migration = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables))

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to Agent domain completion = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables))

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to baseline = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, len(cleanBaselineTables))

	changed, err = runner.DownOne()
	if err != nil || !changed {
		t.Fatalf("DownOne to empty = %t, %v", changed, err)
	}
	assertApplicationTableCount(t, admin, schema, 0)

	changed, err = runner.Up()
	if err != nil || !changed {
		t.Fatalf("second Up = %t, %v", changed, err)
	}
	assertCleanBaselineSchema(t, admin, schema)
}

func TestSceneSelectionSourceMigrationTransformsPlansAndPreservesSessions(
	t *testing.T,
) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("initial Up = %t, %v", changed, upErr)
	}
	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("DownOne to v3 = %t, %v", changed, downErr)
	}

	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect migration data database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	const (
		userID       = "10000000-0000-4000-8000-000000000011"
		planID       = "30000000-0000-4000-8000-000000000011"
		sessionID    = "40000000-0000-4000-8000-000000000011"
		oldSelection = `{
  "scene": {
    "scene_id": "scn_daily_small_talk",
    "scene_version": 2,
    "status": "active",
    "practice_experience": "LIFE_AND_TRAVEL",
    "scene_category": "LIFE_DAILY",
    "name": "日常寒暄与自我介绍",
    "prompt": {},
    "roles": [{"role_definition_id":"conversation_partner","scene_id":"scn_daily_small_talk"}],
    "practice_options": [{"practice_option_id":"option_daily_small_talk_full","scene_id":"scn_daily_small_talk"}]
  },
  "selected_role_ids": ["conversation_partner"],
  "practice_option_id": "option_daily_small_talk_full"
}`
	)
	if _, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email) VALUES ($1, 'migration@example.com')
`, userID); err != nil {
		t.Fatalf("seed migration owner: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $2, $1, '{}'::jsonb, $3::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'LIFE_AND_TRAVEL', 'ready', 'request-migration-plan',
    decode(repeat('11', 32), 'hex')
)
`, userID, planID, oldSelection); err != nil {
		t.Fatalf("seed v3 Practice Plan: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    plan_snapshot, participants, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    $3, $1, $2, 1, 'LIFE_AND_TRAVEL', 'LIFE_DAILY', 'FULL_SIMULATION',
    'daily.general.evaluation.v1', 'starting',
    jsonb_build_object('scene_selection', $4::jsonb), '[{}]'::jsonb,
    'request-migration-session', decode(repeat('12', 32), 'hex')
)
`, userID, planID, sessionID, oldSelection); err != nil {
		t.Fatalf("seed v3 Practice Session: %v", err)
	}

	if changed, upErr := runner.Up(); upErr != nil || !changed {
		t.Fatalf("apply v4 = %t, %v", changed, upErr)
	}
	assertMigratedSceneSelection := func(query string, id string) {
		t.Helper()
		var sourceType, sourceID, sceneKey, roleKey, optionKey string
		var sourceVersion, sceneRevision int
		if err := database.QueryRow(context.Background(), query, id).Scan(
			&sourceType,
			&sourceID,
			&sourceVersion,
			&sceneKey,
			&sceneRevision,
			&roleKey,
			&optionKey,
		); err != nil {
			t.Fatalf("read migrated snapshot: %v", err)
		}
		if sourceType != "CATALOG" || sourceID != "scn_daily_small_talk" ||
			sourceVersion != 2 || sceneKey != sourceID || sceneRevision != 2 ||
			roleKey != sceneKey || optionKey != sceneKey {
			t.Fatalf("migrated snapshot = %q %q %d %q %d %q %q", sourceType, sourceID, sourceVersion, sceneKey, sceneRevision, roleKey, optionKey)
		}
	}
	assertMigratedSceneSelection(`
SELECT scene_selection #>> '{source,type}',
       scene_selection #>> '{source,scene_id}',
       (scene_selection #>> '{source,scene_version}')::integer,
       scene_selection #>> '{scene,scene_key}',
       (scene_selection #>> '{scene,scene_revision}')::integer,
       scene_selection #>> '{scene,roles,0,scene_key}',
       scene_selection #>> '{scene,practice_options,0,scene_key}'
FROM practice_plans WHERE plan_id = $1
`, planID)
	var sessionSelectionUnchanged bool
	if err := database.QueryRow(context.Background(), `
SELECT plan_snapshot->'scene_selection' = $2::jsonb
FROM practice_sessions WHERE session_id = $1
`, sessionID, oldSelection).Scan(&sessionSelectionUnchanged); err != nil {
		t.Fatalf("read preserved Practice Session snapshot: %v", err)
	}
	if !sessionSelectionUnchanged {
		t.Fatal("Practice Session execution snapshot changed during Plan migration")
	}

	if changed, downErr := runner.DownOne(); downErr != nil || !changed {
		t.Fatalf("roll back v4 = %t, %v", changed, downErr)
	}
	var restoredID, restoredRoleID, restoredOptionID, restoredStatus string
	var restoredVersion int
	if err := database.QueryRow(context.Background(), `
SELECT scene_selection #>> '{scene,scene_id}',
       (scene_selection #>> '{scene,scene_version}')::integer,
       scene_selection #>> '{scene,status}',
       scene_selection #>> '{scene,roles,0,scene_id}',
       scene_selection #>> '{scene,practice_options,0,scene_id}'
FROM practice_plans WHERE plan_id = $1
`, planID).Scan(&restoredID, &restoredVersion, &restoredStatus, &restoredRoleID, &restoredOptionID); err != nil {
		t.Fatalf("read restored v3 snapshot: %v", err)
	}
	if restoredID != "scn_daily_small_talk" || restoredVersion != 2 ||
		restoredStatus != "active" || restoredRoleID != restoredID ||
		restoredOptionID != restoredID {
		t.Fatalf("restored snapshot = %q %d %q %q %q", restoredID, restoredVersion, restoredStatus, restoredRoleID, restoredOptionID)
	}
}

func TestCleanBaselineOwnershipStateAndPartialUniqueness(t *testing.T) {
	config, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(config)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if changed, err := runner.Up(); err != nil || !changed {
		t.Fatalf("Up = %t, %v", changed, err)
	}

	database, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect baseline: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })

	const (
		userA     = "10000000-0000-4000-8000-000000000001"
		userB     = "10000000-0000-4000-8000-000000000002"
		threadA   = "20000000-0000-4000-8000-000000000001"
		planA     = "30000000-0000-4000-8000-000000000001"
		sessionA  = "40000000-0000-4000-8000-000000000001"
		questionA = "50000000-0000-4000-8000-000000000001"
		questionB = "50000000-0000-4000-8000-000000000002"
	)
	if _, err := database.Exec(context.Background(), `
INSERT INTO users (id, canonical_email)
VALUES ($1, 'a@example.com'), ($2, 'b@example.com')
`, userA, userB); err != nil {
		t.Fatalf("seed owners: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO agent_threads (id, user_id) VALUES ($1, $2)
`, threadA, userA); err != nil {
		t.Fatalf("seed Agent Thread: %v", err)
	}

	_, err = database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, source_thread_id, preparation_snapshot,
    scene_selection, session_policy, practice_objectives,
    practice_experience, status, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    gen_random_uuid(), $1, $2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
    '[{}]'::jsonb, 'conversation', 'draft', 'request-bad-owner',
    decode(repeat('00', 32), 'hex')
)`, userB, threadA)
	expectPostgresCode(t, err, "23503")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_plans (
    plan_id, user_id, preparation_snapshot, scene_selection, session_policy,
    practice_objectives, practice_experience, status,
    initial_client_request_id, initial_request_fingerprint
) VALUES (
    $1, $2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '[{}]'::jsonb,
    'conversation', 'ready', 'request-plan-a',
    decode(repeat('01', 32), 'hex')
)
`, planA, userA); err != nil {
		t.Fatalf("seed Practice Plan: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_sessions (
    session_id, user_id, plan_id, plan_version, practice_experience,
    scene_category, practice_mode, evaluation_policy_ref, status,
    plan_snapshot, participants, initial_client_request_id,
    initial_request_fingerprint
) VALUES (
    $1, $2, $3, 1, 'conversation', 'general', 'voice',
    'general.evaluation.v1', 'starting', '{}'::jsonb, '[{}]'::jsonb,
    'request-session-a', decode(repeat('02', 32), 'hex')
)
`, sessionA, userA, planA); err != nil {
		t.Fatalf("seed Practice Session: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence
) VALUES (
    $1, $2, 'objective-a', 'prompt', 'Tell me about yourself.',
    'coach', ARRAY['learner'], 1
)
`, questionA, sessionA); err != nil {
		t.Fatalf("seed Practice Question: %v", err)
	}
	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_questions (
    question_id, session_id, objective_id, question_type, content,
    speaker_participant_id, addressee_participant_ids, sequence,
    parent_question_id
) VALUES (
    $1, $2, 'objective-a', 'FOLLOW_UP', 'What trade-off did you make?',
    'coach', ARRAY['learner'], 2, $3
)
`, questionB, sessionA, questionA); err != nil {
		t.Fatalf("seed follow-up Practice Question: %v", err)
	}

	_, err = database.Exec(context.Background(), `
UPDATE practice_sessions
SET status = 'completed', updated_at = CURRENT_TIMESTAMP
WHERE session_id = $1
`, sessionA)
	expectPostgresCode(t, err, "23514")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 1, 'EFFECTIVE', 'confirmed',
    true, gen_random_uuid(), 'transcript-a', 1, 'First answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionA); err != nil {
		t.Fatalf("seed confirmed effective Turn: %v", err)
	}
	_, err = database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 2, 'EFFECTIVE', 'confirmed',
    true, gen_random_uuid(), 'transcript-b', 1, 'Second answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionA)
	expectPostgresCode(t, err, "23505")

	if _, err := database.Exec(context.Background(), `
INSERT INTO practice_turns (
    turn_id, session_id, question_id, respondent_participant_id, sequence,
    turn_kind, status, counts_toward_turn_limit, candidate_id,
    transcript_id, evidence_version, transcript, confirmed_at
) VALUES (
    gen_random_uuid(), $1, $2, 'learner', 3, 'EFFECTIVE', 'confirmed',
    false, gen_random_uuid(), 'transcript-follow-up', 1, 'Follow-up answer',
    CURRENT_TIMESTAMP
)`, sessionA, questionB); err != nil {
		t.Fatalf("seed non-counting effective follow-up Turn: %v", err)
	}

	var evaluationID string
	if err := database.QueryRow(context.Background(), `
INSERT INTO evaluations (
    user_id, kind, source_id, context_id, input_snapshot, input_hash,
    config_lineage, config_hash
) VALUES (
    $1, 'SESSION_REPORT', gen_random_uuid(), $2, '{}'::json,
    decode(repeat('03', 32), 'hex'), '{}'::json,
    decode(repeat('04', 32), 'hex')
) RETURNING id::text
`, userA, sessionA).Scan(&evaluationID); err != nil || evaluationID == "" {
		t.Fatalf("built-in gen_random_uuid = %q, %v", evaluationID, err)
	}
}

func assertCleanBaselineSchema(
	t *testing.T,
	database *pgx.Conn,
	schema string,
) {
	t.Helper()
	rows, err := database.Query(context.Background(), `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = $1
  AND table_type = 'BASE TABLE'
  AND table_name <> 'schema_migrations'
ORDER BY table_name
`, schema)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tables, cleanBaselineTables) {
		t.Fatalf("application tables = %v, want %v", tables, cleanBaselineTables)
	}

	var functions, triggers, extensions, customTypes int
	if err := database.QueryRow(context.Background(), `
SELECT
    (SELECT count(*) FROM pg_proc p
     JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = $1),
    (SELECT count(*) FROM pg_trigger t
     JOIN pg_class c ON c.oid = t.tgrelid
     JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = $1 AND NOT t.tgisinternal),
    (SELECT count(*) FROM pg_extension e
     JOIN pg_namespace n ON n.oid = e.extnamespace
     WHERE n.nspname = $1),
    (SELECT count(*) FROM pg_type ty
     JOIN pg_namespace n ON n.oid = ty.typnamespace
     WHERE n.nspname = $1 AND ty.typtype IN ('d', 'e'))
`, schema).Scan(&functions, &triggers, &extensions, &customTypes); err != nil {
		t.Fatal(err)
	}
	if functions != 0 || triggers != 0 || extensions != 0 || customTypes != 0 {
		t.Fatalf(
			"database programming objects = functions:%d triggers:%d extensions:%d custom types:%d",
			functions, triggers, extensions, customTypes,
		)
	}
}

func assertApplicationTableCount(
	t *testing.T,
	database *pgx.Conn,
	schema string,
	want int,
) {
	t.Helper()
	var count int
	if err := database.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.tables
WHERE table_schema = $1
  AND table_type = 'BASE TABLE'
  AND table_name <> 'schema_migrations'
`, schema).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("application table count = %d, want %d", count, want)
	}
}

func expectPostgresCode(t *testing.T, err error, code string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("PostgreSQL error = %v, want SQLSTATE %s", err, code)
	}
}

func isolatedMigrationConfig(
	t *testing.T,
) (*pgx.ConnConfig, *pgx.Conn, string) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnectTimeout = ConnectTimeout

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		dropContext, dropCancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer dropCancel()
		if _, err := admin.Exec(
			dropContext, "DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	migrationConfig := config.Copy()
	if migrationConfig.RuntimeParams == nil {
		migrationConfig.RuntimeParams = make(map[string]string)
	}
	migrationConfig.RuntimeParams["search_path"] = schema
	return migrationConfig, admin, schema
}
