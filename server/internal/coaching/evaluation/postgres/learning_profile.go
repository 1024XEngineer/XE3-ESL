package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/learningprofile"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/jackc/pgx/v5"
)

type learningProfileContributionIssue struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type learningProfileContribution struct {
	EvaluationID         string
	EvaluationRevisionID string
	Scale                report.ReportScoreScale
	Score                float64
	Confidence           float64
	Issues               []learningProfileContributionIssue
	CreatedAt            time.Time
}

func persistFormalReportAndLearningProfile(
	ctx context.Context,
	tx pgx.Tx,
	claim durableSceneJobClaim,
	formal report.FormalReport,
) error {
	if ctx == nil || tx == nil || !formal.Valid() ||
		formal.SceneType != claim.Snapshot.SceneType {
		return evaluation.ErrInvalidRequest
	}
	encoded, err := json.Marshal(formal)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	var reportID string
	err = tx.QueryRow(ctx, `
		INSERT INTO evaluation_formal_reports (
			evaluation_id,
			evaluation_revision_id,
			owner_user_id,
			practice_session_id,
			revision,
			scene_type,
			scene_model,
			scoreability_status,
			schema_version,
			report_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
		ON CONFLICT (evaluation_revision_id) DO NOTHING
		RETURNING report_id::text
	`, claim.EvaluationID, claim.EvaluationRevisionID,
		claim.OwnerUserID, claim.Snapshot.PracticeSessionID,
		claim.Revision, formal.SceneType, formal.SceneModel,
		formal.ScoreabilityStatus, formal.SchemaVersion, encoded).Scan(
		&reportID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var persisted []byte
		if err := tx.QueryRow(ctx, `
			SELECT report_payload
			FROM evaluation_formal_reports
			WHERE evaluation_revision_id = $1
			  AND evaluation_id = $2
			  AND owner_user_id = $3
		`, claim.EvaluationRevisionID, claim.EvaluationID,
			claim.OwnerUserID).Scan(&persisted); err != nil {
			return fmt.Errorf("read replayed Evaluation report: %w", err)
		}
		if !sameJSON(persisted, encoded) {
			return scoring.ErrRuntimeConfigurationConflict
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("insert Evaluation report: %w", err)
	}
	if !validUUID(reportID) {
		return evaluation.ErrInvalidRequest
	}
	if formal.ScoreabilityStatus != report.ReportScoreabilityProvisional {
		return nil
	}
	projected := make([]string, 0, len(formal.Dimensions))
	for _, dimension := range formal.Dimensions {
		if dimension.Score == nil || len(dimension.EvidenceRefs) == 0 {
			continue
		}
		dimensionKey, err := learningProfileDimensionKey(
			formal.SceneType,
			dimension.Key,
		)
		if err != nil {
			return err
		}
		issues := contributionIssues(dimensionKey, dimension)
		issueJSON, err := json.Marshal(issues)
		if err != nil {
			return evaluation.ErrInvalidRequest
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO learning_profile_contributions (
				owner_user_id,
				evaluation_id,
				evaluation_revision_id,
				dimension_key,
				score_scale,
				score,
				confidence,
				recurring_issues,
				evidence_ref_ids,
				strategy_version
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
			ON CONFLICT (
				owner_user_id,
				evaluation_revision_id,
				dimension_key
			) DO NOTHING
		`, claim.OwnerUserID, claim.EvaluationID,
			claim.EvaluationRevisionID, dimensionKey, dimension.Scale,
			*dimension.Score, dimension.Confidence, issueJSON,
			dimension.EvidenceRefs, learningprofile.StrategyVersion)
		if err != nil {
			return fmt.Errorf("insert Learning Profile contribution: %w", err)
		}
		if tag.RowsAffected() == 1 {
			projected = append(projected, dimensionKey)
		}
	}
	for _, dimensionKey := range projected {
		if err := rebuildLearningProfileDimension(
			ctx,
			tx,
			claim.OwnerUserID,
			dimensionKey,
		); err != nil {
			return err
		}
	}
	return nil
}

func rebuildLearningProfileForRevision(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	revisionID string,
) error {
	rows, err := tx.Query(ctx, `
		SELECT dimension_key
		FROM learning_profile_contributions
		WHERE owner_user_id = $1
		  AND evaluation_revision_id = $2
		ORDER BY dimension_key
	`, ownerUserID, revisionID)
	if err != nil {
		return fmt.Errorf("list superseded Learning Profile dimensions: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return fmt.Errorf("scan superseded Learning Profile dimension: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate superseded Learning Profile dimensions: %w", err)
	}
	rows.Close()
	for _, key := range keys {
		if err := rebuildLearningProfileDimension(
			ctx,
			tx,
			ownerUserID,
			key,
		); err != nil {
			return err
		}
	}
	return nil
}

func rebuildLearningProfileDimension(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
	dimensionKey string,
) error {
	rows, err := tx.Query(ctx, `
		SELECT
			contribution.evaluation_id::text,
			contribution.evaluation_revision_id::text,
			contribution.score_scale,
			contribution.score::float8,
			contribution.confidence::float8,
			contribution.recurring_issues,
			contribution.created_at
		FROM learning_profile_contributions AS contribution
		JOIN evaluation_revision_states AS state
		  ON state.revision_id = contribution.evaluation_revision_id
		 AND state.evaluation_id = contribution.evaluation_id
		 AND state.owner_user_id = contribution.owner_user_id
		JOIN evaluation_revisions AS revision
		  ON revision.id = state.revision_id
		 AND revision.evaluation_id = state.evaluation_id
		 AND revision.owner_user_id = state.owner_user_id
		WHERE contribution.owner_user_id = $1
		  AND contribution.dimension_key = $2
		  AND contribution.strategy_version = $3
		  AND state.evaluation_status = 'READY'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_revisions AS later
		      WHERE later.evaluation_id = state.evaluation_id
		        AND later.owner_user_id = state.owner_user_id
		        AND later.revision > revision.revision
		  )
		ORDER BY
			contribution.created_at DESC,
			contribution.evaluation_revision_id DESC
		LIMIT 20
	`, ownerUserID, dimensionKey, learningprofile.StrategyVersion)
	if err != nil {
		return fmt.Errorf("read Learning Profile contributions: %w", err)
	}
	defer rows.Close()
	contributions := make([]learningProfileContribution, 0, 20)
	for rows.Next() {
		var (
			contribution learningProfileContribution
			issuesJSON   []byte
		)
		if err := rows.Scan(
			&contribution.EvaluationID,
			&contribution.EvaluationRevisionID,
			&contribution.Scale,
			&contribution.Score,
			&contribution.Confidence,
			&issuesJSON,
			&contribution.CreatedAt,
		); err != nil {
			return fmt.Errorf("scan Learning Profile contribution: %w", err)
		}
		if err := json.Unmarshal(issuesJSON, &contribution.Issues); err != nil {
			return evaluation.ErrInvalidRequest
		}
		contribution.CreatedAt = contribution.CreatedAt.UTC()
		contributions = append(contributions, contribution)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate Learning Profile contributions: %w", err)
	}
	rows.Close()
	if len(contributions) == 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM learning_profile_dimensions
			WHERE owner_user_id = $1
			  AND dimension_key = $2
		`, ownerUserID, dimensionKey); err != nil {
			return fmt.Errorf("delete empty Learning Profile dimension: %w", err)
		}
		return nil
	}
	dimension, err := aggregateLearningProfileDimension(
		dimensionKey,
		contributions,
	)
	if err != nil {
		return err
	}
	issuesJSON, err := json.Marshal(dimension.RecurringIssues)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	sourcesJSON, err := json.Marshal(dimension.SourceEvaluations)
	if err != nil {
		return evaluation.ErrInvalidRequest
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO learning_profile_dimensions (
			owner_user_id,
			dimension_key,
			score_scale,
			estimated_value,
			confidence,
			trend,
			recurring_issues,
			source_evaluation_refs,
			strategy_version,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9,
		        transaction_timestamp())
		ON CONFLICT (owner_user_id, dimension_key) DO UPDATE
		SET score_scale = EXCLUDED.score_scale,
		    estimated_value = EXCLUDED.estimated_value,
		    confidence = EXCLUDED.confidence,
		    trend = EXCLUDED.trend,
		    recurring_issues = EXCLUDED.recurring_issues,
		    source_evaluation_refs = EXCLUDED.source_evaluation_refs,
		    strategy_version = EXCLUDED.strategy_version,
		    updated_at = EXCLUDED.updated_at
	`, ownerUserID, dimension.Key, dimension.Scale,
		dimension.EstimatedValue, dimension.Confidence,
		dimension.Trend, issuesJSON, sourcesJSON,
		learningprofile.StrategyVersion)
	if err != nil {
		return fmt.Errorf("upsert Learning Profile dimension: %w", err)
	}
	return nil
}

func aggregateLearningProfileDimension(
	key string,
	contributions []learningProfileContribution,
) (learningprofile.Dimension, error) {
	if !validVersion(key) || len(contributions) == 0 || len(contributions) > 20 {
		return learningprofile.Dimension{}, evaluation.ErrInvalidRequest
	}
	scale := contributions[0].Scale
	weightedScore := 0.0
	weightTotal := 0.0
	confidenceTotal := 0.0
	issues := make(map[string]learningprofile.Issue)
	sources := make([]learningprofile.SourceRef, 0, len(contributions))
	for _, contribution := range contributions {
		if contribution.Scale != scale || contribution.Confidence < 0 ||
			contribution.Confidence > 1 ||
			math.IsNaN(contribution.Score) ||
			math.IsInf(contribution.Score, 0) ||
			math.IsNaN(contribution.Confidence) ||
			math.IsInf(contribution.Confidence, 0) ||
			contribution.CreatedAt.IsZero() {
			return learningprofile.Dimension{}, evaluation.ErrInvalidRequest
		}
		weight := math.Max(contribution.Confidence, 0.1)
		weightedScore += contribution.Score * weight
		weightTotal += weight
		confidenceTotal += contribution.Confidence
		for _, issue := range contribution.Issues {
			current := issues[issue.Key]
			if current.Key == "" {
				current = learningprofile.Issue{
					Key:      issue.Key,
					Label:    issue.Label,
					LastSeen: contribution.CreatedAt,
				}
			}
			current.Count++
			if contribution.CreatedAt.After(current.LastSeen) {
				current.LastSeen = contribution.CreatedAt
				current.Label = issue.Label
			}
			issues[issue.Key] = current
		}
		sources = append(sources, learningprofile.SourceRef{
			EvaluationID:         contribution.EvaluationID,
			EvaluationRevisionID: contribution.EvaluationRevisionID,
			CreatedAt:            contribution.CreatedAt,
		})
	}
	if weightTotal == 0 {
		return learningprofile.Dimension{}, evaluation.ErrInvalidRequest
	}
	recurring := make([]learningprofile.Issue, 0, len(issues))
	for _, issue := range issues {
		recurring = append(recurring, issue)
	}
	slices.SortFunc(recurring, func(left, right learningprofile.Issue) int {
		if left.Count != right.Count {
			return right.Count - left.Count
		}
		if compared := right.LastSeen.Compare(left.LastSeen); compared != 0 {
			return compared
		}
		return strings.Compare(left.Key, right.Key)
	})
	if len(recurring) > 10 {
		recurring = recurring[:10]
	}
	return learningprofile.Dimension{
		Key:               key,
		Scale:             scale,
		EstimatedValue:    roundProfileValue(weightedScore / weightTotal),
		Confidence:        roundProfileConfidence(confidenceTotal / float64(len(contributions))),
		Trend:             learningProfileTrend(scale, contributions),
		RecurringIssues:   recurring,
		SourceEvaluations: sources,
		StrategyVersion:   learningprofile.StrategyVersion,
		UpdatedAt:         contributions[0].CreatedAt,
	}, nil
}

func contributionIssues(
	dimensionKey string,
	dimension report.ReportDimension,
) []learningProfileContributionIssue {
	limit := min(len(dimension.Improvements), 5)
	issues := make([]learningProfileContributionIssue, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, finding := range dimension.Improvements {
		label := strings.TrimSpace(finding.Message)
		if label == "" {
			continue
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(label), " "))
		digest := sha256.Sum256([]byte(dimensionKey + "\x00" + normalized))
		key := "issue:" + hex.EncodeToString(digest[:12])
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		issues = append(issues, learningProfileContributionIssue{
			Key:   key,
			Label: label,
		})
		if len(issues) == 5 {
			break
		}
	}
	return issues
}

func learningProfileDimensionKey(
	sceneType evaluation.SceneType,
	dimensionKey string,
) (string, error) {
	if !validVersion(dimensionKey) {
		return "", evaluation.ErrInvalidRequest
	}
	var namespace string
	switch sceneType {
	case evaluation.SceneInterview:
		namespace = "interview"
	case evaluation.SceneIELTSSpeaking:
		namespace = "ielts_speaking"
	case evaluation.SceneOverseasDaily:
		namespace = "overseas_daily_life"
	case evaluation.SceneOverseasWorkplace:
		namespace = "overseas_workplace"
	default:
		return "", evaluation.ErrInvalidRequest
	}
	key := namespace + "." + dimensionKey
	if !validVersion(key) {
		return "", evaluation.ErrInvalidRequest
	}
	return key, nil
}

func validLearningProfileTrend(trend learningprofile.Trend) bool {
	switch trend {
	case learningprofile.TrendInitial,
		learningprofile.TrendStable,
		learningprofile.TrendImproving,
		learningprofile.TrendDeclining:
		return true
	default:
		return false
	}
}

func learningProfileTrend(
	scale report.ReportScoreScale,
	contributions []learningProfileContribution,
) learningprofile.Trend {
	if len(contributions) < 2 {
		return learningprofile.TrendInitial
	}
	delta := contributions[0].Score - contributions[1].Score
	threshold := 2.0
	if scale == report.ReportScaleIELTSBand {
		threshold = 0.5
	}
	switch {
	case delta >= threshold:
		return learningprofile.TrendImproving
	case delta <= -threshold:
		return learningprofile.TrendDeclining
	default:
		return learningprofile.TrendStable
	}
}

func roundProfileValue(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func roundProfileConfidence(value float64) float64 {
	return math.Round(value*100000) / 100000
}

func sameJSON(left []byte, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil ||
		json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftCanonical, leftErr := json.Marshal(leftValue)
	rightCanonical, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil &&
		bytes.Equal(leftCanonical, rightCanonical)
}
