package xfyun_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

func TestISELive(t *testing.T) {
	if os.Getenv("XFYUN_ISE_LIVE_TEST") != "1" {
		t.Skip("set XFYUN_ISE_LIVE_TEST=1 to call iFlytek ISE")
	}
	audioPath := os.Getenv("XFYUN_ISE_LIVE_TEST_AUDIO")
	referenceText := os.Getenv("XFYUN_ISE_LIVE_TEST_TEXT")
	if audioPath == "" || referenceText == "" {
		t.Fatal(
			"XFYUN_ISE_LIVE_TEST_AUDIO and XFYUN_ISE_LIVE_TEST_TEXT are required",
		)
	}
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read live audio: %v", err)
	}
	configuration, err := config.LoadISE()
	if err != nil {
		t.Fatalf("load ISE config: %v", err)
	}
	evaluator, err := xfyun.NewEvaluator(
		xfyun.ISEConfig{
			Endpoint: configuration.Endpoint,
			Timeout:  configuration.Timeout,
		},
		configuration.AppID.Reveal(),
		configuration.APIKey.Reveal(),
		configuration.APISecret.Reveal(),
	)
	if err != nil {
		t.Fatalf("new ISE evaluator: %v", err)
	}
	result, err := evaluator.Evaluate(context.Background(), xfyun.EvaluationRequest{
		Audio:         audio,
		ReferenceText: referenceText,
		Category:      xfyun.CategoryReadSentence,
	})
	if err != nil {
		t.Fatalf("evaluate live audio: %v", err)
	}

	fieldNames := make(map[string]struct{}, len(result.AvailableFields))
	for _, field := range result.AvailableFields {
		fieldNames[field.Name] = struct{}{}
	}
	available := make([]string, 0, len(fieldNames))
	for name := range fieldNames {
		available = append(available, name)
	}
	sort.Strings(available)
	t.Logf("sid=%s", result.SessionID)
	t.Logf(
		"scores total=%s accuracy=%s fluency=%s integrity=%s standard=%s rejected=%s exception=%s",
		floatValue(result.Summary.TotalScore),
		floatValue(result.Summary.AccuracyScore),
		floatValue(result.Summary.FluencyScore),
		floatValue(result.Summary.IntegrityScore),
		floatValue(result.Summary.StandardScore),
		boolValue(result.Summary.Rejected),
		result.Summary.ExceptionInfo,
	)
	t.Logf("available_fields=%v", available)
}

func floatValue(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.6f", *value)
}

func boolValue(value *bool) string {
	if value == nil {
		return "unavailable"
	}
	return strconv.FormatBool(*value)
}
