package memory

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

type PostgreSQL interface {
	Begin(context.Context) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	database PostgreSQL
	ids      IDGenerator
}

func NewPostgresRepository(
	database PostgreSQL,
	ids IDGenerator,
) (*PostgresRepository, error) {
	if database == nil || ids == nil {
		return nil, ErrRepository
	}
	return &PostgresRepository{database: database, ids: ids}, nil
}

func (repository *PostgresRepository) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateCommand,
) (Memory, error) {
	if ctx == nil || !validActor(actor) || !command.Valid() {
		return Memory{}, ErrInvalidArgument
	}
	memoryID, sourceID, err := repository.newMutationIDs()
	if err != nil {
		return Memory{}, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return Memory{}, ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockActiveOwner(ctx, tx, actor.UserID); err != nil {
		return Memory{}, err
	}
	if command.Scope == ScopeMatter {
		if err := requireOwnedMatter(
			ctx,
			tx,
			actor.UserID,
			command.MatterID,
		); err != nil {
			return Memory{}, err
		}
	}

	item, err := scanMemory(tx.QueryRow(ctx, `
INSERT INTO agent_memories (
    id,
    owner_user_id,
    memory_type,
    canonical_key,
    content,
    scope_type,
    matter_id,
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    'active', 1, $8, $9,
    transaction_timestamp(),
    transaction_timestamp()
)
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
		memoryID,
		actor.UserID,
		command.Type,
		command.CanonicalKey,
		command.Content,
		command.Scope,
		nullableUUID(command.MatterID),
		command.PolicyVersion,
		command.ExpiresAt,
	))
	if err != nil {
		return Memory{}, mapPostgresError(err)
	}
	if err := insertSource(
		ctx,
		tx,
		sourceID,
		actor.UserID,
		item.ID,
		command.Source,
	); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Memory{}, ErrRepository
	}
	return item, nil
}

func (repository *PostgresRepository) Find(
	ctx context.Context,
	actor requestcontext.Actor,
	memoryID string,
) (Memory, error) {
	if ctx == nil || !validActor(actor) || !validUUID(memoryID) {
		return Memory{}, ErrInvalidArgument
	}
	item, err := scanMemory(repository.database.QueryRow(ctx, `
SELECT
    memories.id::text,
    memories.owner_user_id::text,
    memories.memory_type,
    memories.canonical_key,
    memories.content,
    memories.scope_type,
    coalesce(memories.matter_id::text, ''),
    memories.status,
    memories.version,
    memories.policy_version,
    memories.expires_at,
    memories.created_at,
    memories.updated_at,
    memories.inactivated_at
FROM agent_memories AS memories
JOIN identity_users AS users
  ON users.id = memories.owner_user_id
 AND users.account_status = 'active'
WHERE memories.id = $1
  AND memories.owner_user_id = $2`,
		memoryID,
		actor.UserID,
	))
	if err != nil {
		return Memory{}, mapPostgresError(err)
	}
	return item, nil
}

func (repository *PostgresRepository) ListActive(
	ctx context.Context,
	actor requestcontext.Actor,
	filter ScopeFilter,
) ([]Memory, error) {
	if ctx == nil || !validActor(actor) || !filter.Valid() {
		return nil, ErrInvalidArgument
	}
	query := `
SELECT
    memories.id::text,
    memories.owner_user_id::text,
    memories.memory_type,
    memories.canonical_key,
    memories.content,
    memories.scope_type,
    coalesce(memories.matter_id::text, ''),
    memories.status,
    memories.version,
    memories.policy_version,
    memories.expires_at,
    memories.created_at,
    memories.updated_at,
    memories.inactivated_at
FROM agent_memories AS memories
JOIN identity_users AS users
  ON users.id = memories.owner_user_id
 AND users.account_status = 'active'
WHERE memories.owner_user_id = $1
  AND memories.scope_type = 'user'
  AND memories.matter_id IS NULL
  AND memories.status = 'active'
  AND (memories.expires_at IS NULL OR memories.expires_at > CURRENT_TIMESTAMP)
ORDER BY memories.updated_at DESC, memories.id DESC
LIMIT $2`
	arguments := []any{actor.UserID, filter.Limit}
	if filter.Scope == ScopeMatter {
		query = `
SELECT
    memories.id::text,
    memories.owner_user_id::text,
    memories.memory_type,
    memories.canonical_key,
    memories.content,
    memories.scope_type,
    coalesce(memories.matter_id::text, ''),
    memories.status,
    memories.version,
    memories.policy_version,
    memories.expires_at,
    memories.created_at,
    memories.updated_at,
    memories.inactivated_at
FROM agent_memories AS memories
JOIN identity_users AS users
  ON users.id = memories.owner_user_id
 AND users.account_status = 'active'
WHERE memories.owner_user_id = $1
  AND memories.scope_type = 'matter'
  AND memories.matter_id = $2
  AND memories.status = 'active'
  AND (memories.expires_at IS NULL OR memories.expires_at > CURRENT_TIMESTAMP)
ORDER BY memories.updated_at DESC, memories.id DESC
LIMIT $3`
		arguments = []any{actor.UserID, filter.MatterID, filter.Limit}
	}
	rows, err := repository.database.Query(ctx, query, arguments...)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	items := make([]Memory, 0)
	for rows.Next() {
		item, scanErr := scanMemory(rows)
		if scanErr != nil {
			return nil, ErrRepository
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return items, nil
}

func (repository *PostgresRepository) ListSources(
	ctx context.Context,
	actor requestcontext.Actor,
	memoryID string,
) ([]Source, error) {
	if ctx == nil || !validActor(actor) || !validUUID(memoryID) {
		return nil, ErrInvalidArgument
	}
	rows, err := repository.database.Query(ctx, `
SELECT
    sources.id::text,
    sources.owner_user_id::text,
    sources.memory_id::text,
    sources.source_type,
    sources.source_id,
    sources.source_version,
    sources.source_checksum,
    sources.created_at
FROM agent_memory_sources AS sources
JOIN agent_memories AS memories
  ON memories.id = sources.memory_id
 AND memories.owner_user_id = sources.owner_user_id
JOIN identity_users AS users
  ON users.id = sources.owner_user_id
 AND users.account_status = 'active'
WHERE sources.memory_id = $1
  AND sources.owner_user_id = $2
ORDER BY sources.created_at, sources.id`,
		memoryID,
		actor.UserID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	items := make([]Source, 0)
	for rows.Next() {
		item, scanErr := scanSource(rows)
		if scanErr != nil {
			return nil, ErrRepository
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	if len(items) == 0 {
		if _, findErr := repository.Find(
			ctx,
			actor,
			memoryID,
		); findErr != nil {
			return nil, findErr
		}
	}
	return items, nil
}

func (repository *PostgresRepository) Update(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateCommand,
) (Memory, error) {
	if ctx == nil || !validActor(actor) || !command.Valid() {
		return Memory{}, ErrInvalidArgument
	}
	sourceID, err := repository.newID()
	if err != nil {
		return Memory{}, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return Memory{}, ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockActiveOwner(ctx, tx, actor.UserID); err != nil {
		return Memory{}, err
	}

	item, err := scanMemory(tx.QueryRow(ctx, `
UPDATE agent_memories
SET
    content = $4,
    policy_version = $5,
    expires_at = $6,
    version = version + 1,
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND version = $3
  AND status = 'active'
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
		command.MemoryID,
		actor.UserID,
		command.ExpectedVersion,
		command.Content,
		command.PolicyVersion,
		command.ExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, classifyMutationMiss(
			ctx,
			tx,
			actor.UserID,
			command.MemoryID,
		)
	}
	if err != nil {
		return Memory{}, mapPostgresError(err)
	}
	if err := insertSource(
		ctx,
		tx,
		sourceID,
		actor.UserID,
		item.ID,
		command.Source,
	); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Memory{}, ErrRepository
	}
	return item, nil
}

func (repository *PostgresRepository) Inactivate(
	ctx context.Context,
	actor requestcontext.Actor,
	command InactivateCommand,
) (Memory, error) {
	if ctx == nil || !validActor(actor) || !command.Valid() {
		return Memory{}, ErrInvalidArgument
	}
	sourceID, err := repository.newID()
	if err != nil {
		return Memory{}, err
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return Memory{}, ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockActiveOwner(ctx, tx, actor.UserID); err != nil {
		return Memory{}, err
	}

	item, err := scanMemory(tx.QueryRow(ctx, `
UPDATE agent_memories
SET
    status = 'inactive',
    version = version + 1,
    inactivated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    ),
    updated_at = GREATEST(
        transaction_timestamp(),
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND version = $3
  AND status = 'active'
RETURNING
    id::text,
    owner_user_id::text,
    memory_type,
    canonical_key,
    content,
    scope_type,
    coalesce(matter_id::text, ''),
    status,
    version,
    policy_version,
    expires_at,
    created_at,
    updated_at,
    inactivated_at`,
		command.MemoryID,
		actor.UserID,
		command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, classifyMutationMiss(
			ctx,
			tx,
			actor.UserID,
			command.MemoryID,
		)
	}
	if err != nil {
		return Memory{}, mapPostgresError(err)
	}
	if err := insertSource(
		ctx,
		tx,
		sourceID,
		actor.UserID,
		item.ID,
		command.Source,
	); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Memory{}, ErrRepository
	}
	return item, nil
}

func (repository *PostgresRepository) Delete(
	ctx context.Context,
	actor requestcontext.Actor,
	memoryID string,
) error {
	if ctx == nil || !validActor(actor) || !validUUID(memoryID) {
		return ErrInvalidArgument
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockActiveOwner(ctx, tx, actor.UserID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
DELETE FROM agent_memories
WHERE id = $1 AND owner_user_id = $2`,
		memoryID,
		actor.UserID,
	)
	if err != nil {
		return mapPostgresError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrRepository
	}
	return nil
}

func (repository *PostgresRepository) DeleteOwnerData(
	ctx context.Context,
	command DeleteOwnerCommand,
) error {
	if ctx == nil || !command.Valid() {
		return ErrInvalidArgument
	}
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return ErrRepository
	}
	defer rollback(ctx, tx)
	if err := lockOwner(ctx, tx, command.UserID); err != nil {
		return err
	}

	var accountStatus string
	err = tx.QueryRow(ctx, `
SELECT account_status
FROM identity_users
WHERE id = $1
FOR UPDATE`,
		command.UserID,
	).Scan(&accountStatus)
	identityMissing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !identityMissing {
		return ErrRepository
	}

	var persistedGeneration int64
	fenceErr := tx.QueryRow(ctx, `
SELECT deletion_generation
FROM agent_memory_deletion_fences
WHERE owner_user_id = $1
FOR UPDATE`,
		command.UserID,
	).Scan(&persistedGeneration)
	fenceMissing := errors.Is(fenceErr, pgx.ErrNoRows)
	if fenceErr != nil && !fenceMissing {
		return ErrRepository
	}
	if identityMissing && fenceMissing {
		return ErrNotFound
	}
	if !identityMissing &&
		accountStatus != "deleting" &&
		accountStatus != "deleted" {
		return ErrInvalidArgument
	}
	if !fenceMissing && int64(command.Generation) < persistedGeneration {
		return ErrDeletionGeneration
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO agent_memory_deletion_fences (
    owner_user_id,
    deletion_generation,
    created_at,
    updated_at
) VALUES (
    $1, $2, transaction_timestamp(), transaction_timestamp()
)
ON CONFLICT (owner_user_id) DO UPDATE
SET
    deletion_generation = GREATEST(
        agent_memory_deletion_fences.deletion_generation,
        EXCLUDED.deletion_generation
    ),
    updated_at = CASE
        WHEN EXCLUDED.deletion_generation >
             agent_memory_deletion_fences.deletion_generation
        THEN transaction_timestamp()
        ELSE agent_memory_deletion_fences.updated_at
    END`,
		command.UserID,
		command.Generation,
	); err != nil {
		return mapPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_memory_extraction_jobs WHERE owner_user_id = $1`,
		command.UserID,
	); err != nil {
		return mapPostgresError(err)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM agent_memories WHERE owner_user_id = $1`,
		command.UserID,
	); err != nil {
		return mapPostgresError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrRepository
	}
	return nil
}

func (repository *PostgresRepository) newMutationIDs() (
	string,
	string,
	error,
) {
	memoryID, err := repository.newID()
	if err != nil {
		return "", "", err
	}
	sourceID, err := repository.newID()
	if err != nil {
		return "", "", err
	}
	return memoryID, sourceID, nil
}

func (repository *PostgresRepository) newID() (string, error) {
	identifier, err := repository.ids.NewID()
	if err != nil || !validUUID(identifier) {
		return "", ErrRepository
	}
	return identifier, nil
}

func lockActiveOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
) error {
	if err := lockOwner(ctx, tx, ownerID); err != nil {
		return err
	}
	var accountStatus string
	err := tx.QueryRow(ctx, `
SELECT account_status
FROM identity_users
WHERE id = $1
FOR SHARE`,
		ownerID,
	).Scan(&accountStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountDeleted
	}
	if err != nil {
		return ErrRepository
	}
	if accountStatus != "active" {
		return ErrAccountDeleted
	}
	var fenceExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM agent_memory_deletion_fences
    WHERE owner_user_id = $1
)`,
		ownerID,
	).Scan(&fenceExists); err != nil {
		return ErrRepository
	}
	if fenceExists {
		return ErrAccountDeleted
	}
	return nil
}

func lockOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
) error {
	if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"agent_memory:"+ownerID,
	); err != nil {
		return ErrRepository
	}
	return nil
}

func requireOwnedMatter(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	matterID string,
) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM matters
    WHERE id = $1 AND owner_user_id = $2
)`,
		matterID,
		ownerID,
	).Scan(&exists); err != nil {
		return ErrRepository
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func insertSource(
	ctx context.Context,
	tx pgx.Tx,
	sourceID string,
	ownerID string,
	memoryID string,
	source SourceInput,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO agent_memory_sources (
    id,
    owner_user_id,
    memory_id,
    source_type,
    source_id,
    source_version,
    source_checksum,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, transaction_timestamp()
)
ON CONFLICT (
    owner_user_id,
    memory_id,
    source_type,
    source_id,
    source_version,
    source_checksum
) DO NOTHING`,
		sourceID,
		ownerID,
		memoryID,
		source.Type,
		source.SourceID,
		source.Version,
		source.Checksum[:],
	); err != nil {
		return mapPostgresError(err)
	}
	return nil
}

func classifyMutationMiss(
	ctx context.Context,
	tx pgx.Tx,
	ownerID string,
	memoryID string,
) error {
	var version int64
	if err := tx.QueryRow(ctx, `
SELECT version
FROM agent_memories
WHERE id = $1 AND owner_user_id = $2`,
		memoryID,
		ownerID,
	).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return ErrRepository
	}
	return ErrConflict
}

type rowScanner interface {
	Scan(...any) error
}

func scanMemory(row rowScanner) (Memory, error) {
	var item Memory
	var memoryType string
	var scope string
	var status string
	if err := row.Scan(
		&item.ID,
		&item.OwnerID,
		&memoryType,
		&item.CanonicalKey,
		&item.Content,
		&scope,
		&item.MatterID,
		&status,
		&item.Version,
		&item.PolicyVersion,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InactivatedAt,
	); err != nil {
		return Memory{}, err
	}
	item.Type = Type(memoryType)
	item.Scope = ScopeType(scope)
	item.Status = Status(status)
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	normalizeTime(&item.ExpiresAt)
	normalizeTime(&item.InactivatedAt)
	if !item.Valid() {
		return Memory{}, ErrRepository
	}
	return item, nil
}

func scanSource(row rowScanner) (Source, error) {
	var item Source
	var sourceType string
	var checksum []byte
	if err := row.Scan(
		&item.ID,
		&item.OwnerID,
		&item.MemoryID,
		&sourceType,
		&item.SourceID,
		&item.Version,
		&checksum,
		&item.CreatedAt,
	); err != nil {
		return Source{}, err
	}
	if len(checksum) != len(item.Checksum) {
		return Source{}, ErrRepository
	}
	copy(item.Checksum[:], checksum)
	item.Type = SourceType(sourceType)
	item.CreatedAt = item.CreatedAt.UTC()
	if !item.Valid() {
		return Source{}, ErrRepository
	}
	return item, nil
}

func normalizeTime(value **time.Time) {
	if *value == nil {
		return
	}
	normalized := (*value).UTC()
	*value = &normalized
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validActor(actor requestcontext.Actor) bool {
	return actor.Valid() &&
		validUUID(actor.UserID) &&
		validUUID(actor.SessionID)
}

func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrNotFound
		case "23505":
			return ErrConflict
		case "22001", "22P02", "23514":
			return ErrInvalidArgument
		}
	}
	return ErrRepository
}

func rollback(ctx context.Context, tx pgx.Tx) {
	rollbackContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		rollbackTimeout,
	)
	defer cancel()
	_ = tx.Rollback(rollbackContext)
}

var _ Repository = (*PostgresRepository)(nil)
