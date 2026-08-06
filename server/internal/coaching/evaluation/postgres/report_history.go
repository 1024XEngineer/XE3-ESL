package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/jackc/pgx/v5"
)

const maxFormalReportSearchBytes = 2000

func (r *PostgresRepository) GetFormalReport(
	ctx context.Context,
	ownerUserID string,
	reportID string,
) (report.StoredFormalReport, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(reportID) {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return report.StoredFormalReport{}, fmt.Errorf(
			"begin formal report read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		return report.StoredFormalReport{}, err
	}
	stored, err := scanStoredFormalReport(tx.QueryRow(ctx, formalReportSelect+`
		WHERE report.owner_user_id = $1
		  AND report.report_id = $2
		  AND state.evaluation_status = 'READY'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.owner_user_id = revision.owner_user_id
		        AND later.revision > revision.revision
		  )
	`, ownerUserID, reportID))
	if errors.Is(err, pgx.ErrNoRows) {
		return report.StoredFormalReport{}, evaluation.ErrNotFound
	}
	if err != nil {
		return report.StoredFormalReport{}, fmt.Errorf("read formal report: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return report.StoredFormalReport{}, fmt.Errorf(
			"commit formal report read: %w",
			err,
		)
	}
	return stored, nil
}

func (r *PostgresRepository) ListFormalReports(
	ctx context.Context,
	ownerUserID string,
	query report.HistoryQuery,
) (report.HistoryPage, error) {
	query.Search = strings.TrimSpace(query.Search)
	query.PracticeSessionID = strings.TrimSpace(query.PracticeSessionID)
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || query.Limit < 1 || query.Limit > 50 ||
		(query.Before != nil && !query.Before.Valid()) ||
		!utf8.ValidString(query.Search) ||
		strings.ContainsRune(query.Search, '\x00') ||
		len(query.Search) > maxFormalReportSearchBytes ||
		(query.PracticeSessionID != "" &&
			!validIdentifier(query.PracticeSessionID)) {
		return report.HistoryPage{}, evaluation.ErrInvalidRequest
	}
	var beforeCreatedAt any
	var beforeReportID any
	if query.Before != nil {
		beforeCreatedAt = query.Before.CreatedAt.UTC()
		beforeReportID = query.Before.ReportID
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return report.HistoryPage{}, fmt.Errorf(
			"begin formal report history read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		return report.HistoryPage{}, err
	}
	rows, err := tx.Query(ctx, formalReportSelect+`
		WHERE report.owner_user_id = $1
		  AND state.evaluation_status = 'READY'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = revision.evaluation_id
		        AND later.owner_user_id = revision.owner_user_id
		        AND later.revision > revision.revision
		  )
		  AND (
		      $2::timestamptz IS NULL
		      OR report.created_at < $2::timestamptz
		      OR (
		          report.created_at = $2::timestamptz
		          AND report.report_id < $3::uuid
		      )
		  )
		  AND (
		      $4::text = ''
		      OR strpos(lower(report.report_payload::text), lower($4)) > 0
		  )
		  AND (
		      $5::text = ''
		      OR report.practice_session_id = $5
		  )
		ORDER BY report.created_at DESC, report.report_id DESC
		LIMIT $6
	`, ownerUserID, beforeCreatedAt, beforeReportID,
		query.Search, query.PracticeSessionID, query.Limit+1)
	if err != nil {
		return report.HistoryPage{}, fmt.Errorf(
			"list formal report history: %w",
			err,
		)
	}
	defer rows.Close()
	items := make([]report.StoredFormalReport, 0, query.Limit+1)
	for rows.Next() {
		item, err := scanStoredFormalReport(rows)
		if err != nil {
			return report.HistoryPage{}, fmt.Errorf(
				"scan formal report history: %w",
				err,
			)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return report.HistoryPage{}, fmt.Errorf(
			"iterate formal report history: %w",
			err,
		)
	}
	rows.Close()
	hasMore := len(items) > query.Limit
	if hasMore {
		items = items[:query.Limit]
	}
	if err := tx.Commit(ctx); err != nil {
		return report.HistoryPage{}, fmt.Errorf(
			"commit formal report history read: %w",
			err,
		)
	}
	return report.HistoryPage{Items: items, HasMore: hasMore}, nil
}

const formalReportSelect = `
	SELECT
		report.report_id::text,
		report.evaluation_id::text,
		report.evaluation_revision_id::text,
		report.owner_user_id::text,
		report.practice_session_id,
		report.revision,
		report.report_payload,
		report.created_at
	FROM evaluation_formal_reports AS report
	JOIN evaluation_revisions AS revision
	  ON revision.id = report.evaluation_revision_id
	 AND revision.evaluation_id = report.evaluation_id
	 AND revision.owner_user_id = report.owner_user_id
	JOIN evaluation_revision_states AS state
	  ON state.revision_id = revision.id
	 AND state.evaluation_id = revision.evaluation_id
	 AND state.owner_user_id = revision.owner_user_id
`

func scanStoredFormalReport(row rowScanner) (report.StoredFormalReport, error) {
	var (
		stored  report.StoredFormalReport
		payload []byte
	)
	if err := row.Scan(
		&stored.ReportID,
		&stored.EvaluationID,
		&stored.EvaluationRevisionID,
		&stored.OwnerUserID,
		&stored.PracticeSessionID,
		&stored.Revision,
		&payload,
		&stored.CreatedAt,
	); err != nil {
		return report.StoredFormalReport{}, err
	}
	if err := json.Unmarshal(payload, &stored.Report); err != nil {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	stored.CreatedAt = stored.CreatedAt.UTC()
	if !stored.Valid() {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	return stored, nil
}
