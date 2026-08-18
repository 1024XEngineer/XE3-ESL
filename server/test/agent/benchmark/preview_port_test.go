package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestBenchmarkPreviewPortRecordsOnlyStructuredResolution(t *testing.T) {
	tests := []struct {
		name           string
		input          json.RawMessage
		wantKind       string
		wantCatalogID  string
		wantCandidates []string
		privateTerm    string
	}{
		{
			name: "catalog",
			input: json.RawMessage(`{
  "resolution_kind": "CATALOG",
  "catalog_scene_ids": ["scn_travel_hotel_checkin"],
  "custom_scenario": "",
  "custom_experience_hint": "NONE",
  "background_summary": "海景房预订不见了"
}`),
			wantKind:      "CATALOG",
			wantCatalogID: "scn_travel_hotel_checkin",
			privateTerm:   "海景房",
		},
		{
			name: "needs clarification",
			input: json.RawMessage(`{
  "resolution_kind": "NEEDS_CLARIFICATION",
  "catalog_scene_ids": [
    "scn_travel_hotel_checkin",
    "scn_travel_airport_checkin"
  ],
  "custom_scenario": "",
  "custom_experience_hint": "NONE"
}`),
			wantKind: "NEEDS_CLARIFICATION",
			wantCandidates: []string{
				"scn_travel_hotel_checkin",
				"scn_travel_airport_checkin",
			},
			privateTerm: "酒店入住还是机场值机",
		},
		{
			name: "custom",
			input: json.RawMessage(`{
  "resolution_kind": "CUSTOM",
  "catalog_scene_ids": [],
  "custom_scenario": "在宠物店沟通鹦鹉寄养",
  "custom_experience_hint": "LIFE_AND_TRAVEL",
  "background_summary": "周末寄养两天"
}`),
			wantKind:    "CUSTOM",
			privateTerm: "鹦鹉寄养",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			port, err := newBenchmarkPreviewPort(logger)
			if err != nil {
				t.Fatalf("newBenchmarkPreviewPort() error = %v", err)
			}
			tool, err := preparationcapability.NewPreviewTool(port)
			if err != nil {
				t.Fatalf("NewPreviewTool() error = %v", err)
			}
			_, err = tool.Execute(
				context.Background(),
				benchmarkPreviewCallContext(),
				test.input,
			)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			line := output.String()
			if strings.Contains(line, test.privateTerm) {
				t.Fatalf("benchmark log leaked raw scene text: %s", line)
			}
			var event struct {
				Message           string   `json:"msg"`
				Kind              string   `json:"kind"`
				CatalogSceneID    string   `json:"catalog_scene_id"`
				CandidateSceneIDs []string `json:"candidate_scene_ids"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
				t.Fatalf("decode benchmark event: %v", err)
			}
			if event.Message != "agent.benchmark.preview.input" ||
				event.Kind != test.wantKind ||
				event.CatalogSceneID != test.wantCatalogID ||
				!sameCandidateIDs(event.CandidateSceneIDs, test.wantCandidates) {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestBenchmarkPreviewPortRejectsInvalidUnionBeforeRecording(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	port, err := newBenchmarkPreviewPort(logger)
	if err != nil {
		t.Fatalf("newBenchmarkPreviewPort() error = %v", err)
	}
	tool, err := preparationcapability.NewPreviewTool(port)
	if err != nil {
		t.Fatalf("NewPreviewTool() error = %v", err)
	}
	_, err = tool.Execute(
		context.Background(),
		benchmarkPreviewCallContext(),
		json.RawMessage(`{
  "resolution_kind": "CATALOG",
  "catalog_scene_ids": [
    "scn_travel_hotel_checkin",
    "scn_travel_airport_checkin"
  ],
  "custom_scenario": "",
  "custom_experience_hint": "NONE"
}`),
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("Execute() error = %v, want ErrInvalidInput", err)
	}
	if output.Len() != 0 {
		t.Fatalf("invalid input reached benchmark port: %s", output.String())
	}
}

func benchmarkPreviewCallContext() capability.CallContext {
	return capability.CallContext{
		Actor: requestcontext.Actor{
			UserID:    "benchmark-user",
			SessionID: "benchmark-session",
		},
		ThreadID:      "benchmark-thread",
		RunID:         "benchmark-run",
		ToolCallID:    "benchmark-tool-call",
		RequestID:     "benchmark-request",
		Authorization: json.RawMessage(`{"intent":"REQUEST_CREATE"}`),
	}
}

func sameCandidateIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
