package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/jackc/pgx/v5"
)

const maxReportSearchBytes = 2000

func (store *Store) GetFormalReport(
	ctx context.Context,
	userID string,
	reportID string,
) (report.StoredFormalReport, error) {
	if store == nil || store.pool == nil || ctx == nil ||
		!validUUID(userID) || !validUUID(reportID) {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return report.StoredFormalReport{}, fmt.Errorf("begin Evaluation report read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return report.StoredFormalReport{}, err
	}
	stored, err := scanCompactFormalReport(tx.QueryRow(ctx, compactFormalReportSelect+`
		WHERE evaluation.user_id = $1 AND evaluation.id = $2
		  AND evaluation.kind = 'SESSION_REPORT' AND evaluation.status = 'READY'`,
		userID, reportID))
	if errors.Is(err, pgx.ErrNoRows) {
		return report.StoredFormalReport{}, evaluation.ErrNotFound
	}
	if err != nil {
		return report.StoredFormalReport{}, fmt.Errorf("read Evaluation report: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return report.StoredFormalReport{}, fmt.Errorf("commit Evaluation report read: %w", err)
	}
	return stored, nil
}

func (store *Store) ListFormalReports(
	ctx context.Context,
	userID string,
	query report.HistoryQuery,
) (report.HistoryPage, error) {
	query.Search = strings.TrimSpace(query.Search)
	if store == nil || store.pool == nil || ctx == nil || !validUUID(userID) ||
		query.Limit < 1 || query.Limit > 50 ||
		(query.Before != nil && !query.Before.Valid()) ||
		!utf8.ValidString(query.Search) || strings.ContainsRune(query.Search, '\x00') ||
		len(query.Search) > maxReportSearchBytes {
		return report.HistoryPage{}, evaluation.ErrInvalidRequest
	}
	var beforeFinishedAt any
	var beforeID any
	if query.Before != nil {
		beforeFinishedAt = query.Before.CreatedAt.UTC()
		beforeID = query.Before.ReportID
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return report.HistoryPage{}, fmt.Errorf("begin Evaluation report list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return report.HistoryPage{}, err
	}
	rows, err := tx.Query(ctx, compactFormalReportSelect+`
		WHERE evaluation.user_id = $1
		  AND evaluation.kind = 'SESSION_REPORT' AND evaluation.status = 'READY'
		  AND ($2::timestamptz IS NULL OR evaluation.finished_at < $2::timestamptz
		       OR (evaluation.finished_at = $2::timestamptz AND evaluation.id < $3::uuid))
		  AND ($4::text = ''
		       OR strpos(lower(COALESCE(evaluation.result->>'summary', '')), lower($4)) > 0
		       OR strpos(lower(COALESCE(evaluation.result->>'scene_category', '')), lower($4)) > 0
		       OR strpos(lower(COALESCE(evaluation.result->>'practice_experience', '')), lower($4)) > 0
		       OR strpos(lower(COALESCE(evaluation.result->>'practice_mode', '')), lower($4)) > 0)
		ORDER BY evaluation.finished_at DESC, evaluation.id DESC
		LIMIT $5`, userID, beforeFinishedAt, beforeID, query.Search, query.Limit+1)
	if err != nil {
		return report.HistoryPage{}, fmt.Errorf("list Evaluation reports: %w", err)
	}
	defer rows.Close()
	items := make([]report.StoredFormalReport, 0, query.Limit+1)
	for rows.Next() {
		item, err := scanCompactFormalReport(rows)
		if err != nil {
			return report.HistoryPage{}, fmt.Errorf("scan Evaluation report: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return report.HistoryPage{}, fmt.Errorf("iterate Evaluation reports: %w", err)
	}
	hasMore := len(items) > query.Limit
	if hasMore {
		items = items[:query.Limit]
	}
	if err := tx.Commit(ctx); err != nil {
		return report.HistoryPage{}, fmt.Errorf("commit Evaluation report list: %w", err)
	}
	return report.HistoryPage{Items: items, HasMore: hasMore}, nil
}

const compactFormalReportSelect = `SELECT
	evaluation.id::text,
	evaluation.user_id::text,
	evaluation.source_id::text,
	evaluation.result,
	evaluation.finished_at
	FROM evaluations AS evaluation `

func scanCompactFormalReport(row rowScanner) (report.StoredFormalReport, error) {
	var stored report.StoredFormalReport
	var payload []byte
	if err := row.Scan(
		&stored.ReportID,
		&stored.OwnerUserID,
		&stored.PracticeSessionID,
		&payload,
		&stored.CreatedAt,
	); err != nil {
		return report.StoredFormalReport{}, err
	}
	// One Evaluation is one report; no second report identity exists.
	stored.EvaluationID = stored.ReportID
	if err := evaluation.DecodeStrict(payload, &stored.Report); err != nil {
		return report.StoredFormalReport{}, err
	}
	stored.CreatedAt = stored.CreatedAt.UTC()
	if !stored.Valid() {
		return report.StoredFormalReport{}, evaluation.ErrInvalidRequest
	}
	return stored, nil
}
