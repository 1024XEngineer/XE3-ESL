package scene

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const listActiveScenesQuery = `
WITH latest_builtin AS (
    SELECT DISTINCT ON (versions.scene_id)
        versions.scene_id,
        versions.scene_version,
        versions.practice_experience,
        versions.scene_category,
        versions.name,
        versions.status,
        versions.prompt,
        versions.roles,
        versions.practice_options,
        versions.display_order
    FROM coaching_scene_versions AS versions
    JOIN coaching_scenes AS scenes
      ON scenes.scene_id = versions.scene_id
    WHERE scenes.owner_user_id IS NULL
    ORDER BY versions.scene_id, versions.scene_version DESC
)
SELECT
    scene_id,
    scene_version,
    practice_experience,
    scene_category,
    name,
    status,
    prompt,
    roles,
    practice_options,
    display_order
FROM latest_builtin
WHERE status = 'active'
ORDER BY display_order, scene_id`

const getActiveSceneQuery = `
WITH latest_builtin AS (
    SELECT
        versions.scene_id,
        versions.scene_version,
        versions.practice_experience,
        versions.scene_category,
        versions.name,
        versions.status,
        versions.prompt,
        versions.roles,
        versions.practice_options,
        versions.display_order
    FROM coaching_scene_versions AS versions
    JOIN coaching_scenes AS scenes
      ON scenes.scene_id = versions.scene_id
    WHERE scenes.owner_user_id IS NULL
      AND versions.scene_id = $1
    ORDER BY versions.scene_version DESC
    LIMIT 1
)
SELECT
    scene_id,
    scene_version,
    practice_experience,
    scene_category,
    name,
    status,
    prompt,
    roles,
    practice_options,
    display_order
FROM latest_builtin
WHERE status = 'active'`

const resolveActiveSceneVersionQuery = `
WITH latest_builtin AS (
    SELECT
        versions.scene_id,
        versions.scene_version,
        versions.practice_experience,
        versions.scene_category,
        versions.name,
        versions.status,
        versions.prompt,
        versions.roles,
        versions.practice_options,
        versions.display_order
    FROM coaching_scene_versions AS versions
    JOIN coaching_scenes AS scenes
      ON scenes.scene_id = versions.scene_id
    WHERE scenes.owner_user_id IS NULL
      AND versions.scene_id = $1
    ORDER BY versions.scene_version DESC
    LIMIT 1
)
SELECT
    scene_id,
    scene_version,
    practice_experience,
    scene_category,
    name,
    status,
    prompt,
    roles,
    practice_options,
    display_order
FROM latest_builtin
WHERE scene_version = $2
  AND status = 'active'`

const resolveAccessibleActiveSceneVersionQuery = `
WITH latest_accessible AS (
    SELECT
        versions.scene_id,
        versions.scene_version,
        versions.practice_experience,
        versions.scene_category,
        versions.name,
        versions.status,
        versions.prompt,
        versions.roles,
        versions.practice_options,
        versions.display_order
    FROM coaching_scene_versions AS versions
    JOIN coaching_scenes AS scenes
      ON scenes.scene_id = versions.scene_id
    WHERE versions.scene_id = $1
      AND (
          scenes.owner_user_id IS NULL
          OR scenes.owner_user_id = $2
      )
    ORDER BY versions.scene_version DESC
    LIMIT 1
)
SELECT
    scene_id,
    scene_version,
    practice_experience,
    scene_category,
    name,
    status,
    prompt,
    roles,
    practice_options,
    display_order
FROM latest_accessible
WHERE scene_version = $3
  AND status = 'active'`

type catalogRow interface {
	Scan(...any) error
}

type catalogRows interface {
	Close()
	Next() bool
	Scan(...any) error
	Err() error
}

type catalogDatabase interface {
	Query(context.Context, string, ...any) (catalogRows, error)
	QueryRow(context.Context, string, ...any) catalogRow
}

type pgxCatalogDatabase struct {
	pool *pgxpool.Pool
}

func (database pgxCatalogDatabase) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (catalogRows, error) {
	return database.pool.Query(ctx, query, arguments...)
}

func (database pgxCatalogDatabase) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) catalogRow {
	return database.pool.QueryRow(ctx, query, arguments...)
}

// PostgresCatalog is the production read adapter for immutable Scene versions.
type PostgresCatalog struct {
	database        catalogDatabase
	policyValidator EvaluationPolicyReferenceValidator
}

func NewPostgresCatalog(
	pool *pgxpool.Pool,
	policyValidator EvaluationPolicyReferenceValidator,
) (*PostgresCatalog, error) {
	if pool == nil {
		return nil, errors.New("scene: PostgreSQL catalog pool is required")
	}
	return newPostgresCatalog(
		pgxCatalogDatabase{pool: pool},
		policyValidator,
	)
}

func newPostgresCatalog(
	database catalogDatabase,
	policyValidator EvaluationPolicyReferenceValidator,
) (*PostgresCatalog, error) {
	if database == nil {
		return nil, errors.New("scene: PostgreSQL catalog database is required")
	}
	if policyValidator == nil {
		return nil, errors.New(
			"scene: Evaluation policy validator is required",
		)
	}
	return &PostgresCatalog{
		database:        database,
		policyValidator: policyValidator,
	}, nil
}

func (catalog *PostgresCatalog) ListActiveScenes(
	ctx context.Context,
) ([]SceneDefinition, error) {
	if err := catalogContextError(ctx); err != nil {
		return nil, err
	}
	rows, err := catalog.database.Query(ctx, listActiveScenesQuery)
	if err != nil {
		return nil, catalogReadError("list active Scenes", err)
	}
	defer rows.Close()

	definitions := make([]SceneDefinition, 0)
	for rows.Next() {
		definition, scanErr := scanSceneDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, catalogReadError("list active Scenes", err)
	}
	return validateLoadedScenes(definitions, catalog.policyValidator)
}

func (catalog *PostgresCatalog) GetScene(
	ctx context.Context,
	sceneID string,
) (SceneDefinition, error) {
	if err := catalogContextError(ctx); err != nil {
		return SceneDefinition{}, err
	}
	definition, err := scanSceneDefinition(
		catalog.database.QueryRow(ctx, getActiveSceneQuery, sceneID),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SceneDefinition{}, ErrSceneNotFound
	}
	if err != nil {
		return SceneDefinition{}, err
	}
	validated, err := validateLoadedScenes(
		[]SceneDefinition{definition},
		catalog.policyValidator,
	)
	if err != nil {
		return SceneDefinition{}, err
	}
	return validated[0], nil
}

func (catalog *PostgresCatalog) ListRoles(
	ctx context.Context,
	sceneID string,
) ([]RoleDefinition, error) {
	definition, err := catalog.GetScene(ctx, sceneID)
	if err != nil {
		return nil, err
	}
	return cloneRoles(definition.Roles), nil
}

func (catalog *PostgresCatalog) ResolveSelection(
	ctx context.Context,
	sceneID string,
	sceneVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
) (SelectionSnapshot, error) {
	if err := catalogContextError(ctx); err != nil {
		return SelectionSnapshot{}, err
	}
	definition, err := scanSceneDefinition(catalog.database.QueryRow(
		ctx,
		resolveActiveSceneVersionQuery,
		sceneID,
		sceneVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return SelectionSnapshot{}, ErrSceneNotFound
	}
	if err != nil {
		return SelectionSnapshot{}, err
	}
	validated, err := validateLoadedScenes(
		[]SceneDefinition{definition},
		catalog.policyValidator,
	)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	memoryCatalog, err := newValidatedCatalog(
		validated,
		catalog.policyValidator,
	)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	return memoryCatalog.ResolveSelection(
		ctx,
		sceneID,
		sceneVersion,
		selectedRoleIDs,
		practiceOptionID,
	)
}

func (catalog *PostgresCatalog) ResolveAccessibleSelection(
	ctx context.Context,
	ownerUserID string,
	sceneID string,
	sceneVersion int,
	selectedRoleIDs []string,
	practiceOptionID string,
) (SelectionSnapshot, error) {
	if err := catalogContextError(ctx); err != nil {
		return SelectionSnapshot{}, err
	}
	if !validCatalogOwner(ownerUserID) {
		return SelectionSnapshot{}, ErrCatalogSelectionInvalid
	}
	definition, err := scanSceneDefinition(catalog.database.QueryRow(
		ctx,
		resolveAccessibleActiveSceneVersionQuery,
		sceneID,
		ownerUserID,
		sceneVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return SelectionSnapshot{}, ErrSceneNotFound
	}
	if err != nil {
		return SelectionSnapshot{}, err
	}
	validated, err := validateLoadedScenes(
		[]SceneDefinition{definition},
		catalog.policyValidator,
	)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	memoryCatalog, err := newValidatedCatalog(
		validated,
		catalog.policyValidator,
	)
	if err != nil {
		return SelectionSnapshot{}, err
	}
	return memoryCatalog.ResolveSelection(
		ctx,
		sceneID,
		sceneVersion,
		selectedRoleIDs,
		practiceOptionID,
	)
}

type databaseRoleDefinition struct {
	ID                 string                        `json:"role_definition_id"`
	SceneID            string                        `json:"scene_id"`
	Type               string                        `json:"role_type"`
	DisplayName        string                        `json:"display_name"`
	Responsibilities   string                        `json:"responsibilities"`
	Style              string                        `json:"style"`
	PracticeObjectives []PracticeObjectiveDefinition `json:"practice_objectives"`
	VoiceConfigRef     string                        `json:"voice_config_ref,omitempty"`
	DisplayOrder       int                           `json:"display_order"`
}

type databasePracticeOption struct {
	ID                       string       `json:"practice_option_id"`
	SceneID                  string       `json:"scene_id"`
	RoleDefinitionID         string       `json:"role_definition_id,omitempty"`
	Mode                     PracticeMode `json:"practice_mode"`
	DisplayName              string       `json:"display_name"`
	SuggestedDurationSeconds int          `json:"suggested_duration_seconds"`
	TurnPolicyRef            string       `json:"turn_policy_ref"`
	SessionPolicyRef         string       `json:"session_policy_ref"`
	EvaluationPolicyRef      string       `json:"evaluation_policy_ref"`
	DisplayOrder             int          `json:"display_order"`
}

func scanSceneDefinition(scanner catalogRow) (SceneDefinition, error) {
	var (
		definition     SceneDefinition
		version        int64
		promptPayload  []byte
		rolesPayload   []byte
		optionsPayload []byte
	)
	if err := scanner.Scan(
		&definition.ID,
		&version,
		&definition.Experience,
		&definition.Category,
		&definition.Name,
		&definition.Status,
		&promptPayload,
		&rolesPayload,
		&optionsPayload,
		&definition.DisplayOrder,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SceneDefinition{}, pgx.ErrNoRows
		}
		return SceneDefinition{}, catalogReadError("scan Scene version", err)
	}
	if version < 1 || version > int64(math.MaxInt) {
		return SceneDefinition{}, invalidDefinition(
			"scene %q has invalid version %d",
			definition.ID,
			version,
		)
	}
	definition.Version = int(version)
	if err := decodeStrictJSON(promptPayload, &definition.Prompt); err != nil {
		return SceneDefinition{}, invalidDefinition(
			"scene %q version %d has invalid prompt JSON: %v",
			definition.ID,
			definition.Version,
			err,
		)
	}
	var databaseRoles []databaseRoleDefinition
	if err := decodeStrictJSON(rolesPayload, &databaseRoles); err != nil {
		return SceneDefinition{}, invalidDefinition(
			"scene %q version %d has invalid roles JSON: %v",
			definition.ID,
			definition.Version,
			err,
		)
	}
	definition.Roles = make([]RoleDefinition, len(databaseRoles))
	for index, role := range databaseRoles {
		definition.Roles[index] = RoleDefinition{
			ID:                 role.ID,
			SceneID:            role.SceneID,
			Type:               role.Type,
			DisplayName:        role.DisplayName,
			Responsibilities:   role.Responsibilities,
			Style:              role.Style,
			PracticeObjectives: append([]PracticeObjectiveDefinition(nil), role.PracticeObjectives...),
			VoiceConfigRef:     role.VoiceConfigRef,
			DisplayOrder:       role.DisplayOrder,
		}
	}
	var databaseOptions []databasePracticeOption
	if err := decodeStrictJSON(optionsPayload, &databaseOptions); err != nil {
		return SceneDefinition{}, invalidDefinition(
			"scene %q version %d has invalid practice_options JSON: %v",
			definition.ID,
			definition.Version,
			err,
		)
	}
	definition.PracticeOptions = make([]PracticeOption, len(databaseOptions))
	for index, option := range databaseOptions {
		definition.PracticeOptions[index] = PracticeOption{
			ID:                       option.ID,
			SceneID:                  option.SceneID,
			RoleDefinitionID:         option.RoleDefinitionID,
			Mode:                     option.Mode,
			DisplayName:              option.DisplayName,
			SuggestedDurationSeconds: option.SuggestedDurationSeconds,
			TurnPolicyRef:            option.TurnPolicyRef,
			SessionPolicyRef:         option.SessionPolicyRef,
			EvaluationPolicyRef:      option.EvaluationPolicyRef,
			DisplayOrder:             option.DisplayOrder,
		}
	}
	return definition, nil
}

func decodeStrictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateLoadedScenes(
	definitions []SceneDefinition,
	policyValidator EvaluationPolicyReferenceValidator,
) ([]SceneDefinition, error) {
	if len(definitions) == 0 {
		return []SceneDefinition{}, nil
	}
	catalog, err := newValidatedCatalog(definitions, policyValidator)
	if err != nil {
		return nil, err
	}
	result := make([]SceneDefinition, len(catalog.scenes))
	for index, definition := range catalog.scenes {
		result[index] = cloneScene(definition)
	}
	return result, nil
}

func catalogReadError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrCatalogReadFailed, operation, err)
}
