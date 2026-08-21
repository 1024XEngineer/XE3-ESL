package agentcapability

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestServicePortReadsEvaluationReportAuthority(t *testing.T) {
	t.Parallel()
	stored := validStoredReport()
	reader := &reportReaderStub{reports: []report.StoredFormalReport{stored}}
	port, err := NewServicePort(reader)
	if err != nil {
		t.Fatal(err)
	}
	items, err := port.SearchReviews(
		context.Background(),
		validReviewCallContext(),
		ReviewSearchInput{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.query.Limit != defaultReviewSearchLimit || len(items) != 1 ||
		items[0].ID != stored.ReportID ||
		items[0].SourceRefs[0].Type != "evaluation_report" {
		t.Fatalf("query=%#v items=%#v", reader.query, items)
	}
}

func TestServicePortMapsDetailWithoutInternalEvaluationPayload(t *testing.T) {
	t.Parallel()
	stored := validStoredReport()
	reader := &reportReaderStub{report: stored}
	port, err := NewServicePort(reader)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := port.GetReview(
		context.Background(),
		validReviewCallContext(),
		ReviewGetInput{ReportID: stored.ReportID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != stored.ReportID || len(detail.Dimensions) != 1 ||
		len(detail.Dimensions[0].Improvements) != 1 ||
		detail.Dimensions[0].Improvements[0].OriginalExcerpts[0] != "I need room" {
		t.Fatalf("detail = %#v", detail)
	}
}

func validStoredReport() report.StoredFormalReport {
	score := 72.0
	return report.StoredFormalReport{
		ReportID:          "10000000-0000-4000-8000-000000000001",
		EvaluationID:      "10000000-0000-4000-8000-000000000001",
		OwnerUserID:       "40000000-0000-4000-8000-000000000001",
		PracticeSessionID: "20000000-0000-4000-8000-000000000001",
		Report: report.FormalReport{
			SchemaVersion:      report.FormalReportSchemaVersion,
			SceneType:          "OVERSEAS_DAILY_LIFE",
			PracticeExperience: "LIFE_AND_TRAVEL",
			SceneCategory:      "LIFE_DAILY",
			PracticeMode:       "FULL_SIMULATION",
			ScoreabilityStatus: report.ReportScoreabilityProvisional,
			Summary:            "本次练习已形成场景沟通评估。",
			Questions: []report.ReportQuestion{{
				ID:       "50000000-0000-4000-8000-000000000001",
				Position: 1,
				Text:     "What kind of room do you need?",
				Answer: &report.ReportAnswer{
					TurnID:     "30000000-0000-4000-8000-000000000001",
					Transcript: "I need room",
				},
			}},
			Dimensions: []report.ReportDimension{{
				Key:          "TASK_ACHIEVEMENT",
				Score:        &score,
				Scale:        report.ReportScalePercentage100,
				Coverage:     1,
				Confidence:   0.6,
				ReasonCodes:  []string{},
				EvidenceRefs: []string{"30000000-0000-4000-8000-000000000001"},
				Strengths:    []report.ReportFinding{},
				Improvements: []report.ReportFinding{{
					ID:         "finding-improvement",
					Message:    "Make the intended outcome clearer.",
					Suggestion: "State the request first.",
					Evidence: []report.ReportEvidence{{
						EvidenceRefID:   "30000000-0000-4000-8000-000000000001",
						TurnID:          "30000000-0000-4000-8000-000000000001",
						StartUTF8Byte:   0,
						EndUTF8Byte:     11,
						OriginalExcerpt: "I need room",
					}},
				}},
				Examples: []report.ReportFinding{},
			}},
			PriorityActions: []report.ReportPriorityAction{{
				DimensionKey: "TASK_ACHIEVEMENT",
				FindingID:    "finding-improvement",
			}},
		},
		CreatedAt: time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC),
	}
}

func validReviewCallContext() capability.CallContext {
	return capability.CallContext{Actor: requestcontext.Actor{
		UserID:    "40000000-0000-4000-8000-000000000001",
		SessionID: "session-1",
	}}
}

type reportReaderStub struct {
	report  report.StoredFormalReport
	reports []report.StoredFormalReport
	query   report.HistoryQuery
}

func (reader *reportReaderStub) GetFormalReport(
	context.Context,
	string,
	string,
) (report.StoredFormalReport, error) {
	return reader.report, nil
}

func (reader *reportReaderStub) ListFormalReports(
	_ context.Context,
	_ string,
	query report.HistoryQuery,
) (report.HistoryPage, error) {
	reader.query = query
	return report.HistoryPage{Items: reader.reports}, nil
}
