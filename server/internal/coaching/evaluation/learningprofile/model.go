package learningprofile

import (
	"context"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
)

const StrategyVersion = "learning-profile-weighted-mean/v1"

type Trend string

const (
	TrendInitial   Trend = "INITIAL"
	TrendStable    Trend = "STABLE"
	TrendImproving Trend = "IMPROVING"
	TrendDeclining Trend = "DECLINING"
)

type Issue struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

type SourceRef struct {
	EvaluationID         string    `json:"evaluation_id"`
	EvaluationRevisionID string    `json:"evaluation_revision_id"`
	CreatedAt            time.Time `json:"created_at"`
}

type Dimension struct {
	Key               string                  `json:"dimension_key"`
	Scale             report.ReportScoreScale `json:"score_scale"`
	EstimatedValue    float64                 `json:"estimated_value"`
	Confidence        float64                 `json:"confidence"`
	Trend             Trend                   `json:"trend"`
	RecurringIssues   []Issue                 `json:"recurring_issues"`
	SourceEvaluations []SourceRef             `json:"source_evaluations"`
	StrategyVersion   string                  `json:"strategy_version"`
	UpdatedAt         time.Time               `json:"updated_at"`
}

func (dimension Dimension) Valid() bool {
	if !validVersion(dimension.Key) ||
		(dimension.Scale != report.ReportScalePercentage100 &&
			dimension.Scale != report.ReportScaleIELTSBand) ||
		math.IsNaN(dimension.EstimatedValue) ||
		math.IsInf(dimension.EstimatedValue, 0) ||
		math.IsNaN(dimension.Confidence) ||
		math.IsInf(dimension.Confidence, 0) ||
		dimension.Confidence < 0 || dimension.Confidence > 1 ||
		!validTrend(dimension.Trend) ||
		dimension.RecurringIssues == nil ||
		len(dimension.RecurringIssues) > 10 ||
		len(dimension.SourceEvaluations) == 0 ||
		len(dimension.SourceEvaluations) > 20 ||
		!validVersion(dimension.StrategyVersion) ||
		dimension.UpdatedAt.IsZero() {
		return false
	}
	switch dimension.Scale {
	case report.ReportScalePercentage100:
		if dimension.EstimatedValue < 0 || dimension.EstimatedValue > 100 {
			return false
		}
	case report.ReportScaleIELTSBand:
		if dimension.EstimatedValue < 0 || dimension.EstimatedValue > 9 {
			return false
		}
	}
	for _, issue := range dimension.RecurringIssues {
		if !validVersion(issue.Key) || strings.TrimSpace(issue.Label) == "" ||
			issue.Count < 1 || issue.LastSeen.IsZero() {
			return false
		}
	}
	for _, source := range dimension.SourceEvaluations {
		if !validUUID(source.EvaluationID) ||
			!validUUID(source.EvaluationRevisionID) ||
			source.CreatedAt.IsZero() {
			return false
		}
	}
	return true
}

type Query struct {
	GoalID    string
	SceneType evaluation.SceneType
	Limit     int
}

type Reader interface {
	ReadLearningProfile(
		context.Context,
		string,
		Query,
	) ([]Dimension, error)
}

func validTrend(trend Trend) bool {
	switch trend {
	case TrendInitial, TrendStable, TrendImproving, TrendDeclining:
		return true
	default:
		return false
	}
}

var (
	uuidPattern = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
	)
	versionPattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9._:/-]{0,127}$`,
	)
)

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func validVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}
