package agenttool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type fakeLatestPracticeReportPort struct{}

func (fakeLatestPracticeReportPort) LatestPracticeReport(
	context.Context,
	tool.CallContext,
) (LatestPracticeReport, error) {
	score := 76
	return LatestPracticeReport{
		Scene:          "面试英语",
		AssessmentMode: "评分与反馈",
		Dimensions: []ReportDimension{{
			Name:  "回答相关性",
			Score: &score,
			Improvements: []ReportFinding{{
				Message:    "回答需要更聚焦问题。",
				Suggestion: "先给结论，再补充例子。",
			}},
		}},
		Answers: []ReportAnswer{{
			Question:   "Why are you interested in this role?",
			Transcript: "I enjoy building AI products.",
		}},
	}, nil
}

func TestLatestPracticeReportToolNeedsNoUserIdentifier(t *testing.T) {
	definition := NewLatestPracticeReportTool(
		fakeLatestPracticeReportPort{},
	).Definition()
	properties, ok := definition.InputSchema["properties"].(map[string]any)
	if !ok || len(properties) != 0 {
		t.Fatalf("input properties = %#v, want empty object", properties)
	}
	result, err := NewLatestPracticeReportTool(
		fakeLatestPracticeReportPort{},
	).Execute(
		context.Background(),
		tool.CallContext{
			Actor: requestcontext.Actor{
				UserID:    "user-1",
				SessionID: "session-1",
			},
		},
		json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	raw, err := json.Marshal(result.Content)
	if err != nil {
		t.Fatal(err)
	}
	for _, internalField := range []string{
		"profile_id",
		"plan_id",
		"session_id",
		"evaluation_id",
		"question_id",
		"finding_id",
		"review_id",
	} {
		if strings.Contains(string(raw), internalField) {
			t.Fatalf("tool result exposes %q: %s", internalField, raw)
		}
	}
}
