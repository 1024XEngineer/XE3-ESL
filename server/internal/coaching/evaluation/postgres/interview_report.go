package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/jackc/pgx/v5"
)

// GetLatestInterviewReportState resolves the newest ready Interview report for
// an owner. The caller supplies only the trusted owner identity; practice and
// evaluation.Evaluation identifiers remain an internal persistence concern.
func (r *PostgresRepository) GetLatestInterviewReportState(
	ctx context.Context,
	ownerUserID string,
) (report.InterviewReadState, error) {
	if r == nil || r.pool == nil || ctx == nil || !validUUID(ownerUserID) {
		return report.InterviewReadState{}, evaluation.ErrInvalidRequest
	}
	var practiceSessionID string
	err := r.pool.QueryRow(ctx, `
		SELECT result.practice_session_id
		FROM evaluation_interview_scene_results AS result
		JOIN evaluation_ledgers AS ledger
		  ON ledger.id = result.evaluation_id
		 AND ledger.owner_user_id = result.owner_user_id
		JOIN evaluation_revisions AS revision
		  ON revision.id = result.evaluation_revision_id
		 AND revision.evaluation_id = result.evaluation_id
		 AND revision.owner_user_id = result.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.revision_id = revision.id
		 AND state.evaluation_id = revision.evaluation_id
		 AND state.owner_user_id = revision.owner_user_id
		WHERE result.owner_user_id = $1
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'INTERVIEW'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $2
		  AND revision.core_4d_strategy_ref IS NULL
		  AND revision.pipeline_version = $3
		  AND revision.schema_version = $4
		  AND state.evaluation_status = 'READY'
		  AND state.is_final = false
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.owner_user_id = revision.owner_user_id
		        AND later.revision > revision.revision
		  )
		ORDER BY result.created_at DESC, result.id DESC
		LIMIT 1
	`, ownerUserID,
		scoring.InterviewShadowStrategyRef,
		scoring.InterviewShadowPipelineVersion,
		evaluation.SchemaVersion,
	).Scan(&practiceSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return report.InterviewReadState{}, evaluation.ErrNotFound
	}
	if err != nil {
		return report.InterviewReadState{}, fmt.Errorf(
			"find latest Interview report: %w",
			err,
		)
	}
	return r.GetCurrentInterviewReportState(
		ctx,
		ownerUserID,
		practiceSessionID,
	)
}

func (r *PostgresRepository) GetCurrentInterviewReportState(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
) (report.InterviewReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validIdentifier(practiceSessionID) {
		return report.InterviewReadState{}, evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return report.InterviewReadState{}, fmt.Errorf(
			"begin Interview report read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT ledger.id::text
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		WHERE ledger.owner_user_id = $1
		  AND ledger.practice_session_id = $2
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type = 'INTERVIEW'
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND revision.scene_strategy_ref = $3
		  AND revision.core_4d_strategy_ref IS NULL
		  AND revision.pipeline_version = $4
		  AND revision.schema_version = $5
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.owner_user_id = revision.owner_user_id
		        AND later.revision > revision.revision
		  )
		ORDER BY ledger.id
		LIMIT 2
	`, ownerUserID, practiceSessionID,
		scoring.InterviewShadowStrategyRef,
		scoring.InterviewShadowPipelineVersion,
		evaluation.SchemaVersion)
	if err != nil {
		return report.InterviewReadState{}, fmt.Errorf(
			"find Interview report Evaluation: %w",
			err,
		)
	}
	evaluationIDs := make([]string, 0, 2)
	for rows.Next() {
		var evaluationID string
		if err := rows.Scan(&evaluationID); err != nil {
			rows.Close()
			return report.InterviewReadState{}, fmt.Errorf(
				"scan Interview report Evaluation: %w",
				err,
			)
		}
		evaluationIDs = append(evaluationIDs, evaluationID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report.InterviewReadState{}, fmt.Errorf(
			"iterate Interview report Evaluations: %w",
			err,
		)
	}
	switch len(evaluationIDs) {
	case 0:
		return report.InterviewReadState{}, evaluation.ErrNotFound
	case 1:
	default:
		return report.InterviewReadState{},
			report.ErrInterviewConfigurationConflict
	}

	value, err := selectLatest(
		ctx,
		tx,
		ownerUserID,
		evaluationIDs[0],
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) {
			return report.InterviewReadState{},
				report.ErrInterviewConfigurationConflict
		}
		return report.InterviewReadState{}, err
	}
	runtime, snapshot, err := getInterviewShadowState(
		ctx,
		tx,
		ownerUserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) {
			return report.InterviewReadState{},
				report.ErrInterviewConfigurationConflict
		}
		return report.InterviewReadState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return report.InterviewReadState{}, fmt.Errorf(
			"commit Interview report read: %w",
			err,
		)
	}
	return report.InterviewReadState{
		Evaluation: value,
		Runtime:    runtime,
		Snapshot:   snapshot,
	}, nil
}
