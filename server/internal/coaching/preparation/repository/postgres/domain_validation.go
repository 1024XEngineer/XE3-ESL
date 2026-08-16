package postgres

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/jackc/pgx/v5"
)

var errInactiveActor = errors.New("preparation: actor is not active")

func lockActiveActor(ctx context.Context, tx pgx.Tx, userID string) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT true FROM users WHERE id=$1 AND status='active' FOR UPDATE`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInactiveActor
	}
	return err
}

func validResourceIdentifier(value string) bool { return preparation.ValidResourceIdentifier(value) }
func validAggregateID(value string) bool        { return preparation.ValidAggregateID(value) }
func validIdempotencyKey(value string) bool     { return preparation.ValidIdempotencyKey(value) }
func validJobTargetInput(value preparation.JobTargetInput) bool {
	return preparation.ValidJobTargetInput(value)
}
func validJobTargetCandidateShape(value preparation.JobTargetCandidate, source preparation.JobTargetSource) bool {
	return preparation.ValidJobTargetCandidateShape(value, source)
}
func validPlanIELTSAssignment(selection scene.SelectionSnapshot, assignment *preparation.IELTSAssignmentSnapshot) bool {
	return preparationservice.ValidPlanIELTSAssignment(selection, assignment)
}
func validPlanResourceID(value string) bool { return preparationservice.ValidPlanResourceID(value) }
