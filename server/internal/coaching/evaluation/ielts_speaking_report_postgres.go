package evaluation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type IELTSSpeakingReportReadState struct {
	Evaluation Evaluation
	Runtime    IELTSSpeakingShadowReadState
	Snapshot   *EvidenceSnapshot
}

func (r *PostgresRepository) GetCurrentIELTSSpeakingReportState(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
) (IELTSSpeakingReportReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		!validIdentifier(practiceSessionID) {
		return IELTSSpeakingReportReadState{}, ErrInvalidRequest
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return IELTSSpeakingReportReadState{}, fmt.Errorf(
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
		IELTSSpeakingShadowStrategyRef,
		IELTSSpeakingShadowPipelineVersion,
		SchemaVersion)
	if err != nil {
		return IELTSSpeakingReportReadState{}, fmt.Errorf(
			"find IELTS Speaking report Evaluation: %w",
			err,
		)
	}
	evaluationIDs := make([]string, 0, 2)
	for rows.Next() {
		var evaluationID string
		if err := rows.Scan(&evaluationID); err != nil {
			rows.Close()
			return IELTSSpeakingReportReadState{}, fmt.Errorf(
				"scan IELTS Speaking report Evaluation: %w",
				err,
			)
		}
		evaluationIDs = append(evaluationIDs, evaluationID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return IELTSSpeakingReportReadState{}, fmt.Errorf(
			"iterate IELTS Speaking report Evaluations: %w",
			err,
		)
	}
	switch len(evaluationIDs) {
	case 0:
		return IELTSSpeakingReportReadState{}, ErrNotFound
	case 1:
	default:
		return IELTSSpeakingReportReadState{},
			ErrIELTSSpeakingShadowConfigurationConflict
	}

	value, err := selectLatest(
		ctx,
		tx,
		ownerUserID,
		evaluationIDs[0],
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return IELTSSpeakingReportReadState{},
				ErrIELTSSpeakingShadowConfigurationConflict
		}
		return IELTSSpeakingReportReadState{}, err
	}
	runtime, snapshot, err := getIELTSSpeakingShadowState(
		ctx,
		tx,
		ownerUserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return IELTSSpeakingReportReadState{},
				ErrIELTSSpeakingShadowConfigurationConflict
		}
		return IELTSSpeakingReportReadState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IELTSSpeakingReportReadState{}, fmt.Errorf(
			"commit IELTS Speaking report read: %w",
			err,
		)
	}
	return IELTSSpeakingReportReadState{
		Evaluation: value,
		Runtime:    runtime,
		Snapshot:   snapshot,
	}, nil
}
