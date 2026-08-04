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

func (r *PostgresRepository) GetCurrentIELTSSpeakingReportState(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
) (report.IELTSSpeakingReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validIdentifier(practiceSessionID) {
		return report.IELTSSpeakingReadState{}, evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return report.IELTSSpeakingReadState{}, fmt.Errorf(
			"begin IELTS Speaking report read: %w",
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
		  AND ledger.scene_type = 'IELTS_SPEAKING'
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
		scoring.IELTSSpeakingShadowStrategyRef,
		scoring.IELTSSpeakingShadowPipelineVersion,
		evaluation.SchemaVersion)
	if err != nil {
		return report.IELTSSpeakingReadState{}, fmt.Errorf(
			"find IELTS Speaking report Evaluation: %w",
			err,
		)
	}
	evaluationIDs := make([]string, 0, 2)
	for rows.Next() {
		var evaluationID string
		if err := rows.Scan(&evaluationID); err != nil {
			rows.Close()
			return report.IELTSSpeakingReadState{}, fmt.Errorf(
				"scan IELTS Speaking report Evaluation: %w",
				err,
			)
		}
		evaluationIDs = append(evaluationIDs, evaluationID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report.IELTSSpeakingReadState{}, fmt.Errorf(
			"iterate IELTS Speaking report Evaluations: %w",
			err,
		)
	}
	switch len(evaluationIDs) {
	case 0:
		return report.IELTSSpeakingReadState{}, evaluation.ErrNotFound
	case 1:
	default:
		return report.IELTSSpeakingReadState{},
			report.ErrIELTSSpeakingConfigurationConflict
	}

	value, err := selectLatest(
		ctx,
		tx,
		ownerUserID,
		evaluationIDs[0],
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) {
			return report.IELTSSpeakingReadState{},
				report.ErrIELTSSpeakingConfigurationConflict
		}
		return report.IELTSSpeakingReadState{}, err
	}
	runtime, snapshot, err := getIELTSSpeakingShadowState(
		ctx,
		tx,
		ownerUserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) {
			return report.IELTSSpeakingReadState{},
				report.ErrIELTSSpeakingConfigurationConflict
		}
		return report.IELTSSpeakingReadState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return report.IELTSSpeakingReadState{}, fmt.Errorf(
			"commit IELTS Speaking report read: %w",
			err,
		)
	}
	return report.IELTSSpeakingReadState{
		Evaluation: value,
		Runtime:    runtime,
		Snapshot:   snapshot,
	}, nil
}
