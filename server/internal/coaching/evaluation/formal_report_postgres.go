package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const maxFormalReportSearchBytes = 2000

type FormalReportHistoryBoundary struct {
	CreatedAt time.Time
	ReportID  string
}

func (boundary FormalReportHistoryBoundary) valid() bool {
	return !boundary.CreatedAt.IsZero() && validUUID(boundary.ReportID)
}

type FormalReportHistoryQuery struct {
	Limit             int
	Before            *FormalReportHistoryBoundary
	Search            string
	PracticeSessionID string
}

type FormalReportHistoryPage struct {
	Items   []StoredFormalReport
	HasMore bool
}

func (r *PostgresRepository) GetFormalReport(
	ctx context.Context,
	ownerUserID string,
	reportID string,
) (StoredFormalReport, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(reportID) {
		return StoredFormalReport{}, ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return StoredFormalReport{}, fmt.Errorf(
			"begin formal report read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		return StoredFormalReport{}, err
	}
	report, err := scanStoredFormalReport(tx.QueryRow(ctx, formalReportSelect+`
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
		return StoredFormalReport{}, ErrNotFound
	}
	if err != nil {
		return StoredFormalReport{}, fmt.Errorf("read formal report: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StoredFormalReport{}, fmt.Errorf(
			"commit formal report read: %w",
			err,
		)
	}
	return report, nil
}

func (r *PostgresRepository) ListFormalReports(
	ctx context.Context,
	ownerUserID string,
	query FormalReportHistoryQuery,
) (FormalReportHistoryPage, error) {
	query.Search = strings.TrimSpace(query.Search)
	query.PracticeSessionID = strings.TrimSpace(query.PracticeSessionID)
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || query.Limit < 1 || query.Limit > 50 ||
		(query.Before != nil && !query.Before.valid()) ||
		!utf8.ValidString(query.Search) ||
		strings.ContainsRune(query.Search, '\x00') ||
		len(query.Search) > maxFormalReportSearchBytes ||
		(query.PracticeSessionID != "" &&
			!validIdentifier(query.PracticeSessionID)) {
		return FormalReportHistoryPage{}, ErrInvalidRequest
	}
	var beforeCreatedAt any
	var beforeReportID any
	if query.Before != nil {
		beforeCreatedAt = query.Before.CreatedAt.UTC()
		beforeReportID = query.Before.ReportID
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FormalReportHistoryPage{}, fmt.Errorf(
			"begin formal report history read: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		return FormalReportHistoryPage{}, err
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
		return FormalReportHistoryPage{}, fmt.Errorf(
			"list formal report history: %w",
			err,
		)
	}
	defer rows.Close()
	items := make([]StoredFormalReport, 0, query.Limit+1)
	for rows.Next() {
		item, err := scanStoredFormalReport(rows)
		if err != nil {
			return FormalReportHistoryPage{}, fmt.Errorf(
				"scan formal report history: %w",
				err,
			)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return FormalReportHistoryPage{}, fmt.Errorf(
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
		return FormalReportHistoryPage{}, fmt.Errorf(
			"commit formal report history read: %w",
			err,
		)
	}
	return FormalReportHistoryPage{Items: items, HasMore: hasMore}, nil
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

func scanStoredFormalReport(row rowScanner) (StoredFormalReport, error) {
	var (
		report  StoredFormalReport
		payload []byte
	)
	if err := row.Scan(
		&report.ReportID,
		&report.EvaluationID,
		&report.EvaluationRevisionID,
		&report.OwnerUserID,
		&report.PracticeSessionID,
		&report.Revision,
		&payload,
		&report.CreatedAt,
	); err != nil {
		return StoredFormalReport{}, err
	}
	if err := json.Unmarshal(payload, &report.Report); err != nil {
		return StoredFormalReport{}, ErrInvalidRequest
	}
	report.CreatedAt = report.CreatedAt.UTC()
	if !report.Valid() {
		return StoredFormalReport{}, ErrInvalidRequest
	}
	return report, nil
}
