package evaluationapi

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/report"
	"github.com/gin-gonic/gin"
)

func TestEvaluationRoutesShareCanonicalResourceParameterNames(t *testing.T) {
	router := gin.New()
	noop := func(*gin.Context) {}
	router.GET("/v1/practice-sessions/:practice_session_id", noop)
	router.GET("/v1/agent-messages/:message_id/translation", noop)

	(&HTTPHandler{}).RegisterRoutes(router)
}

func TestPublicSpeechResultDoesNotExposeProviderLineage(t *testing.T) {
	t.Parallel()
	pronunciation := 91.0
	fluency := 82.0
	integrity := 88.0
	speed := 126.0
	result := evaluation.SpeechResult{
		SchemaVersion:      "speech-feedback/v1",
		ScoreabilityStatus: "PROVISIONAL",
		Summary:            "Feedback is ready.",
		ReasonCodes:        []string{},
		Acoustic: evaluation.AcousticCheckpoint{
			Status:           evaluation.AcousticAssessed,
			Pronunciation:    &pronunciation,
			Fluency:          &fluency,
			Integrity:        &integrity,
			SpeakingSpeedWPM: &speed,
			Provider:         "iflytek",
			ProviderSession:  "provider-session-1",
		},
	}
	encoded, _, err := evaluation.EncodeStrict(result)
	if err != nil {
		t.Fatalf("EncodeStrict: %v", err)
	}
	public, err := publicResult(evaluation.Record{
		Kind:   evaluation.KindPracticeTurnFeedback,
		Result: encoded,
	})
	if err != nil {
		t.Fatalf("publicResult: %v", err)
	}
	payload, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(payload), "provider") ||
		strings.Contains(string(payload), "provider-session-1") {
		t.Fatalf("public payload leaked provider lineage: %s", payload)
	}
	if !strings.Contains(string(payload), `"pronunciation":91`) {
		t.Fatalf("public payload lost product acoustic score: %s", payload)
	}
}

func TestDecodeCursorRejectsTrailingJSON(t *testing.T) {
	t.Parallel()
	boundary := report.HistoryBoundary{
		CreatedAt: time.Date(2026, 8, 15, 1, 2, 3, 4, time.UTC),
		ReportID:  "7311adb4-1ea0-41c7-8c6d-f336f854f1c6",
	}
	valid := encodeCursor(boundary)
	decoded, err := decodeCursor(valid)
	if err != nil || decoded != boundary {
		t.Fatalf("decodeCursor(valid) = %#v, %v", decoded, err)
	}
	trailing := base64.RawURLEncoding.EncodeToString([]byte(
		`{"created_at":"2026-08-15T01:02:03.000000004Z","report_id":"7311adb4-1ea0-41c7-8c6d-f336f854f1c6"}{}`,
	))
	if _, err := decodeCursor(trailing); err == nil {
		t.Fatal("decodeCursor accepted trailing JSON")
	}
}
