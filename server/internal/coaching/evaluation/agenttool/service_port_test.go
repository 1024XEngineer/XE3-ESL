package agenttool

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestMapLatestInterviewReportProjectsOnlyUserFacingContent(t *testing.T) {
	score := 82
	completedAt := time.Date(2026, 8, 1, 4, 30, 0, 0, time.UTC)
	report := evaluation.InterviewReport{
		ScoreabilityStatus: evaluation.InterviewScoreabilityProvisional,
		Dimensions: []evaluation.InterviewReportDimension{{
			DimensionID: evaluation.InterviewDimensionStructure,
			Score:       &score,
			Improvements: []evaluation.InterviewReportFinding{{
				FindingID:  "internal-finding-id",
				Message:    "回答结构可以更清楚。",
				Suggestion: "使用 STAR 结构。",
				Evidence: []evaluation.InterviewReportEvidence{{
					OriginalExcerpt: "I made the product better.",
				}},
			}},
		}},
		Questions: []evaluation.InterviewReportQuestion{{
			QuestionID:          "internal-question-id",
			AssessmentStatus:    evaluation.InterviewAssessmentAssessed,
			QuestionText:        "Tell me about a project.",
			ConfirmedTranscript: "I made the product better.",
		}},
		PriorityActions: []evaluation.InterviewReportPriorityRef{{
			DimensionID: evaluation.InterviewDimensionStructure,
			FindingID:   "internal-finding-id",
		}},
	}

	got := mapLatestInterviewReport(report, &completedAt)
	if got.AssessmentMode != "评分与反馈" ||
		got.CompletedAt != completedAt.Format(time.RFC3339Nano) ||
		len(got.Dimensions) != 1 ||
		got.Dimensions[0].Name != "回答结构" ||
		got.Dimensions[0].Score == nil ||
		*got.Dimensions[0].Score != score ||
		len(got.Answers) != 1 ||
		len(got.PriorityActions) != 1 {
		t.Fatalf("mapped report = %#v", got)
	}
}
