package agenttool

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestMapLatestFormalReportProjectsCanonicalUserFacingContent(t *testing.T) {
	score := 82.0
	completedAt := time.Date(2026, 8, 1, 4, 30, 0, 0, time.UTC)
	stored := evaluation.StoredFormalReport{
		ReportID:             "20000000-0000-4000-8000-000000000002",
		EvaluationID:         "7b000001-0000-4000-8000-000000000001",
		EvaluationRevisionID: "a1000001-0000-4000-8000-000000000001",
		OwnerUserID:          "10000000-0000-4000-8000-000000000001",
		PracticeSessionID:    "session_demo_002",
		Revision:             1,
		CreatedAt:            completedAt,
		Report: evaluation.FormalReport{
			SchemaVersion:      evaluation.FormalReportSchemaVersion,
			SceneType:          evaluation.SceneInterview,
			SceneModel:         "PROJECT_EXPERIENCE_DEEP_DIVE",
			ScoreabilityStatus: evaluation.ReportScoreabilityProvisional,
			Summary:            "本次练习已形成面试表达评估。",
			Dimensions: []evaluation.ReportDimension{{
				Key:        "INTERVIEW_STRUCTURE",
				Score:      &score,
				Scale:      evaluation.ReportScalePercentage100,
				Coverage:   1,
				Confidence: 0.8,
				ReasonCodes: []string{
					"ASR_CONFIDENCE_UNAVAILABLE",
				},
				EvidenceRefs: []string{"evidence_demo_001"},
				Strengths:    []evaluation.ReportFinding{},
				Improvements: []evaluation.ReportFinding{{
					ID:         "structure_action",
					Message:    "回答结构可以更清楚。",
					Suggestion: "使用 STAR 结构。",
					Evidence: []evaluation.ReportEvidence{{
						EvidenceRefID:   "evidence_demo_001",
						TurnID:          "turn_demo_001",
						StartUTF8Byte:   0,
						EndUTF8Byte:     26,
						OriginalExcerpt: "I made the product better.",
					}},
				}},
				Examples: []evaluation.ReportFinding{},
			}},
			PriorityActions: []evaluation.ReportPriorityAction{{
				DimensionKey: "INTERVIEW_STRUCTURE",
				FindingID:    "structure_action",
			}},
			DetailSchema: "interview-report/v1",
			Detail:       json.RawMessage(`{"schema_version":"interview-report/v1"}`),
		},
	}

	got := mapLatestFormalReport(stored)
	if got.Scene != "面试英语" || got.SceneModel != stored.Report.SceneModel ||
		got.AssessmentMode != "暂定评分与反馈" ||
		got.Summary != stored.Report.Summary ||
		got.CompletedAt != completedAt.Format(time.RFC3339Nano) ||
		len(got.Dimensions) != 1 ||
		got.Dimensions[0].Name != "回答结构" ||
		got.Dimensions[0].Score == nil ||
		*got.Dimensions[0].Score != score ||
		len(got.PriorityActions) != 1 ||
		got.PriorityActions[0].OriginalExcerpts[0] !=
			"I made the product better." {
		t.Fatalf("mapped report = %#v", got)
	}
}
