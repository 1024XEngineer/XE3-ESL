package matter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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

func (r *PostgresRepository) Create(
	ctx context.Context,
	ownerID string,
	title string,
) (Matter, error) {
	matterID, err := r.ids.NewID()
	if err != nil {
		return Matter{}, ErrRepository
	}
	var result Matter
	var status string
	err = r.database.QueryRow(ctx, `
INSERT INTO matters (
    id,
    owner_user_id,
    title,
    status,
    version,
    created_at,
    updated_at
) VALUES ($1, $2, $3, 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING
    id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at`,
		matterID,
		ownerID,
		title,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&status,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return Matter{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func (r *PostgresRepository) CreateIdempotent(
	ctx context.Context,
	ownerID string,
	requestID string,
	title string,
) (Matter, error) {
	fingerprint := sha256.Sum256([]byte(title))
	if replay, found, err := readCreateReplay(
		ctx,
		r.database,
		ownerID,
		requestID,
		fingerprint[:],
	); err != nil || found {
		return replay, err
	}

	matterID, err := r.ids.NewID()
	if err != nil {
		return Matter{}, ErrRepository
	}
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return Matter{}, ErrRepository
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	result, err := insertMatter(ctx, transaction, matterID, ownerID, title)
	if err != nil {
		return Matter{}, err
	}
	_, err = transaction.Exec(ctx, `
	INSERT INTO matter_agent_create_requests (
	    owner_user_id,
	    request_id,
	    payload_fingerprint,
	    matter_id,
	    created_at
	) VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)`,
		ownerID,
		requestID,
		fingerprint[:],
		result.ID,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
				return Matter{}, ErrRepository
			}
			replay, found, replayErr := readCreateReplay(
				ctx,
				r.database,
				ownerID,
				requestID,
				fingerprint[:],
			)
			if replayErr != nil {
				return Matter{}, replayErr
			}
			if !found {
				return Matter{}, ErrRepository
			}
			return replay, nil
		}
		return Matter{}, mapPostgresError(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Matter{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) ListOwned(
	ctx context.Context,
	ownerID string,
) ([]Matter, error) {
	rows, err := r.database.Query(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at
FROM matters
WHERE owner_user_id = $1
ORDER BY updated_at DESC, id DESC`,
		ownerID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	result := make([]Matter, 0)
	for rows.Next() {
		var item Matter
		var status string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.Title,
			&status,
			&item.Version,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, ErrRepository
		}
		item.Status = Status(status)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) SearchOwned(
	ctx context.Context,
	ownerID string,
	query SearchQuery,
) ([]Matter, error) {
	rows, err := r.database.Query(ctx, `
	SELECT
	    id::text,
	    owner_user_id::text,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at
	FROM matters
	WHERE owner_user_id = $1
	  AND strpos(lower(title), lower($2)) > 0
	ORDER BY updated_at DESC, id DESC
	LIMIT $3`,
		ownerID,
		query.Query,
		query.Limit,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	return scanMatters(rows)
}

func (r *PostgresRepository) FindOwned(
	ctx context.Context,
	ownerID string,
	matterID string,
) (Matter, error) {
	var result Matter
	var status string
	err := r.database.QueryRow(ctx, `
SELECT
    id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at
FROM matters
WHERE id = $1 AND owner_user_id = $2`,
		matterID,
		ownerID,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&status,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return Matter{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func (r *PostgresRepository) UpdateStatus(
	ctx context.Context,
	ownerID string,
	matterID string,
	expectedVersion int64,
	status Status,
) (Matter, error) {
	var result Matter
	var persistedStatus string
	err := r.database.QueryRow(ctx, `
UPDATE matters
SET
    status = $4,
    version = version + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1
  AND owner_user_id = $2
  AND version = $3
RETURNING
    id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at`,
		matterID,
		ownerID,
		expectedVersion,
		status,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&persistedStatus,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, findErr := r.FindOwned(ctx, ownerID, matterID); findErr != nil {
			return Matter{}, findErr
		}
		return Matter{}, ErrConflict
	}
	if err != nil {
		return Matter{}, mapPostgresError(err)
	}
	result.Status = Status(persistedStatus)
	return result, nil
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
		}
	}
	return ErrRepository
}

type matterQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertMatter(
	ctx context.Context,
	database matterQueryRow,
	matterID string,
	ownerID string,
	title string,
) (Matter, error) {
	var result Matter
	var status string
	err := database.QueryRow(ctx, `
	INSERT INTO matters (
	    id,
	    owner_user_id,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at
	) VALUES ($1, $2, $3, 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	RETURNING
	    id::text,
	    owner_user_id::text,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at`,
		matterID,
		ownerID,
		title,
	).Scan(
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&status,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return Matter{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func readCreateReplay(
	ctx context.Context,
	database matterQueryRow,
	ownerID string,
	requestID string,
	fingerprint []byte,
) (Matter, bool, error) {
	var result Matter
	var persistedFingerprint []byte
	var status string
	err := database.QueryRow(ctx, `
	SELECT
	    requests.payload_fingerprint,
	    matters.id::text,
	    matters.owner_user_id::text,
	    matters.title,
	    matters.status,
	    matters.version,
	    matters.created_at,
	    matters.updated_at
	FROM matter_agent_create_requests AS requests
	JOIN matters
	  ON matters.id = requests.matter_id
	 AND matters.owner_user_id = requests.owner_user_id
	WHERE requests.owner_user_id = $1
	  AND requests.request_id = $2`,
		ownerID,
		requestID,
	).Scan(
		&persistedFingerprint,
		&result.ID,
		&result.OwnerID,
		&result.Title,
		&status,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Matter{}, false, nil
	}
	if err != nil {
		return Matter{}, false, ErrRepository
	}
	if !bytes.Equal(persistedFingerprint, fingerprint) {
		return Matter{}, true, ErrConflict
	}
	result.Status = Status(status)
	return result, true, nil
}

func scanMatters(rows pgx.Rows) ([]Matter, error) {
	result := make([]Matter, 0)
	for rows.Next() {
		var item Matter
		var status string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerID,
			&item.Title,
			&status,
			&item.Version,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, ErrRepository
		}
		item.Status = Status(status)
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return result, nil
}
