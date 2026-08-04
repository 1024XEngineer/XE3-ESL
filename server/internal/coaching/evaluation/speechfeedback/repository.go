package speechfeedback

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errAccountUnavailable = errors.New(
		"evaluation: SpeechFeedback account unavailable",
	)
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

type queryable interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type queryer interface {
	queryable
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type rowScanner interface {
	Scan(...any) error
}

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func lockActiveIdentityUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	deletionGeneration int64,
) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT owner.account_status
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		        AND fence.deletion_generation >= $2
		  )
		FOR SHARE OF owner
	`, userID, deletionGeneration).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return errAccountUnavailable
	}
	return err
}
