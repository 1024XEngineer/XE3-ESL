package agentcapability

import (
	"context"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	domainreview "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestServicePortListsCanonicalEvaluationReports(t *testing.T) {
	t.Parallel()
	report := validAgentToolReport()
	repository := &reviewHistoryRepositoryStub{reports: []domainreview.Report{report}}
	port, err := NewServicePort(domainreview.NewHistoryService(repository))
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
	if repository.listQuery.Limit != defaultReviewSearchLimit ||
		len(items) != 1 || items[0].ID != report.ID ||
		items[0].SourceRefs[0].Type != "evaluation_report" {
		t.Fatalf("query=%#v items=%#v", repository.listQuery, items)
	}
	if items[0].Summary != report.Summary ||
		items[0].CompletedAt != report.CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("item = %#v", items[0])
	}
}

func TestServicePortMapsReportDetailWithoutRawEvidencePayload(t *testing.T) {
	t.Parallel()
	report := validAgentToolReport()
	repository := &reviewHistoryRepositoryStub{report: report}
	port, err := NewServicePort(domainreview.NewHistoryService(repository))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := port.GetReview(
		context.Background(),
		validReviewCallContext(),
		ReviewGetInput{ReportID: report.ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != report.ID || len(detail.Dimensions) != 1 ||
		len(detail.Dimensions[0].Improvements) != 1 ||
		detail.Dimensions[0].Improvements[0].OriginalExcerpts[0] !=
			"I need room" || detail.SourceRefs[0].Type != "evaluation_report" {
		t.Fatalf("detail = %#v", detail)
	}
}

func validAgentToolReport() domainreview.Report {
	score := 72.0
	return domainreview.Report{
		ID:                   "10000000-0000-4000-8000-000000000001",
		EvaluationID:         "20000000-0000-4000-8000-000000000001",
		EvaluationRevisionID: "30000000-0000-4000-8000-000000000001",
		OwnerUserID:          "40000000-0000-4000-8000-000000000001",
		PracticeSessionID:    "practice-session-1",
		Revision:             1,
		SchemaVersion:        "evaluation-report/v1",
		SceneType:            "OVERSEAS_DAILY_LIFE",
		PracticeExperience:   "LIFE_AND_TRAVEL",
		SceneCategory:        "LIFE_DAILY",
		PracticeMode:         "FULL_SIMULATION",
		ScoreabilityStatus:   "PROVISIONAL",
		Summary:              "本次练习已形成场景沟通评估。",
		Dimensions: []domainreview.ReportDimension{{
			Key:          "TASK_ACHIEVEMENT",
			Score:        &score,
			Scale:        "PERCENTAGE_100",
			Coverage:     1,
			Confidence:   0.6,
			ReasonCodes:  []string{"ASR_CONFIDENCE_UNAVAILABLE"},
			EvidenceRefs: []string{"evidence-1"},
			Strengths:    []domainreview.ReportFinding{},
			Improvements: []domainreview.ReportFinding{{
				ID:         "finding-improvement",
				Message:    "Make the intended outcome clearer.",
				Suggestion: "State the request first.",
				Evidence: []domainreview.ReportEvidence{{
					EvidenceRefID:   "evidence-1",
					TurnID:          "turn-1",
					StartUTF8Byte:   0,
					EndUTF8Byte:     11,
					OriginalExcerpt: "I need room",
				}},
			}},
			Examples: []domainreview.ReportFinding{},
		}},
		PriorityActions: []domainreview.ReportPriorityAction{{
			DimensionKey: "TASK_ACHIEVEMENT",
			FindingID:    "finding-improvement",
		}},
		DetailSchema: "general-scene-evaluation/v1",
		Detail:       []byte(`{"schema_version":"general-scene-evaluation/v1"}`),
		CreatedAt: time.Date(
			2026,
			time.August,
			4,
			8,
			0,
			0,
			0,
			time.UTC,
		),
	}
}

func validReviewCallContext() capability.CallContext {
	return capability.CallContext{Actor: requestcontext.Actor{
		UserID:    "40000000-0000-4000-8000-000000000001",
		SessionID: "session-1",
	}}
}

type reviewHistoryRepositoryStub struct {
	report    domainreview.Report
	reports   []domainreview.Report
	listQuery domainreview.HistoryQuery
}

func (repository *reviewHistoryRepositoryStub) GetReport(
	context.Context,
	domainreview.Actor,
	string,
) (domainreview.Report, error) {
	return repository.report, nil
}

func (repository *reviewHistoryRepositoryStub) ListReports(
	_ context.Context,
	_ domainreview.Actor,
	query domainreview.HistoryQuery,
) (domainreview.HistoryPage, error) {
	repository.listQuery = query
	return domainreview.HistoryPage{Items: repository.reports}, nil
}

func (repository *reviewHistoryRepositoryStub) SearchReports(
	context.Context,
	domainreview.Actor,
	domainreview.HistorySearchQuery,
) ([]domainreview.Report, error) {
	return repository.reports, nil
}
