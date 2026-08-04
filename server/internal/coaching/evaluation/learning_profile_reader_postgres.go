package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (r *PostgresRepository) ReadLearningProfile(
	ctx context.Context,
	ownerUserID string,
	query LearningProfileQuery,
) ([]LearningProfileDimension, error) {
	query.GoalID = strings.TrimSpace(query.GoalID)
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || query.Limit < 1 || query.Limit > 16 ||
		(query.GoalID != "" && !validUUID(query.GoalID)) ||
		(query.SceneType != "" && !validSceneType(query.SceneType)) {
		return nil, ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin Learning Profile read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockActiveOwner(ctx, tx, ownerUserID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT
			profile.dimension_key,
			profile.score_scale,
			profile.estimated_value::float8,
			profile.confidence::float8,
			profile.trend,
			profile.recurring_issues,
			profile.source_evaluation_refs,
			profile.strategy_version,
			profile.updated_at
		FROM learning_profile_dimensions AS profile
		WHERE profile.owner_user_id = $1
		  AND (
		      ($2::uuid IS NULL AND $3::text = '')
		      OR EXISTS (
		          SELECT 1
		          FROM learning_profile_contributions AS contribution
		          JOIN evaluation_ledgers AS ledger
		            ON ledger.id = contribution.evaluation_id
		           AND ledger.owner_user_id = contribution.owner_user_id
		          JOIN evaluation_revisions AS revision
		            ON revision.id = contribution.evaluation_revision_id
		           AND revision.evaluation_id = contribution.evaluation_id
		           AND revision.owner_user_id = contribution.owner_user_id
		          JOIN evaluation_revision_states AS state
		            ON state.revision_id = revision.id
		           AND state.evaluation_id = revision.evaluation_id
		           AND state.owner_user_id = revision.owner_user_id
		          LEFT JOIN practice_sessions AS practice_session
		            ON practice_session.session_id = ledger.practice_session_id
		           AND practice_session.owner_user_id = ledger.owner_user_id
		          LEFT JOIN preparation_practice_plan_revisions AS plan_revision
		            ON plan_revision.owner_user_id = practice_session.owner_user_id
		           AND plan_revision.plan_id = practice_session.plan_id
		           AND plan_revision.revision = practice_session.plan_revision
		          WHERE contribution.owner_user_id = profile.owner_user_id
		            AND contribution.dimension_key = profile.dimension_key
		            AND state.evaluation_status = 'READY'
		            AND NOT EXISTS (
		                SELECT 1
		                FROM evaluation_revisions AS later
		                WHERE later.evaluation_id = revision.evaluation_id
		                  AND later.owner_user_id = revision.owner_user_id
		                  AND later.revision > revision.revision
		            )
		            AND ($2::uuid IS NULL OR plan_revision.goal_id = $2::uuid)
		            AND ($3::text = '' OR ledger.scene_type = $3::text)
		      )
		  )
		ORDER BY profile.updated_at DESC, profile.dimension_key
		LIMIT $4
	`, ownerUserID, nullableUUID(query.GoalID), query.SceneType, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("read Learning Profile: %w", err)
	}
	defer rows.Close()
	dimensions := make([]LearningProfileDimension, 0, query.Limit)
	for rows.Next() {
		var (
			dimension   LearningProfileDimension
			issuesJSON  []byte
			sourcesJSON []byte
		)
		if err := rows.Scan(
			&dimension.Key,
			&dimension.Scale,
			&dimension.EstimatedValue,
			&dimension.Confidence,
			&dimension.Trend,
			&issuesJSON,
			&sourcesJSON,
			&dimension.StrategyVersion,
			&dimension.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan Learning Profile: %w", err)
		}
		if json.Unmarshal(issuesJSON, &dimension.RecurringIssues) != nil ||
			json.Unmarshal(sourcesJSON, &dimension.SourceEvaluations) != nil {
			return nil, ErrInvalidRequest
		}
		dimension.UpdatedAt = dimension.UpdatedAt.UTC()
		for index := range dimension.RecurringIssues {
			dimension.RecurringIssues[index].LastSeen =
				dimension.RecurringIssues[index].LastSeen.UTC()
		}
		for index := range dimension.SourceEvaluations {
			dimension.SourceEvaluations[index].CreatedAt =
				dimension.SourceEvaluations[index].CreatedAt.UTC()
		}
		if !dimension.Valid() {
			return nil, ErrInvalidRequest
		}
		dimensions = append(dimensions, dimension)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Learning Profile: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Learning Profile read: %w", err)
	}
	return dimensions, nil
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
