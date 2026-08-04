package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	builtinSceneCount       = 31
	builtinSceneCatalogHash = "ef292c986b26cfa10ad8094cf7ffe5a17c87719662ce780caea7f224458b6531"
)

func TestSceneAuthorityMigrationSeedsImmutableBuiltinCatalog(t *testing.T) {
	migrationConfig, admin, schema := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	if err := runner.migrate.Steps(49); err != nil {
		t.Fatalf("apply migrations through version 49: %v", err)
	}
	assertMigrationStatus(t, runner, 49)
	assertSceneAuthoritySchema(t, admin, schema, false)

	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to version 49 schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close Scene authority connection: %v", err)
		}
	})

	if err := runner.migrate.Steps(1); err != nil {
		t.Fatalf("apply Scene authority migration: %v", err)
	}
	assertMigrationStatus(t, runner, 50)
	assertSceneAuthoritySchema(t, admin, schema, true)
	assertBuiltinSceneCatalog(t, database)
	assertSceneVersionsAreAppendOnly(t, database)

	changed, err := runner.DownOne()
	if err != nil {
		t.Fatalf("revert Scene authority migration: %v", err)
	}
	if !changed {
		t.Fatal("Scene authority down migration reported no change")
	}
	assertMigrationStatus(t, runner, 49)
	assertSceneAuthoritySchema(t, admin, schema, false)

	if err := runner.migrate.Steps(1); err != nil {
		t.Fatalf("reapply Scene authority migration: %v", err)
	}
	assertMigrationStatus(t, runner, 50)
	assertSceneAuthoritySchema(t, admin, schema, true)
	assertBuiltinSceneCatalog(t, database)
}

func TestSceneAuthorityDownRejectsAdditionalVersions(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})

	if err := runner.migrate.Steps(50); err != nil {
		t.Fatalf("apply migrations through version 50: %v", err)
	}
	assertMigrationStatus(t, runner, 50)

	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to migrated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close Scene authority connection: %v", err)
		}
	})

	if _, err := database.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
		    scene_id,
		    scene_version,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		)
		SELECT
		    scene_id,
		    scene_version + 100,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_programmer_interview'
	`); err != nil {
		t.Fatalf("insert later Scene version: %v", err)
	}

	changed, err := runner.DownOne()
	if err == nil || changed {
		t.Fatalf(
			"Scene authority down with an additional version = changed %t, error %v",
			changed,
			err,
		)
	}
	if !strings.Contains(
		err.Error(),
		"only removes the fixed builtin catalog",
	) {
		t.Fatalf("Scene authority guarded down error = %q", err)
	}
}

func TestSceneAuthorityMigrationRejectsInvalidRoleObjectives(t *testing.T) {
	migrationConfig, _, _ := isolatedMigrationConfig(t)
	runner, err := openConfig(migrationConfig)
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if err := runner.migrate.Steps(50); err != nil {
		t.Fatalf("apply migrations through version 50: %v", err)
	}
	database, err := pgx.ConnectConfig(context.Background(), migrationConfig)
	if err != nil {
		t.Fatalf("connect to Scene authority schema: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close Scene authority connection: %v", err)
		}
	})

	var rolesDocument []byte
	if err := database.QueryRow(context.Background(), `
		SELECT roles
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_programmer_interview'
		  AND scene_version = 1
	`).Scan(&rolesDocument); err != nil {
		t.Fatalf("read role objectives: %v", err)
	}

	tests := []struct {
		name   string
		mutate func([]map[string]any)
	}{
		{
			name: "legacy focus areas",
			mutate: func(roles []map[string]any) {
				delete(roles[0], "practice_objectives")
				roles[0]["focus_areas"] = []any{"evidence"}
			},
		},
		{
			name: "invalid objective id",
			mutate: func(roles []map[string]any) {
				objectives := roles[0]["practice_objectives"].([]any)
				objectives[0].(map[string]any)["objective_id"] = "Evidence"
			},
		},
		{
			name: "conflicting descriptions across roles",
			mutate: func(roles []map[string]any) {
				first := roles[0]["practice_objectives"].([]any)[0].(map[string]any)
				second := roles[1]["practice_objectives"].([]any)[0].(map[string]any)
				second["objective_id"] = first["objective_id"]
				second["description"] = "A conflicting description."
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var roles []map[string]any
			if err := json.Unmarshal(rolesDocument, &roles); err != nil {
				t.Fatalf("decode role objectives: %v", err)
			}
			test.mutate(roles)
			mutated, err := json.Marshal(roles)
			if err != nil {
				t.Fatalf("encode mutated role objectives: %v", err)
			}
			_, err = database.Exec(context.Background(), `
				INSERT INTO coaching_scene_versions (
				    scene_id, scene_version, scene_family, scene_model,
				    name, status, turn_policy_ref, session_policy_ref,
				    prompt, roles, practice_options, display_order
				)
				SELECT
				    scene_id, scene_version + $1, scene_family, scene_model,
				    name, status, turn_policy_ref, session_policy_ref,
				    prompt, $2::jsonb, practice_options, display_order
				FROM coaching_scene_versions
				WHERE scene_id = 'scn_programmer_interview'
				  AND scene_version = 1
			`, int64(300+index), string(mutated))
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) ||
				pgError.Code != "23514" ||
				pgError.ConstraintName != "coaching_scene_versions_payload_check" {
				t.Fatalf("invalid role objectives error = %v", err)
			}
		})
	}
}

func assertSceneAuthoritySchema(
	t *testing.T,
	connection *pgx.Conn,
	schema string,
	wantPresent bool,
) {
	t.Helper()

	for _, table := range []string{
		"coaching_scenes",
		"coaching_scene_versions",
	} {
		var relation *string
		if err := connection.QueryRow(
			context.Background(),
			"SELECT to_regclass($1)",
			schema+"."+table,
		).Scan(&relation); err != nil {
			t.Fatalf("inspect Scene authority table %s: %v", table, err)
		}
		if (relation != nil) != wantPresent {
			t.Errorf(
				"Scene authority table %s present = %t, want %t",
				table,
				relation != nil,
				wantPresent,
			)
		}
	}

	for _, forbiddenTable := range []string{
		"scenario_configs",
		"coaching_scenario_configs",
	} {
		var relation *string
		if err := connection.QueryRow(
			context.Background(),
			"SELECT to_regclass($1)",
			schema+"."+forbiddenTable,
		).Scan(&relation); err != nil {
			t.Fatalf("inspect forbidden catalog table %s: %v", forbiddenTable, err)
		}
		if relation != nil {
			t.Errorf("forbidden catalog table %s is present", forbiddenTable)
		}
	}

	var immutableTrigger bool
	if err := connection.QueryRow(context.Background(), `
		SELECT EXISTS (
		    SELECT 1
		    FROM pg_trigger AS trigger
		    JOIN pg_class AS relation
		      ON relation.oid = trigger.tgrelid
		    JOIN pg_namespace AS namespace
		      ON namespace.oid = relation.relnamespace
		    WHERE namespace.nspname = $1
		      AND relation.relname = 'coaching_scene_versions'
		      AND trigger.tgname = 'coaching_scene_versions_are_immutable'
		      AND NOT trigger.tgisinternal
		)
	`, schema).Scan(&immutableTrigger); err != nil {
		t.Fatalf("inspect Scene version immutable trigger: %v", err)
	}
	if immutableTrigger != wantPresent {
		t.Errorf(
			"Scene version immutable trigger present = %t, want %t",
			immutableTrigger,
			wantPresent,
		)
	}
}

func assertBuiltinSceneCatalog(t *testing.T, database *pgx.Conn) {
	t.Helper()

	var sceneCount, versionCount, ownedCount int
	if err := database.QueryRow(context.Background(), `
		SELECT
		    (SELECT count(*) FROM coaching_scenes),
		    (SELECT count(*) FROM coaching_scene_versions),
		    (
		        SELECT count(*)
		        FROM coaching_scenes
		        WHERE owner_user_id IS NOT NULL
		    )
	`).Scan(&sceneCount, &versionCount, &ownedCount); err != nil {
		t.Fatalf("count builtin Scene catalog: %v", err)
	}
	if sceneCount != builtinSceneCount || versionCount != builtinSceneCount {
		t.Fatalf(
			"builtin Scene identity/version count = %d/%d, want %d/%d",
			sceneCount,
			versionCount,
			builtinSceneCount,
			builtinSceneCount,
		)
	}
	if ownedCount != 0 {
		t.Fatalf("builtin Scene owner count = %d, want 0", ownedCount)
	}

	var fullMockTurnPolicy, fullMockSessionPolicy string
	if err := database.QueryRow(context.Background(), `
		SELECT turn_policy_ref, session_policy_ref
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_ielts_speaking_full'
		  AND scene_version = 2
	`).Scan(&fullMockTurnPolicy, &fullMockSessionPolicy); err != nil {
		t.Fatalf("read IELTS full mock policy refs: %v", err)
	}
	if fullMockTurnPolicy != "ielts.speaking_full_mock.turn.v1" ||
		fullMockSessionPolicy != "ielts.speaking_full_mock.session.v1" {
		t.Fatalf(
			"IELTS full mock policy refs = %q/%q, want dedicated refs",
			fullMockTurnPolicy,
			fullMockSessionPolicy,
		)
	}

	var interview, exam, workplace, daily int
	if err := database.QueryRow(context.Background(), `
		SELECT
		    count(*) FILTER (WHERE scene_family = 'INTERVIEW'),
		    count(*) FILTER (WHERE scene_family = 'EXAM'),
		    count(*) FILTER (WHERE scene_family = 'WORKPLACE'),
		    count(*) FILTER (WHERE scene_family = 'DAILY')
		FROM coaching_scene_versions
	`).Scan(&interview, &exam, &workplace, &daily); err != nil {
		t.Fatalf("count builtin Scene families: %v", err)
	}
	if interview != 7 || exam != 5 || workplace != 8 || daily != 11 {
		t.Fatalf(
			"builtin Scene family counts = %d/%d/%d/%d, want 7/5/8/11",
			interview,
			exam,
			workplace,
			daily,
		)
	}

	if hash := readSceneCatalogHash(t, database); hash != builtinSceneCatalogHash {
		t.Fatalf(
			"builtin Scene catalog hash = %s, want %s",
			hash,
			builtinSceneCatalogHash,
		)
	}
}

func assertSceneVersionsAreAppendOnly(t *testing.T, database *pgx.Conn) {
	t.Helper()

	for operation, statement := range map[string]string{
		"update": `
			UPDATE coaching_scene_versions
			SET name = name
			WHERE scene_id = 'scn_programmer_interview'
		`,
		"delete": `
			DELETE FROM coaching_scene_versions
			WHERE scene_id = 'scn_programmer_interview'
		`,
	} {
		_, err := database.Exec(context.Background(), statement)
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "55000" {
			t.Errorf("Scene version %s error = %v, want SQLSTATE 55000", operation, err)
		}
	}

	transaction, err := database.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin new Scene version insert: %v", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	if _, err := transaction.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
		    scene_id,
		    scene_version,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		)
		SELECT
		    scene_id,
		    scene_version + 100,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_programmer_interview'
	`); err != nil {
		t.Fatalf("insert a later immutable Scene version: %v", err)
	}
	if err := transaction.Rollback(context.Background()); err != nil {
		t.Fatalf("roll back later Scene version: %v", err)
	}

	_, err = database.Exec(context.Background(), `
		INSERT INTO coaching_scene_versions (
		    scene_id,
		    scene_version,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		)
		SELECT
		    scene_id,
		    scene_version + 200,
		    scene_family,
		    scene_model,
		    name,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    jsonb_set(roles, '{0,version}', '1'::jsonb),
		    practice_options,
		    display_order
		FROM coaching_scene_versions
		WHERE scene_id = 'scn_programmer_interview'
	`)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) ||
		constraintError.Code != "23514" ||
		constraintError.ConstraintName != "coaching_scene_versions_payload_check" {
		t.Fatalf(
			"Role-owned version insertion error = %v, want Scene payload check",
			err,
		)
	}
}

func readSceneCatalogHash(t *testing.T, database *pgx.Conn) string {
	t.Helper()

	rows, err := database.Query(context.Background(), `
		SELECT
		    scene_id,
		    scene_family,
		    scene_model,
		    name,
		    scene_version,
		    status,
		    turn_policy_ref,
		    session_policy_ref,
		    prompt,
		    roles,
		    practice_options,
		    display_order
		FROM coaching_scene_versions
		ORDER BY scene_id, scene_version
	`)
	if err != nil {
		t.Fatalf("query builtin Scene catalog: %v", err)
	}
	defer rows.Close()

	records := make([]map[string]any, 0, builtinSceneCount)
	for rows.Next() {
		var (
			sceneID          string
			sceneFamily      string
			sceneModel       string
			name             string
			sceneVersion     int64
			status           string
			turnPolicyRef    string
			sessionPolicyRef string
			promptJSON       []byte
			rolesJSON        []byte
			optionsJSON      []byte
			displayOrder     int
		)
		if err := rows.Scan(
			&sceneID,
			&sceneFamily,
			&sceneModel,
			&name,
			&sceneVersion,
			&status,
			&turnPolicyRef,
			&sessionPolicyRef,
			&promptJSON,
			&rolesJSON,
			&optionsJSON,
			&displayOrder,
		); err != nil {
			t.Fatalf("scan builtin Scene catalog row: %v", err)
		}

		var prompt, roles, options any
		for label, document := range map[string]struct {
			raw    []byte
			target *any
		}{
			"prompt":           {raw: promptJSON, target: &prompt},
			"roles":            {raw: rolesJSON, target: &roles},
			"practice_options": {raw: optionsJSON, target: &options},
		} {
			if err := json.Unmarshal(document.raw, document.target); err != nil {
				t.Fatalf("decode Scene %s: %v", label, err)
			}
		}

		records = append(records, map[string]any{
			"scene_id":           sceneID,
			"scene_family":       sceneFamily,
			"scene_model":        sceneModel,
			"name":               name,
			"scene_version":      sceneVersion,
			"status":             status,
			"turn_policy_ref":    turnPolicyRef,
			"session_policy_ref": sessionPolicyRef,
			"prompt":             prompt,
			"roles":              roles,
			"practice_options":   options,
			"display_order":      displayOrder,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate builtin Scene catalog: %v", err)
	}

	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode builtin Scene catalog hash input: %v", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
