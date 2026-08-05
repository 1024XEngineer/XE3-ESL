package scene

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCatalogReadsOnlyLatestActiveBuiltinScenes(t *testing.T) {
	pool := sceneCatalogTestDatabase(t)

	inactiveLatest := testSceneDefinition()
	inactiveLatest.ID = "scene-inactive-latest"
	reparentTestScene(&inactiveLatest)
	insertSceneVersion(t, pool, inactiveLatest, "", nil)
	inactiveLatest.Version = 2
	inactiveLatest.Status = SceneStatusInactive
	insertSceneVersion(t, pool, inactiveLatest, "", nil)

	second := testSceneDefinition()
	second.ID = "scene-second"
	second.DisplayOrder = 20
	reparentTestScene(&second)
	insertSceneVersion(t, pool, second, "", nil)

	first := testSceneDefinition()
	first.ID = "scene-first"
	first.DisplayOrder = 10
	reparentTestScene(&first)
	insertSceneVersion(t, pool, first, "", nil)

	custom := testSceneDefinition()
	custom.ID = "scene-custom"
	reparentTestScene(&custom)
	insertSceneVersion(
		t,
		pool,
		custom,
		"11111111-1111-1111-1111-111111111111",
		nil,
	)

	catalog, err := NewPostgresCatalog(pool, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewPostgresCatalog() error = %v", err)
	}
	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if len(definitions) != 2 || definitions[0].ID != first.ID ||
		definitions[1].ID != second.ID {
		t.Fatalf("active builtin Scenes = %#v", definitions)
	}
	for _, sceneID := range []string{inactiveLatest.ID, custom.ID} {
		if _, err := catalog.GetScene(context.Background(), sceneID); !errors.Is(err, ErrSceneNotFound) {
			t.Fatalf("GetScene(%q) error = %v", sceneID, err)
		}
	}
}

func TestPostgresCatalogResolvesOnlyLatestActiveVersion(t *testing.T) {
	pool := sceneCatalogTestDatabase(t)
	definition := testSceneDefinition()
	insertSceneVersion(t, pool, definition, "", nil)

	catalog, err := NewPostgresCatalog(pool, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewPostgresCatalog() error = %v", err)
	}
	selection, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		definition.Version,
		[]string{testRoleID},
		testFocusOptionID,
	)
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}
	if selection.Scene.ID != definition.ID ||
		selection.Scene.Version != definition.Version ||
		selection.PracticeOptionID != testFocusOptionID {
		t.Fatalf("selection = %#v", selection)
	}

	inactive := definition
	inactive.Version = definition.Version + 1
	inactive.Status = SceneStatusInactive
	insertSceneVersion(t, pool, inactive, "", nil)
	if _, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		definition.Version,
		[]string{testRoleID},
		testFocusOptionID,
	); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("historical active version after retirement error = %v", err)
	}
	if _, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		inactive.Version,
		[]string{testRoleID},
		testFocusOptionID,
	); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("inactive exact version error = %v", err)
	}
	if _, err := catalog.ResolveSelection(
		context.Background(),
		definition.ID,
		inactive.Version+1,
		[]string{testRoleID},
		testFocusOptionID,
	); !errors.Is(err, ErrSceneNotFound) {
		t.Fatalf("missing exact version error = %v", err)
	}
}

func TestPostgresCatalogResolvesPublicOrOwnedPrivateScene(t *testing.T) {
	pool := sceneCatalogTestDatabase(t)
	const (
		ownerUserID = "11111111-1111-4111-8111-111111111111"
		otherUserID = "22222222-2222-4222-8222-222222222222"
	)
	publicDefinition := testSceneDefinition()
	publicDefinition.ID = "scene-public-accessible"
	reparentTestScene(&publicDefinition)
	insertSceneVersion(t, pool, publicDefinition, "", nil)

	privateDefinition := testSceneDefinition()
	privateDefinition.ID = "scene-private-accessible"
	reparentTestScene(&privateDefinition)
	insertSceneVersion(t, pool, privateDefinition, ownerUserID, nil)

	catalog, err := NewPostgresCatalog(pool, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewPostgresCatalog() error = %v", err)
	}
	for _, test := range []struct {
		name    string
		userID  string
		scene   SceneDefinition
		wantErr error
	}{
		{
			name:   "public Scene",
			userID: otherUserID,
			scene:  publicDefinition,
		},
		{
			name:   "owned private Scene",
			userID: ownerUserID,
			scene:  privateDefinition,
		},
		{
			name:    "another user's private Scene",
			userID:  otherUserID,
			scene:   privateDefinition,
			wantErr: ErrSceneNotFound,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection, resolveErr := catalog.ResolveAccessibleSelection(
				context.Background(),
				test.userID,
				test.scene.ID,
				test.scene.Version,
				[]string{test.scene.Roles[0].ID},
				test.scene.PracticeOptions[1].ID,
			)
			if !errors.Is(resolveErr, test.wantErr) {
				t.Fatalf("ResolveAccessibleSelection() error = %v", resolveErr)
			}
			if test.wantErr == nil && selection.Scene.ID != test.scene.ID {
				t.Fatalf("selection Scene = %#v", selection.Scene)
			}
		})
	}

	definitions, err := catalog.ListActiveScenes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if len(definitions) != 1 || definitions[0].ID != publicDefinition.ID {
		t.Fatalf("public active Scenes = %#v", definitions)
	}
}

func TestPostgresCatalogRejectsInvalidStoredJSON(t *testing.T) {
	pool := sceneCatalogTestDatabase(t)
	definition := testSceneDefinition()
	insertSceneVersion(
		t,
		pool,
		definition,
		"",
		[]byte(`{"unexpected":true}`),
	)

	catalog, err := NewPostgresCatalog(pool, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewPostgresCatalog() error = %v", err)
	}
	_, err = catalog.GetScene(context.Background(), definition.ID)
	if !errors.Is(err, ErrCatalogDefinitionInvalid) {
		t.Fatalf("GetScene() error = %v", err)
	}
}

func TestPostgresCatalogRejectsUnavailableEvaluationPolicy(t *testing.T) {
	pool := sceneCatalogTestDatabase(t)
	definition := testSceneDefinition()
	definition.EvaluationPolicyRef = "unknown.fixture.evaluation.v1"
	insertSceneVersion(t, pool, definition, "", nil)

	catalog, err := NewPostgresCatalog(pool, testPolicyValidator())
	if err != nil {
		t.Fatalf("NewPostgresCatalog() error = %v", err)
	}
	if _, err := catalog.GetScene(context.Background(), definition.ID); !errors.Is(err, ErrCatalogDefinitionInvalid) {
		t.Fatalf("GetScene() error = %v", err)
	}
}

func TestPostgresCatalogReportsDatabaseAndContextErrors(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	catalog, err := newPostgresCatalog(
		failingCatalogDatabase{err: databaseErr},
		testPolicyValidator(),
	)
	if err != nil {
		t.Fatalf("newPostgresCatalog() error = %v", err)
	}
	if _, err := catalog.ListActiveScenes(context.Background()); !errors.Is(err, ErrCatalogReadFailed) || !errors.Is(err, databaseErr) {
		t.Fatalf("ListActiveScenes() error = %v", err)
	}
	if _, err := catalog.GetScene(context.Background(), testSceneID); !errors.Is(err, ErrCatalogReadFailed) || !errors.Is(err, databaseErr) {
		t.Fatalf("GetScene() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ListActiveScenes(ctx); !errors.Is(err, ErrCatalogReadFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ListActiveScenes() error = %v", err)
	}
}

type failingCatalogDatabase struct {
	err error
}

func (database failingCatalogDatabase) Query(
	context.Context,
	string,
	...any,
) (catalogRows, error) {
	return nil, database.err
}

func (database failingCatalogDatabase) QueryRow(
	context.Context,
	string,
	...any,
) catalogRow {
	return failingCatalogRow{err: database.err}
}

type failingCatalogRow struct {
	err error
}

func (row failingCatalogRow) Scan(...any) error {
	return row.err
}

func sceneCatalogTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open Scene catalog test database: %v", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		admin.Close()
		t.Fatalf("create Scene catalog schema suffix: %v", err)
	}
	schema := "scene_catalog_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatalf("create Scene catalog schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
		t.Fatalf("parse Scene catalog database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
		t.Fatalf("open isolated Scene catalog pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop Scene catalog schema: %v", err)
		}
		admin.Close()
	})

	statements := []string{
		`CREATE TABLE coaching_scenes (
            scene_id text PRIMARY KEY,
            owner_user_id uuid,
            created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
        )`,
		`CREATE TABLE coaching_scene_versions (
            scene_id text NOT NULL REFERENCES coaching_scenes (scene_id),
            scene_version bigint NOT NULL,
            scene_family text NOT NULL,
            scene_model text NOT NULL,
            name text NOT NULL,
            status text NOT NULL,
            turn_policy_ref text NOT NULL,
            session_policy_ref text NOT NULL,
			evaluation_policy_ref text NOT NULL,
            prompt jsonb NOT NULL,
            roles jsonb NOT NULL,
            practice_options jsonb NOT NULL,
            display_order integer NOT NULL,
            created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
            PRIMARY KEY (scene_id, scene_version)
        )`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("create Scene catalog table: %v", err)
		}
	}
	return pool
}

func insertSceneVersion(
	t *testing.T,
	pool *pgxpool.Pool,
	definition SceneDefinition,
	ownerUserID string,
	promptOverride []byte,
) {
	t.Helper()
	prompt, err := json.Marshal(definition.Prompt)
	if err != nil {
		t.Fatalf("encode Scene prompt: %v", err)
	}
	if promptOverride != nil {
		prompt = promptOverride
	}
	databaseRoles := make([]databaseRoleDefinition, len(definition.Roles))
	for index, role := range definition.Roles {
		databaseRoles[index] = databaseRoleDefinition{
			ID:                 role.ID,
			SceneID:            role.SceneID,
			Type:               role.Type,
			DisplayName:        role.DisplayName,
			Responsibilities:   role.Responsibilities,
			Style:              role.Style,
			PracticeObjectives: role.PracticeObjectives,
			VoiceConfigRef:     role.VoiceConfigRef,
			DisplayOrder:       role.DisplayOrder,
		}
	}
	roles, err := json.Marshal(databaseRoles)
	if err != nil {
		t.Fatalf("encode Scene roles: %v", err)
	}
	databaseOptions := make([]databasePracticeOption, len(definition.PracticeOptions))
	for index, option := range definition.PracticeOptions {
		databaseOptions[index] = databasePracticeOption{
			ID:               option.ID,
			SceneID:          option.SceneID,
			RoleDefinitionID: option.RoleDefinitionID,
			Type:             option.Type,
			DisplayName:      option.DisplayName,
			DisplayOrder:     option.DisplayOrder,
		}
	}
	options, err := json.Marshal(databaseOptions)
	if err != nil {
		t.Fatalf("encode Scene practice options: %v", err)
	}
	var owner any
	if ownerUserID != "" {
		owner = ownerUserID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO coaching_scenes (scene_id, owner_user_id)
         VALUES ($1, $2)
         ON CONFLICT (scene_id) DO NOTHING`,
		definition.ID,
		owner,
	); err != nil {
		t.Fatalf("insert Scene identity: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO coaching_scene_versions (
            scene_id,
            scene_version,
            scene_family,
            scene_model,
            name,
            status,
            turn_policy_ref,
            session_policy_ref,
			evaluation_policy_ref,
            prompt,
            roles,
            practice_options,
            display_order
         ) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10::jsonb, $11::jsonb, $12::jsonb, $13
         )`,
		definition.ID,
		definition.Version,
		definition.Family,
		definition.Model,
		definition.Name,
		definition.Status,
		definition.TurnPolicyRef,
		definition.SessionPolicyRef,
		definition.EvaluationPolicyRef,
		string(prompt),
		string(roles),
		string(options),
		definition.DisplayOrder,
	); err != nil {
		t.Fatalf("insert Scene version: %v", err)
	}
}
