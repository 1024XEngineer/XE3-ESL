package evidence

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
	identifierPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
)

type PostgresRepository struct {
	pool                     *pgxpool.Pool
	afterEvidenceSourceFence func()
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

type queryable interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func validIdentifier(value string) bool {
	return identifierPattern.MatchString(strings.TrimSpace(value))
}

func validActor(actor requestcontext.Actor) bool {
	return actor.Valid() && validUUID(actor.UserID)
}

func validScope(scope evaluation.Scope) bool {
	return scope == evaluation.ScopeTurn || scope == evaluation.ScopeSession
}

func validSceneType(sceneType evaluation.SceneType) bool {
	switch sceneType {
	case evaluation.SceneIELTSSpeaking,
		evaluation.SceneInterview,
		evaluation.SceneOverseasDaily,
		evaluation.SceneOverseasWorkplace:
		return true
	default:
		return false
	}
}

func lockActiveOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
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
		  )
		FOR SHARE OF owner
	`, ownerUserID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return evaluation.ErrAccountUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock Evaluation evidence owner: %w", err)
	}
	return nil
}
