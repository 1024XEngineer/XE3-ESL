package agentcapability

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
)

func TestMapLatestFormalReportProjectsCanonicalUserFacingContent(t *testing.T) {
	score := 82.0
	completedAt := time.Date(2026, 8, 1, 4, 30, 0, 0, time.UTC)
	stored := report.StoredFormalReport{
		ReportID:             "20000000-0000-4000-8000-000000000002",
		EvaluationID:         "7b000001-0000-4000-8000-000000000001",
		EvaluationRevisionID: "a1000001-0000-4000-8000-000000000001",
		OwnerUserID:          "10000000-0000-4000-8000-000000000001",
		PracticeSessionID:    "session_demo_002",
		Revision:             1,
		CreatedAt:            completedAt,
		Report: report.FormalReport{
			SchemaVersion:      report.FormalReportSchemaVersion,
			SceneType:          evaluation.SceneInterview,
			PracticeExperience: "INTERVIEW",
			SceneCategory:      "INTERVIEW_PROFESSIONAL",
			PracticeMode:       "FULL_SIMULATION",
			ScoreabilityStatus: report.ReportScoreabilityProvisional,
			Summary:            "本次练习已形成面试表达评估。",
			Dimensions: []report.ReportDimension{{
				Key:        "INTERVIEW_STRUCTURE",
				Score:      &score,
				Scale:      report.ReportScalePercentage100,
				Coverage:   1,
				Confidence: 0.8,
				ReasonCodes: []string{
					"ASR_CONFIDENCE_UNAVAILABLE",
				},
				EvidenceRefs: []string{"evidence_demo_001"},
				Strengths:    []report.ReportFinding{},
				Improvements: []report.ReportFinding{{
					ID:         "structure_action",
					Message:    "回答结构可以更清楚。",
					Suggestion: "使用 STAR 结构。",
					Evidence: []report.ReportEvidence{{
						EvidenceRefID:   "evidence_demo_001",
						TurnID:          "turn_demo_001",
						StartUTF8Byte:   0,
						EndUTF8Byte:     26,
						OriginalExcerpt: "I made the product better.",
					}},
				}},
				Examples: []report.ReportFinding{},
			}},
			PriorityActions: []report.ReportPriorityAction{{
				DimensionKey: "INTERVIEW_STRUCTURE",
				FindingID:    "structure_action",
			}},
			DetailSchema: "interview-report/v1",
			Detail:       json.RawMessage(`{"schema_version":"interview-report/v1"}`),
		},
	}

	got := mapLatestFormalReport(stored)
	if got.Scene != "面试英语" ||
		got.PracticeExperience != stored.Report.PracticeExperience ||
		got.SceneCategory != stored.Report.SceneCategory ||
		got.PracticeMode != stored.Report.PracticeMode ||
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
