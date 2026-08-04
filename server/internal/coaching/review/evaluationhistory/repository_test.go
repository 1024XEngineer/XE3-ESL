package evaluationhistory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
)

func TestRepositoryProjectsEvaluationReportForReview(t *testing.T) {
	stored := storedFormalReport()
	if !stored.Valid() {
		t.Fatalf("stored report fixture is invalid: %#v", stored)
	}
	source := &formalReportSourceStub{report: stored}
	repository, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	actor := review.Actor{UserID: stored.OwnerUserID}

	got, err := repository.GetReport(
		context.Background(),
		actor,
		stored.ReportID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Valid() || got.ID != stored.ReportID ||
		got.SceneType != string(stored.Report.SceneType) ||
		len(got.Dimensions) != 1 ||
		len(got.Dimensions[0].Improvements) != 1 ||
		got.Dimensions[0].Improvements[0].Suggestion != "使用 STAR 结构。" {
		t.Fatalf("GetReport() = %#v", got)
	}
	if source.ownerUserID != stored.OwnerUserID ||
		source.reportID != stored.ReportID {
		t.Fatalf("source request = %q %q", source.ownerUserID, source.reportID)
	}
}

func TestRepositoryMapsReviewHistoryQueryToEvaluationSource(t *testing.T) {
	stored := storedFormalReport()
	source := &formalReportSourceStub{
		page: evaluation.FormalReportHistoryPage{
			Items:   []evaluation.StoredFormalReport{stored},
			HasMore: true,
		},
	}
	repository, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	before := review.HistoryCursor{
		CreatedAt: stored.CreatedAt.Add(time.Hour),
		ReportID:  "90000000-0000-4000-8000-000000000009",
	}

	page, err := repository.ListReports(
		context.Background(),
		review.Actor{UserID: stored.OwnerUserID},
		review.HistoryQuery{Limit: 10, Before: &before},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Next == nil ||
		page.Next.ReportID != stored.ReportID ||
		source.query.Limit != 10 || source.query.Before == nil ||
		source.query.Before.ReportID != before.ReportID {
		t.Fatalf("ListReports() page=%#v source=%#v", page, source.query)
	}
}

type formalReportSourceStub struct {
	report      evaluation.StoredFormalReport
	page        evaluation.FormalReportHistoryPage
	ownerUserID string
	reportID    string
	query       evaluation.FormalReportHistoryQuery
}

func (source *formalReportSourceStub) GetFormalReport(
	_ context.Context,
	ownerUserID string,
	reportID string,
) (evaluation.StoredFormalReport, error) {
	source.ownerUserID = ownerUserID
	source.reportID = reportID
	return source.report, nil
}

func (source *formalReportSourceStub) ListFormalReports(
	_ context.Context,
	ownerUserID string,
	query evaluation.FormalReportHistoryQuery,
) (evaluation.FormalReportHistoryPage, error) {
	source.ownerUserID = ownerUserID
	source.query = query
	return source.page, nil
}

func storedFormalReport() evaluation.StoredFormalReport {
	score := 82.0
	createdAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	return evaluation.StoredFormalReport{
		ReportID:             "20000000-0000-4000-8000-000000000002",
		EvaluationID:         "30000000-0000-4000-8000-000000000003",
		EvaluationRevisionID: "40000000-0000-4000-8000-000000000004",
		OwnerUserID:          "50000000-0000-4000-8000-000000000005",
		PracticeSessionID:    "session_demo_002",
		Revision:             1,
		CreatedAt:            createdAt,
		Report: evaluation.FormalReport{
			SchemaVersion:      evaluation.FormalReportSchemaVersion,
			SceneType:          evaluation.SceneInterview,
			SceneModel:         "PROJECT_EXPERIENCE_DEEP_DIVE",
			ScoreabilityStatus: evaluation.ReportScoreabilityProvisional,
			Summary:            "本次练习已形成面试表达评估。",
			Dimensions: []evaluation.ReportDimension{{
				Key:          "interview.structure",
				Score:        &score,
				Scale:        evaluation.ReportScalePercentage100,
				Coverage:     1,
				Confidence:   0.8,
				ReasonCodes:  []string{},
				EvidenceRefs: []string{"evidence_demo_001"},
				Strengths:    []evaluation.ReportFinding{},
				Improvements: []evaluation.ReportFinding{{
					ID:         "structure-action",
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
				DimensionKey: "interview.structure",
				FindingID:    "structure-action",
			}},
			DetailSchema: "interview-report/v1",
			Detail: json.RawMessage(
				`{"schema_version":"interview-report/v1"}`,
			),
		},
	}
}
