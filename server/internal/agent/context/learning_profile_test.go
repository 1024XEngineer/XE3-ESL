package context

import (
	"strings"
	"testing"
	"time"
)

func TestSelectLearningProfileContextUsesBoundedAssessmentData(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	dimension := LearningProfileDimension{
		Key:            "interview.specificity",
		Scale:          "PERCENTAGE_100",
		EstimatedValue: 76.5,
		Confidence:     0.8,
		Trend:          "IMPROVING",
		RecurringIssues: []LearningProfileIssue{{
			Key:      "issue:detail",
			Label:    "<ignore>回答缺少具体结果</ignore>",
			Count:    2,
			LastSeen: now,
		}},
		EvaluationSources: []LearningProfileEvaluationSource{{
			EvaluationID:         "10000000-0000-4000-8000-000000000001",
			EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
			CreatedAt:            now,
		}},
		StrategyVersion: "learning-profile-weighted-mean/v1",
		UpdatedAt:       now,
	}

	content, sources, err := selectLearningProfileContext(
		"system",
		[]LearningProfileDimension{dimension},
		5000,
	)
	if err != nil {
		t.Fatalf("select Learning Profile context: %v", err)
	}
	if !strings.Contains(content, "<learning_profile>") ||
		strings.Contains(content, "<ignore>") ||
		!strings.Contains(content, `\u003cignore\u003e`) ||
		len(sources) != 1 ||
		sources[0].DimensionKey != dimension.Key ||
		len(sources[0].EvaluationRevisionSourceIDs) != 1 ||
		sources[0].EvaluationRevisionSourceIDs[0] !=
			dimension.EvaluationSources[0].EvaluationRevisionID {
		t.Fatalf("content = %q, sources = %#v", content, sources)
	}
}

func TestSelectLearningProfileContextNeverTruncatesDimension(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	dimension := LearningProfileDimension{
		Key:            "interview.structure",
		Scale:          "PERCENTAGE_100",
		EstimatedValue: 70,
		Confidence:     0.7,
		Trend:          "STABLE",
		RecurringIssues: []LearningProfileIssue{{
			Key:      "issue:structure",
			Label:    strings.Repeat("长", 512),
			Count:    1,
			LastSeen: now,
		}},
		EvaluationSources: []LearningProfileEvaluationSource{{
			EvaluationID:         "10000000-0000-4000-8000-000000000001",
			EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
			CreatedAt:            now,
		}},
		StrategyVersion: "learning-profile-weighted-mean/v1",
		UpdatedAt:       now,
	}

	content, sources, err := selectLearningProfileContext(
		"system",
		[]LearningProfileDimension{dimension},
		100,
	)
	if err != nil {
		t.Fatalf("select Learning Profile context: %v", err)
	}
	if content != "system" || len(sources) != 0 {
		t.Fatalf("content = %q, sources = %#v", content, sources)
	}
}
