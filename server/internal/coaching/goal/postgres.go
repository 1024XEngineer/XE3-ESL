package goal

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
) (Goal, error) {
	goalID, err := r.ids.NewID()
	if err != nil {
		return Goal{}, ErrRepository
	}
	var result Goal
	var status string
	err = r.database.QueryRow(ctx, `
INSERT INTO coaching_goals (
    goal_id,
    owner_user_id,
    title,
    status,
    version,
    created_at,
    updated_at
) VALUES ($1, $2, $3, 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
RETURNING
    goal_id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at`,
		goalID,
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
		return Goal{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func (r *PostgresRepository) CreateIdempotent(
	ctx context.Context,
	ownerID string,
	requestID string,
	title string,
) (Goal, error) {
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

	goalID, err := r.ids.NewID()
	if err != nil {
		return Goal{}, ErrRepository
	}
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return Goal{}, ErrRepository
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	result, err := insertGoal(ctx, transaction, goalID, ownerID, title)
	if err != nil {
		return Goal{}, err
	}
	_, err = transaction.Exec(ctx, `
	INSERT INTO goal_agent_create_requests (
	    owner_user_id,
	    request_id,
	    payload_fingerprint,
	    goal_id,
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
				return Goal{}, ErrRepository
			}
			replay, found, replayErr := readCreateReplay(
				ctx,
				r.database,
				ownerID,
				requestID,
				fingerprint[:],
			)
			if replayErr != nil {
				return Goal{}, replayErr
			}
			if !found {
				return Goal{}, ErrRepository
			}
			return replay, nil
		}
		return Goal{}, mapPostgresError(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Goal{}, ErrRepository
	}
	return result, nil
}

func (r *PostgresRepository) ListOwned(
	ctx context.Context,
	ownerID string,
) ([]Goal, error) {
	rows, err := r.database.Query(ctx, `
SELECT
    goal_id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at
FROM coaching_goals
WHERE owner_user_id = $1
ORDER BY updated_at DESC, goal_id DESC`,
		ownerID,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()

	result := make([]Goal, 0)
	for rows.Next() {
		var item Goal
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
) ([]Goal, error) {
	rows, err := r.database.Query(ctx, `
	SELECT
	    goal_id::text,
	    owner_user_id::text,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at
	FROM coaching_goals
	WHERE owner_user_id = $1
	  AND strpos(lower(title), lower($2)) > 0
	ORDER BY updated_at DESC, goal_id DESC
	LIMIT $3`,
		ownerID,
		query.Query,
		query.Limit,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	return scanGoals(rows)
}

func (r *PostgresRepository) FindOwned(
	ctx context.Context,
	ownerID string,
	goalID string,
) (Goal, error) {
	var result Goal
	var status string
	err := r.database.QueryRow(ctx, `
SELECT
    goal_id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at
FROM coaching_goals
WHERE goal_id = $1 AND owner_user_id = $2`,
		goalID,
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
		return Goal{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func (r *PostgresRepository) UpdateStatus(
	ctx context.Context,
	ownerID string,
	goalID string,
	expectedVersion int64,
	status Status,
) (Goal, error) {
	var result Goal
	var persistedStatus string
	err := r.database.QueryRow(ctx, `
UPDATE coaching_goals
SET
    status = $4,
    version = version + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE goal_id = $1
  AND owner_user_id = $2
  AND version = $3
RETURNING
    goal_id::text,
    owner_user_id::text,
    title,
    status,
    version,
    created_at,
    updated_at`,
		goalID,
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
		if _, findErr := r.FindOwned(ctx, ownerID, goalID); findErr != nil {
			return Goal{}, findErr
		}
		return Goal{}, ErrConflict
	}
	if err != nil {
		return Goal{}, mapPostgresError(err)
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

type goalQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertGoal(
	ctx context.Context,
	database goalQueryRow,
	goalID string,
	ownerID string,
	title string,
) (Goal, error) {
	var result Goal
	var status string
	err := database.QueryRow(ctx, `
	INSERT INTO coaching_goals (
	    goal_id,
	    owner_user_id,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at
	) VALUES ($1, $2, $3, 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	RETURNING
	    goal_id::text,
	    owner_user_id::text,
	    title,
	    status,
	    version,
	    created_at,
	    updated_at`,
		goalID,
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
		return Goal{}, mapPostgresError(err)
	}
	result.Status = Status(status)
	return result, nil
}

func readCreateReplay(
	ctx context.Context,
	database goalQueryRow,
	ownerID string,
	requestID string,
	fingerprint []byte,
) (Goal, bool, error) {
	var result Goal
	var persistedFingerprint []byte
	var status string
	err := database.QueryRow(ctx, `
	SELECT
	    requests.payload_fingerprint,
	    coaching_goals.goal_id::text,
	    coaching_goals.owner_user_id::text,
	    coaching_goals.title,
	    coaching_goals.status,
	    coaching_goals.version,
	    coaching_goals.created_at,
	    coaching_goals.updated_at
	FROM goal_agent_create_requests AS requests
	JOIN coaching_goals
	  ON coaching_goals.goal_id = requests.goal_id
	 AND coaching_goals.owner_user_id = requests.owner_user_id
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
		return Goal{}, false, nil
	}
	if err != nil {
		return Goal{}, false, ErrRepository
	}
	if !bytes.Equal(persistedFingerprint, fingerprint) {
		return Goal{}, true, ErrConflict
	}
	result.Status = Status(status)
	return result, true, nil
}

func scanGoals(rows pgx.Rows) ([]Goal, error) {
	result := make([]Goal, 0)
	for rows.Next() {
		var item Goal
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
