package evaluationprofile

import (
	"context"
	"reflect"
	"testing"
	"time"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestReaderProjectsEvaluationLearningProfileForAgentContext(
	t *testing.T,
) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	source := &learningProfileSourceStub{
		dimensions: []evaluation.LearningProfileDimension{{
			Key:            "interview.structure",
			Scale:          evaluation.ReportScalePercentage100,
			EstimatedValue: 82,
			Confidence:     0.8,
			Trend:          evaluation.LearningProfileTrendImproving,
			RecurringIssues: []evaluation.LearningProfileIssue{{
				Key:      "issue:structure",
				Label:    "回答结构需要更清楚",
				Count:    2,
				LastSeen: now,
			}},
			SourceEvaluations: []evaluation.LearningProfileSourceRef{{
				EvaluationID:         "10000000-0000-4000-8000-000000000001",
				EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
				CreatedAt:            now,
			}},
			StrategyVersion: evaluation.LearningProfileStrategyVersion,
			UpdatedAt:       now,
		}},
	}
	reader, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	request := agentcontext.LearningProfileReadRequest{
		Actor: requestcontext.Actor{
			UserID:    "30000000-0000-4000-8000-000000000003",
			SessionID: "50000000-0000-4000-8000-000000000005",
		},
		GoalID: "40000000-0000-4000-8000-000000000004",
		Limit:  8,
	}

	got, err := reader.ReadLearningProfile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := []agentcontext.LearningProfileDimension{{
		Key:            "interview.structure",
		Scale:          "PERCENTAGE_100",
		EstimatedValue: 82,
		Confidence:     0.8,
		Trend:          "IMPROVING",
		RecurringIssues: []agentcontext.LearningProfileIssue{{
			Key:      "issue:structure",
			Label:    "回答结构需要更清楚",
			Count:    2,
			LastSeen: now,
		}},
		EvaluationSources: []agentcontext.LearningProfileEvaluationSource{{
			EvaluationID:         "10000000-0000-4000-8000-000000000001",
			EvaluationRevisionID: "20000000-0000-4000-8000-000000000002",
			CreatedAt:            now,
		}},
		StrategyVersion: evaluation.LearningProfileStrategyVersion,
		UpdatedAt:       now,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadLearningProfile() = %#v, want %#v", got, want)
	}
	if source.ownerUserID != request.Actor.UserID ||
		source.query.GoalID != request.GoalID ||
		source.query.Limit != request.Limit {
		t.Fatalf("source request = %q %#v", source.ownerUserID, source.query)
	}
}

type learningProfileSourceStub struct {
	dimensions  []evaluation.LearningProfileDimension
	ownerUserID string
	query       evaluation.LearningProfileQuery
}

func (source *learningProfileSourceStub) ReadLearningProfile(
	_ context.Context,
	ownerUserID string,
	query evaluation.LearningProfileQuery,
) ([]evaluation.LearningProfileDimension, error) {
	source.ownerUserID = ownerUserID
	source.query = query
	return source.dimensions, nil
}
