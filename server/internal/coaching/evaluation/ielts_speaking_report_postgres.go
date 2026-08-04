package evaluation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type IELTSSpeakingReportReadState struct {
	Evaluation Evaluation
	Runtime    IELTSSpeakingShadowReadState
	Snapshot   *EvidenceSnapshot
}

type IELTSSpeakingReportIndexBoundary struct {
	UpdatedAt    time.Time
	EvaluationID string
}

func (boundary IELTSSpeakingReportIndexBoundary) valid() bool {
	return !boundary.UpdatedAt.IsZero() &&
		validUUID(boundary.EvaluationID)
}

type IELTSSpeakingReportIndexEntry struct {
	SceneType            SceneType
	PracticeSessionID    string
	EvaluationID         string
	EvaluationRevisionID string
	Revision             int
	EvaluationStatus     Status
	IsFinal              bool
	Title                string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (entry IELTSSpeakingReportIndexEntry) Valid() bool {
	return (entry.SceneType == "" || entry.SceneType == SceneIELTSSpeaking || entry.SceneType == SceneInterview) &&
		validIdentifier(entry.PracticeSessionID) &&
		validUUID(entry.EvaluationID) &&
		validUUID(entry.EvaluationRevisionID) &&
		entry.Revision >= 1 &&
		validIELTSSpeakingReportIndexStatus(
			entry.EvaluationStatus,
		) &&
		!entry.IsFinal &&
		!entry.CreatedAt.IsZero() &&
		!entry.UpdatedAt.Before(entry.CreatedAt)
}

func validIELTSSpeakingReportIndexStatus(status Status) bool {
	switch status {
	case StatusQueued, StatusRunning, StatusReady, StatusFailed:
		return true
	default:
		return false
	}
}

type IELTSSpeakingReportIndexPage struct {
	Items   []IELTSSpeakingReportIndexEntry
	HasMore bool
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

func (r *PostgresRepository) ListCurrentIELTSSpeakingReportIndex(
	ctx context.Context,
	ownerUserID string,
	boundary *IELTSSpeakingReportIndexBoundary,
	limit int,
) (IELTSSpeakingReportIndexPage, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) ||
		limit < 1 || limit > 100 ||
		(boundary != nil && !boundary.valid()) {
		return IELTSSpeakingReportIndexPage{}, ErrInvalidRequest
	}
	var beforeUpdatedAt any
	var beforeEvaluationID any
	if boundary != nil {
		beforeUpdatedAt = boundary.UpdatedAt.UTC()
		beforeEvaluationID = boundary.EvaluationID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			ledger.scene_type,
			ledger.practice_session_id,
			ledger.id::text,
			revision.id::text,
			revision.revision,
			state.evaluation_status,
			state.is_final,
			ledger.created_at,
			state.updated_at,
			COALESCE(goal.title, '') AS title
		FROM evaluation_ledgers AS ledger
		JOIN evaluation_revisions AS revision
		  ON revision.evaluation_id = ledger.id
		 AND revision.owner_user_id = ledger.owner_user_id
		JOIN evaluation_revision_states AS state
		  ON state.evaluation_id = ledger.id
		 AND state.revision_id = revision.id
		 AND state.owner_user_id = ledger.owner_user_id
		LEFT JOIN practice_sessions AS practice_session
		  ON practice_session.session_id = ledger.practice_session_id
		 AND practice_session.owner_user_id = ledger.owner_user_id
		LEFT JOIN preparation_practice_plan_revisions AS plan_revision
		  ON plan_revision.owner_user_id = practice_session.owner_user_id
		 AND plan_revision.plan_id = practice_session.plan_id
		 AND plan_revision.revision = practice_session.plan_revision
		LEFT JOIN coaching_goals AS goal
		  ON goal.goal_id = plan_revision.goal_id
		 AND goal.owner_user_id = plan_revision.owner_user_id
		WHERE ledger.owner_user_id = $1
		  AND ledger.scope = 'SESSION'
		  AND ledger.scene_type IN ('IELTS_SPEAKING', 'INTERVIEW')
		  AND revision.channels = ARRAY['SCENE']::text[]
		  AND ((ledger.scene_type = 'IELTS_SPEAKING' AND revision.scene_strategy_ref = $2)
		    OR (ledger.scene_type = 'INTERVIEW' AND revision.scene_strategy_ref = $8))
		  AND revision.core_4d_strategy_ref IS NULL
		  AND revision.pipeline_version = $3
		  AND revision.schema_version = $4
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.owner_user_id = revision.owner_user_id
		        AND later.revision > revision.revision
		  )
		  AND (
		      $5::timestamptz IS NULL
		      OR state.updated_at < $5::timestamptz
		      OR (
		          state.updated_at = $5::timestamptz
		          AND ledger.id < $6::uuid
		      )
		  )
		ORDER BY state.updated_at DESC, ledger.id DESC
		LIMIT $7
	`, ownerUserID,
		IELTSSpeakingShadowStrategyRef,
		IELTSSpeakingShadowPipelineVersion,
		SchemaVersion,
		beforeUpdatedAt,
		beforeEvaluationID,
		limit+1,
		InterviewShadowStrategyRef)
	if err != nil {
		return IELTSSpeakingReportIndexPage{}, fmt.Errorf(
			"list IELTS Speaking report index: %w",
			err,
		)
	}
	defer rows.Close()
	items := make(
		[]IELTSSpeakingReportIndexEntry,
		0,
		limit+1,
	)
	for rows.Next() {
		var item IELTSSpeakingReportIndexEntry
		if err := rows.Scan(
			&item.SceneType,
			&item.PracticeSessionID,
			&item.EvaluationID,
			&item.EvaluationRevisionID,
			&item.Revision,
			&item.EvaluationStatus,
			&item.IsFinal,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.Title,
		); err != nil {
			return IELTSSpeakingReportIndexPage{}, fmt.Errorf(
				"scan IELTS Speaking report index: %w",
				err,
			)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		if !item.Valid() {
			return IELTSSpeakingReportIndexPage{},
				ErrIELTSSpeakingShadowConfigurationConflict
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return IELTSSpeakingReportIndexPage{}, fmt.Errorf(
			"iterate IELTS Speaking report index: %w",
			err,
		)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return IELTSSpeakingReportIndexPage{
		Items:   items,
		HasMore: hasMore,
	}, nil
}
