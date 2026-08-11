package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *PostgresRepository) GetCurrentSessionReportState(
	ctx context.Context,
	ownerUserID string,
	practiceSessionID string,
) (report.SessionReportReadState, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validIdentifier(practiceSessionID) {
		return report.SessionReportReadState{}, evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return report.SessionReportReadState{}, fmt.Errorf(
			"begin session report read: %w", err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		mode              practice.PracticeMode
		policyRef         string
		sessionVersion    int
		planRevision      int
		completionVersion int
		sessionSnapshotID string
		snapshotDocument  []byte
		handoffStatus     string
		handoffFailure    pgtype.Text
		handoffRetryable  pgtype.Bool
	)
	err = tx.QueryRow(ctx, `
		SELECT
			session.practice_mode,
			session.evaluation_policy_ref,
			session.version,
			session.plan_revision,
			completed.session_version,
			session.snapshot_id,
			snapshot.snapshot_document,
			completed.delivery_status,
			completed.failure_code,
			completed.failure_retryable
		FROM practice_sessions AS session
		JOIN practice_session_snapshots AS snapshot
		  ON snapshot.owner_user_id = session.owner_user_id
		 AND snapshot.session_id = session.session_id
		 AND snapshot.snapshot_id = session.snapshot_id
		JOIN practice_completed AS completed
		  ON completed.owner_user_id = session.owner_user_id
		 AND completed.session_id = session.session_id
		JOIN identity_users AS owner
		  ON owner.id = session.owner_user_id
		LEFT JOIN practice_deletion_fences AS fence
		  ON fence.owner_user_id = session.owner_user_id
		WHERE session.owner_user_id = $1
		  AND session.session_id = $2
		  AND session.status = 'completed'
		  AND session.practice_experience = 'IELTS_SPEAKING'
		  AND session.scene_category = 'IELTS_SPEAKING'
		  AND owner.account_status = 'active'
		  AND fence.owner_user_id IS NULL
	`, ownerUserID, practiceSessionID).Scan(
		&mode,
		&policyRef,
		&sessionVersion,
		&planRevision,
		&completionVersion,
		&sessionSnapshotID,
		&snapshotDocument,
		&handoffStatus,
		&handoffFailure,
		&handoffRetryable,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return report.SessionReportReadState{}, evaluation.ErrNotFound
	}
	if err != nil {
		return report.SessionReportReadState{}, fmt.Errorf(
			"read session report authority: %w", err,
		)
	}

	var snapshot practice.SessionSnapshot
	if json.Unmarshal(snapshotDocument, &snapshot) != nil ||
		snapshot.ID != sessionSnapshotID ||
		snapshot.SessionID != practiceSessionID ||
		snapshot.PlanRevision != planRevision ||
		snapshot.Experience != practice.PracticeExperienceIELTSSpeaking ||
		string(snapshot.Category) != "IELTS_SPEAKING" ||
		snapshot.PracticeMode != mode ||
		sessionVersion != completionVersion ||
		!practice.ValidIELTSAssignment(
			snapshot.IELTSAssignment,
			mode,
			snapshot.SceneSelection.Scene.Prompt.TurnBlueprints,
		) {
		return report.SessionReportReadState{},
			report.ErrSessionReportConfigurationConflict
	}
	sections, strategyRef, pipelineVersion, spec, expectedPolicy, ok :=
		ieltsSessionReportAuthority(mode, snapshot.IELTSAssignment)
	if !ok || policyRef != expectedPolicy {
		return report.SessionReportReadState{},
			report.ErrSessionReportConfigurationConflict
	}
	state := report.SessionReportReadState{
		PracticeMode:      string(mode),
		AvailableSections: sections,
		Status:            evaluation.StatusQueued,
	}

	evaluationIDs, err := findSessionReportEvaluationIDs(
		ctx,
		tx,
		ownerUserID,
		practiceSessionID,
		strategyRef,
		pipelineVersion,
	)
	if err != nil {
		return report.SessionReportReadState{}, err
	}
	switch len(evaluationIDs) {
	case 0:
		switch handoffStatus {
		case "PENDING", "RUNNING":
		case "FAILED":
			if !handoffFailure.Valid || !handoffRetryable.Valid {
				return report.SessionReportReadState{},
					report.ErrSessionReportConfigurationConflict
			}
			state.Status = evaluation.StatusFailed
			state.Failure = &report.SessionReportFailure{
				Code: handoffFailure.String, Retryable: handoffRetryable.Bool,
			}
		default:
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return report.SessionReportReadState{}, fmt.Errorf(
				"commit queued session report read: %w", err,
			)
		}
		return state, nil
	case 1:
	default:
		return report.SessionReportReadState{},
			report.ErrSessionReportConfigurationConflict
	}

	value, err := selectLatest(ctx, tx, ownerUserID, evaluationIDs[0])
	if err != nil {
		return report.SessionReportReadState{}, err
	}
	runtime, _, err := getDurableSceneJobState(
		ctx,
		tx,
		spec,
		ownerUserID,
		value.ID,
		value.Revision.ID,
	)
	if err != nil {
		if errors.Is(err, evaluation.ErrNotFound) ||
			errors.Is(err, scoring.ErrRuntimeConfigurationConflict) {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
		return report.SessionReportReadState{}, err
	}
	state.Evaluation = &value
	state.Status = value.Revision.Status
	switch runtime.ModuleStatus {
	case durableSceneJobPending:
		if state.Status != evaluation.StatusValidating &&
			state.Status != evaluation.StatusQueued &&
			state.Status != evaluation.StatusRunning {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
	case durableSceneJobRunning:
		if state.Status != evaluation.StatusRunning {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
	case durableSceneJobReady:
		if state.Status != evaluation.StatusReady {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
		stored, readErr := scanStoredFormalReport(tx.QueryRow(
			ctx,
			formalReportSelect+`
				WHERE report.owner_user_id = $1
				  AND report.evaluation_id = $2
				  AND report.evaluation_revision_id = $3
				  AND state.evaluation_status = 'READY'
			`,
			ownerUserID,
			value.ID,
			value.Revision.ID,
		))
		if readErr != nil || stored.PracticeSessionID != practiceSessionID ||
			stored.Report.PracticeMode != string(mode) {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
		state.FormalReport = &stored
	case durableSceneJobFailed:
		if state.Status != evaluation.StatusFailed || runtime.Failure == nil {
			return report.SessionReportReadState{},
				report.ErrSessionReportConfigurationConflict
		}
		state.Failure = &report.SessionReportFailure{
			Code: runtime.Failure.Code,
		}
	default:
		return report.SessionReportReadState{},
			report.ErrSessionReportConfigurationConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return report.SessionReportReadState{}, fmt.Errorf(
			"commit session report read: %w", err,
		)
	}
	return state, nil
}

func ieltsSessionReportAuthority(
	mode practice.PracticeMode,
	assignment *practice.IELTSAssignment,
) ([]string, string, string, durableSceneJobSpec, string, bool) {
	if assignment == nil {
		return nil, "", "", durableSceneJobSpec{}, "", false
	}
	sections := make([]string, len(assignment.Parts))
	for index, part := range assignment.Parts {
		sections[index] = string(part.Part)
	}
	if mode == practice.PracticeModeFullMock {
		return sections,
			scoring.IELTSSpeakingShadowStrategyRef,
			scoring.IELTSSpeakingShadowPipelineVersion,
			ieltsDurableSceneJobSpec,
			scoring.IELTSSpeakingFullMockEvaluationPolicyRef,
			true
	}
	spec, ok := generalSceneDurableJobSpec(evaluation.SceneIELTSSpeaking)
	if !ok {
		return nil, "", "", durableSceneJobSpec{}, "", false
	}
	return sections,
		scoring.GeneralSceneStrategyRef,
		scoring.GeneralScenePipelineVersion,
		spec,
		scoring.IELTSSpeakingPracticeEvaluationPolicyRef,
		true
}

func findSessionReportEvaluationIDs(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	practiceSessionID string,
	strategyRef string,
	pipelineVersion string,
) ([]string, error) {
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
	`, ownerUserID, practiceSessionID, strategyRef, pipelineVersion,
		evaluation.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("find session report Evaluation: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session report Evaluation: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session report Evaluations: %w", err)
	}
	return ids, nil
}
