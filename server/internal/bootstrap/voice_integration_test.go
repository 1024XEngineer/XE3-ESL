package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVoiceProductionCompositionBearerConcurrencyAndRestart(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	text := &voiceTextGenerator{}
	recognizer := &voiceRecognizer{}
	synthesizer := fake.NewFailingSpeechSynthesizer(
		fmt.Errorf("tts unavailable"),
	)
	vault := newVoiceTestVault(t)
	identityModule, agentModule, err := NewIdentityAndAgentModules(
		context.Background(),
		pool,
		nil,
		"",
		text,
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          vault,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("build production composition: %v", err)
	}
	server := httptest.NewServer(NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{identityModule, agentModule},
	).Handler())
	t.Cleanup(server.Close)

	token := registerAndLoginVoiceUser(t, server.URL, "voice-a@example.com")
	matterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Customer renewal"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	threadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, matterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)

	startPath := "/v1/agent-threads/" + threadID +
		"/voice-practice-sessions"
	archivedMatterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Archived scenario"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	archivedThreadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, archivedMatterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPatch,
		"/v1/matters/"+archivedMatterID,
		`{"status":"archived","expected_version":1}`,
		"",
		http.StatusOK,
	)
	archivedStart, err := voiceRawRequest(
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+archivedThreadID+
			"/voice-practice-sessions",
		nil,
		"start-archived-session",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = archivedStart.Body.Close()
	if archivedStart.StatusCode != http.StatusConflict {
		t.Fatalf("archived Matter Start status = %d", archivedStart.StatusCode)
	}
	replayMatterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Replay after archive"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	replayThreadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, replayMatterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	replayPath := "/v1/agent-threads/" + replayThreadID +
		"/voice-practice-sessions"
	firstReplay := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		replayPath,
		"",
		"start-replay-after-archive",
		http.StatusCreated,
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPatch,
		"/v1/matters/"+replayMatterID,
		`{"status":"archived","expected_version":1}`,
		"",
		http.StatusOK,
	)
	replayedAfterArchive := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		replayPath,
		"",
		"start-replay-after-archive",
		http.StatusCreated,
	)
	if replayedAfterArchive["practice_session_id"] !=
		firstReplay["practice_session_id"] {
		t.Fatalf(
			"archived Matter changed Start replay: first=%#v replay=%#v",
			firstReplay,
			replayedAfterArchive,
		)
	}
	state := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusCreated,
	)
	sessionID := state["practice_session_id"].(string)
	if state["matter"].(map[string]any)["matter_id"] != matterID ||
		state["thread_id"] != threadID {
		t.Fatalf("voice Session lost Thread/Matter binding: %#v", state)
	}
	replayedStart := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusCreated,
	)
	if replayedStart["practice_session_id"] != sessionID {
		t.Fatal("same Start idempotency key created a different Session")
	}
	nextMatterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Leadership transition"}`,
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPut,
		"/v1/agent-threads/"+threadID+"/active-matter",
		fmt.Sprintf(`{"matter_id":%q}`, nextMatterID),
		"",
		http.StatusOK,
	)
	replayedAfterMatterSwitch := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusCreated,
	)
	if replayedAfterMatterSwitch["practice_session_id"] != sessionID ||
		replayedAfterMatterSwitch["matter"].(map[string]any)["matter_id"] !=
			matterID {
		t.Fatalf(
			"Start replay rewrote immutable Matter Snapshot: %#v",
			replayedAfterMatterSwitch,
		)
	}
	conflictingStart, err := voiceRawRequest(
		server.URL,
		token,
		http.MethodPost,
		startPath,
		nil,
		"start-voice-session-0002",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = conflictingStart.Body.Close()
	if conflictingStart.StatusCode != http.StatusConflict {
		t.Fatalf("new key with active Plan status = %d", conflictingStart.StatusCode)
	}

	question := state["current_question"].(map[string]any)
	speechResponse, err := voiceRawRequest(
		server.URL,
		token,
		http.MethodGet,
		question["speech_path"].(string),
		nil,
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = speechResponse.Body.Close()
	if speechResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed TTS status = %d", speechResponse.StatusCode)
	}
	afterTTSFailure := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if afterTTSFailure["current_question"].(map[string]any)["content"] == "" {
		t.Fatal("TTS failure hid the persisted text Question")
	}
	oversized := bytes.NewReader(make([]byte, platformmedia.MaxAudioBytes+1))
	oversizedRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/v1/voice-practice-sessions/"+sessionID+
			"/questions/"+question["question_id"].(string)+
			"/transcription-candidates",
		oversized,
	)
	if err != nil {
		t.Fatal(err)
	}
	oversizedRequest.Header.Set("Authorization", "Bearer "+token)
	oversizedRequest.Header.Set("Idempotency-Key", "oversized-audio-0001")
	oversizedRequest.Header.Set("Content-Type", platformmedia.ContentTypeWAV)
	oversizedResponse, err := http.DefaultClient.Do(oversizedRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer oversizedResponse.Body.Close()
	if oversizedResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized WAV status = %d", oversizedResponse.StatusCode)
	}

	firstCandidate := createVoiceCandidate(
		t,
		server.URL,
		token,
		state,
		"round-1",
	)
	competingCandidate := createVoiceCandidate(
		t,
		server.URL,
		token,
		state,
		"round-1-competing",
	)
	state = voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			firstCandidate["candidate_id"].(string)+"/confirmations",
		"",
		"confirm-round-1-0001",
		http.StatusOK,
	)
	competingConfirm, err := voiceRawRequest(
		server.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			competingCandidate["candidate_id"].(string)+"/confirmations",
		nil,
		"confirm-round-1-competing",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = competingConfirm.Body.Close()
	if competingConfirm.StatusCode != http.StatusConflict {
		t.Fatalf(
			"second Candidate for one Question status = %d",
			competingConfirm.StatusCode,
		)
	}
	afterCompetingCandidate := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if afterCompetingCandidate["effective_turns"] != float64(1) {
		t.Fatalf(
			"competing Candidate advanced Practice: %#v",
			afterCompetingCandidate,
		)
	}
	state = transcribeAndConfirmVoiceRound(
		t,
		server.URL,
		token,
		state,
		"round-2",
	)

	candidate := createVoiceCandidate(
		t,
		server.URL,
		token,
		state,
		"round-3",
	)
	confirmPath := "/v1/transcription-candidates/" +
		candidate["candidate_id"].(string) + "/confirmations"
	const callers = 16
	results := make(chan map[string]any, callers)
	failures := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, requestErr := voiceRawRequest(
				server.URL,
				token,
				http.MethodPost,
				confirmPath,
				nil,
				"confirm-round-3-shared",
				"",
			)
			if requestErr != nil {
				failures <- requestErr
				return
			}
			defer response.Body.Close()
			body, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				failures <- readErr
				return
			}
			if response.StatusCode != http.StatusOK {
				failures <- fmt.Errorf(
					"confirm status %d: %s",
					response.StatusCode,
					body,
				)
				return
			}
			var decoded map[string]any
			if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
				failures <- decodeErr
				return
			}
			results <- decoded
		}()
	}
	wait.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	var reviewID string
	var thirdTurnID string
	for result := range results {
		if result["effective_turns"] != float64(3) ||
			result["session_completed"] != true {
			t.Errorf("unexpected third-round state: %#v", result)
		}
		turn := result["current_turn"].(map[string]any)
		if turn["candidate_id"] != candidate["candidate_id"] {
			t.Errorf("third-round response lost candidate identity: %#v", result)
		}
		currentTurnID, _ := turn["turn_id"].(string)
		if currentTurnID == "" {
			t.Errorf("third-round response has no Turn: %#v", result)
		} else if thirdTurnID == "" {
			thirdTurnID = currentTurnID
		} else if thirdTurnID != currentTurnID {
			t.Errorf("concurrent confirmations returned different Turns")
		}
		currentReview, _ := result["review"].(map[string]any)
		currentReviewID, _ := currentReview["review_id"].(string)
		if currentReviewID == "" {
			t.Errorf("third-round response has no Review: %#v", result)
		}
		if turn["review_id"] != currentReviewID {
			t.Errorf("current Turn does not link its Review: %#v", result)
		}
		if reviewID == "" {
			reviewID = currentReviewID
		} else if reviewID != currentReviewID {
			t.Errorf("concurrent confirmations returned different Reviews")
		}
	}
	if text.ReviewCalls() != 1 {
		t.Fatalf("Review generator calls = %d, want 1", text.ReviewCalls())
	}

	reviewResult := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews/"+reviewID,
		"",
		"",
		http.StatusOK,
	)
	if reviewResult["status"] != "completed" {
		t.Fatalf("formal Review not completed: %#v", reviewResult)
	}
	if reviewResult["implementation_version"] != voiceReviewImplementation ||
		reviewResult["source_turn_id"] != thirdTurnID ||
		reviewResult["source_turn_version"] !=
			"conversation-turn:evidence-v1" {
		t.Fatalf("formal Review lost source identity: %#v", reviewResult)
	}
	var evidenceCount int
	var matchedEvidenceCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
		    count(*)::int,
		    count(*) FILTER (
		        WHERE evidence.source_type = 'conversation_turn'
		          AND evidence.source_checksum ~ '^[0-9a-f]{64}$'
		          AND evidence.evidence_snapshot ? 'question_id'
		          AND evidence.evidence_snapshot ? 'answer_text'
		          AND EXISTS (
		            SELECT 1
		            FROM conversation_confirmed_turns turn
		            WHERE turn.owner_user_id = evidence.owner_user_id
		              AND turn.turn_id = evidence.source_id
		              AND turn.practice_session_id = $2
		              AND evidence.source_version =
		                'conversation-turn:evidence-v' ||
		                turn.evidence_version::text
		          )
		    )::int
		 FROM review_evidence evidence
		 WHERE evidence.review_id = $1`,
		reviewID,
		sessionID,
	).Scan(&evidenceCount, &matchedEvidenceCount); err != nil {
		t.Fatalf("read formal Review evidence: %v", err)
	}
	if evidenceCount != voiceTurnLimit ||
		matchedEvidenceCount != voiceTurnLimit {
		t.Fatalf(
			"formal Review evidence = %d/%d, want %d matched",
			matchedEvidenceCount,
			evidenceCount,
			voiceTurnLimit,
		)
	}
	otherToken := registerAndLoginVoiceUser(
		t,
		server.URL,
		"voice-b@example.com",
	)
	for _, path := range []string{
		"/v1/agent-threads/" + threadID + "/voice-practice-session",
		"/v1/formal-reviews/" + reviewID,
	} {
		response, requestErr := voiceRawRequest(
			server.URL,
			otherToken,
			http.MethodGet,
			path,
			nil,
			"",
			"",
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign resource %s status = %d", path, response.StatusCode)
		}
	}

	server.Close()
	if err := vault.Close(); err != nil {
		t.Fatalf("close first audio vault: %v", err)
	}
	restartedVault := newVoiceTestVault(t)
	restartedIdentity, restartedAgent, err := NewIdentityAndAgentModules(
		context.Background(),
		pool,
		nil,
		"",
		text,
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          restartedVault,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("restart production composition: %v", err)
	}
	restartedServer := httptest.NewServer(NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{restartedIdentity, restartedAgent},
	).Handler())
	t.Cleanup(restartedServer.Close)
	recovered := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if recovered["practice_session_id"] != sessionID ||
		recovered["effective_turns"] != float64(3) ||
		recovered["session_completed"] != true {
		t.Fatalf("restart recovery failed: %#v", recovered)
	}
	recoveredReview := recovered["review"].(map[string]any)
	if recoveredReview["review_id"] != reviewID {
		t.Fatalf("restart lost Review checkpoint: %#v", recovered)
	}
	recoveredFormalReview := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews/"+reviewID,
		"",
		"",
		http.StatusOK,
	)
	if recoveredFormalReview["review_id"] != reviewID ||
		recoveredFormalReview["source_turn_id"] != thirdTurnID ||
		recoveredFormalReview["source_turn_version"] !=
			reviewResult["source_turn_version"] ||
		recoveredFormalReview["result"] == nil {
		t.Fatalf(
			"restart lost formal Review evidence identity: %#v",
			recoveredFormalReview,
		)
	}
	nextSession := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0002",
		http.StatusCreated,
	)
	if nextSession["practice_session_id"] == sessionID {
		t.Fatal("completed Plan could not start a new idempotent Session")
	}

	text.FailNextReviews(1)
	for round := 1; round <= 2; round++ {
		nextSession = transcribeAndConfirmVoiceRound(
			t,
			restartedServer.URL,
			token,
			nextSession,
			fmt.Sprintf("retry-review-round-%d", round),
		)
	}
	retryCandidate := createVoiceCandidate(
		t,
		restartedServer.URL,
		token,
		nextSession,
		"retry-review-round-3",
	)
	failedConfirm, err := voiceRawRequest(
		restartedServer.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			retryCandidate["candidate_id"].(string)+"/confirmations",
		nil,
		"confirm-retry-review-round-3",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = failedConfirm.Body.Close()
	if failedConfirm.StatusCode != http.StatusServiceUnavailable ||
		failedConfirm.Header.Get("Retry-After") == "" {
		t.Fatalf(
			"failed Review status = %d, Retry-After = %q",
			failedConfirm.StatusCode,
			failedConfirm.Header.Get("Retry-After"),
		)
	}
	retrySessionID := nextSession["practice_session_id"].(string)
	var failedReviewID string
	var failedReviewStatus string
	var failedAttempts int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT r.id::text, r.status, count(a.id)::int
FROM reviews r
JOIN review_generation_attempts a ON a.review_id = r.id
WHERE r.practice_session_id = $1
GROUP BY r.id, r.status`,
		retrySessionID,
	).Scan(
		&failedReviewID,
		&failedReviewStatus,
		&failedAttempts,
	); err != nil {
		t.Fatalf("read failed Review attempt: %v", err)
	}
	if failedReviewStatus != "failed" || failedAttempts != 1 {
		t.Fatalf(
			"failed Review state = %s/%d attempts",
			failedReviewStatus,
			failedAttempts,
		)
	}
	retried := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	retriedReview := retried["review"].(map[string]any)
	retriedTurn := retried["current_turn"].(map[string]any)
	if retriedReview["review_id"] != failedReviewID ||
		retriedReview["status"] != "completed" ||
		retriedTurn["review_id"] != failedReviewID {
		t.Fatalf("Resume did not recover failed Review: %#v", retried)
	}
	var recoveredAttempts int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT count(*)::int
FROM review_generation_attempts
WHERE review_id = $1`,
		failedReviewID,
	).Scan(&recoveredAttempts); err != nil {
		t.Fatalf("count recovered Review attempts: %v", err)
	}
	if recoveredAttempts != 2 {
		t.Fatalf("recovered Review attempts = %d, want 2", recoveredAttempts)
	}

	quotaSession := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-quota",
		http.StatusCreated,
	)
	for round := 1; round <= 2; round++ {
		quotaSession = transcribeAndConfirmVoiceRound(
			t,
			restartedServer.URL,
			token,
			quotaSession,
			fmt.Sprintf("quota-review-round-%d", round),
		)
	}
	quotaCandidate := createVoiceCandidate(
		t,
		restartedServer.URL,
		token,
		quotaSession,
		"quota-review-round-3",
	)
	text.FailNextQuotaReview()
	quotaConfirm, err := voiceRawRequest(
		restartedServer.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			quotaCandidate["candidate_id"].(string)+"/confirmations",
		nil,
		"confirm-quota-review-round-3",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertQuotaExhaustedVoiceResponse(t, quotaConfirm)
	quotaReviewCalls := text.ReviewCalls()

	// A fresh composition root exercises a real service restart. The failed
	// Review row must remain terminal across every subsequent Resume.
	restartedServer.Close()
	if err := restartedVault.Close(); err != nil {
		t.Fatalf("close retry audio vault: %v", err)
	}
	terminalVault := newVoiceTestVault(t)
	terminalIdentity, terminalAgent, err := NewIdentityAndAgentModules(
		context.Background(),
		pool,
		nil,
		"",
		text,
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          terminalVault,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("restart terminal Review composition: %v", err)
	}
	terminalServer := httptest.NewServer(NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{terminalIdentity, terminalAgent},
	).Handler())
	t.Cleanup(terminalServer.Close)
	for resume := 0; resume < 3; resume++ {
		response, err := voiceRawRequest(
			terminalServer.URL,
			token,
			http.MethodGet,
			"/v1/agent-threads/"+threadID+"/voice-practice-session",
			nil,
			"",
			"",
		)
		if err != nil {
			t.Fatal(err)
		}
		assertQuotaExhaustedVoiceResponse(t, response)
	}
	if got := text.ReviewCalls(); got != quotaReviewCalls {
		t.Fatalf(
			"terminal Review Resume calls = %d, want unchanged %d",
			got,
			quotaReviewCalls,
		)
	}
}

func assertQuotaExhaustedVoiceResponse(
	t *testing.T,
	response *http.Response,
) {
	t.Helper()
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable ||
		response.Header.Get("Retry-After") != "" {
		t.Fatalf(
			"quota response status = %d, Retry-After = %q: %s",
			response.StatusCode,
			response.Header.Get("Retry-After"),
			raw,
		)
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode quota response: %v", err)
	}
	if payload.Error.Code != "quota_exhausted" ||
		payload.Error.Retryable {
		t.Fatalf("quota response = %#v", payload)
	}
}

func registerAndLoginVoiceUser(
	t *testing.T,
	baseURL string,
	email string,
) string {
	t.Helper()
	credentials := fmt.Sprintf(
		`{"email":%q,"password":"Correct horse battery staple 42!"}`,
		email,
	)
	voiceJSONRequest(
		t,
		baseURL,
		"",
		http.MethodPost,
		"/v1/auth/register",
		credentials,
		"",
		http.StatusCreated,
	)
	login := voiceJSONRequest(
		t,
		baseURL,
		"",
		http.MethodPost,
		"/v1/auth/login",
		credentials,
		"",
		http.StatusOK,
	)
	return login["session_token"].(string)
}

func transcribeAndConfirmVoiceRound(
	t *testing.T,
	baseURL string,
	token string,
	state map[string]any,
	key string,
) map[string]any {
	t.Helper()
	candidate := createVoiceCandidate(t, baseURL, token, state, key)
	return voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			candidate["candidate_id"].(string)+"/confirmations",
		"",
		"confirm-"+key+"-0001",
		http.StatusOK,
	)
}

func createVoiceCandidate(
	t *testing.T,
	baseURL string,
	token string,
	state map[string]any,
	key string,
) map[string]any {
	t.Helper()
	question := state["current_question"].(map[string]any)
	path := "/v1/voice-practice-sessions/" +
		state["practice_session_id"].(string) + "/questions/" +
		question["question_id"].(string) + "/transcription-candidates"
	response, err := voiceRawRequest(
		baseURL,
		token,
		http.MethodPost,
		path,
		bytes.NewReader(testWAV()),
		"transcribe-"+key+"-0001",
		platformmedia.ContentTypeWAV,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create candidate status %d: %s", response.StatusCode, body)
	}
	var candidate map[string]any
	if err := json.Unmarshal(body, &candidate); err != nil {
		t.Fatal(err)
	}
	if _, leaked := candidate["provider_request_id"]; leaked {
		t.Fatalf("candidate leaked provider audit ID: %#v", candidate)
	}
	if _, leaked := candidate["provider"]; leaked {
		t.Fatalf("candidate leaked provider implementation: %#v", candidate)
	}
	return candidate
}

func voiceJSONRequest(
	t *testing.T,
	baseURL string,
	token string,
	method string,
	path string,
	body string,
	idempotencyKey string,
	wantStatus int,
) map[string]any {
	t.Helper()
	var input io.Reader
	contentType := ""
	if body != "" {
		input = strings.NewReader(body)
		contentType = "application/json"
	}
	response, err := voiceRawRequest(
		baseURL,
		token,
		method,
		path,
		input,
		idempotencyKey,
		contentType,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status %d: %s", method, path, response.StatusCode, raw)
	}
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode %s %s: %v", method, path, err)
	}
	return decoded
}

func voiceRawRequest(
	baseURL string,
	token string,
	method string,
	path string,
	body io.Reader,
	idempotencyKey string,
	contentType string,
) (*http.Response, error) {
	request, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return http.DefaultClient.Do(request)
}

type voiceTextGenerator struct {
	questionCalls         atomic.Int64
	reviewCalls           atomic.Int64
	reviewFailuresPending atomic.Int64
	quotaReviewPending    atomic.Bool
}

func (generator *voiceTextGenerator) Generate(
	_ context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	last := request.Messages[len(request.Messages)-1].Content
	if strings.HasPrefix(last, "Review these ") {
		generator.reviewCalls.Add(1)
		if generator.quotaReviewPending.CompareAndSwap(true, false) {
			return ai.TextResult{}, ai.NewGenerationError(
				ai.ErrorQuotaExhausted,
				http.StatusTooManyRequests,
				"FreeTierOnly",
				"review-quota-request",
				errors.New("provider quota exhausted"),
			)
		}
		for {
			pending := generator.reviewFailuresPending.Load()
			if pending == 0 {
				break
			}
			if generator.reviewFailuresPending.CompareAndSwap(
				pending,
				pending-1,
			) {
				return ai.TextResult{}, fmt.Errorf(
					"review provider unavailable",
				)
			}
		}
		return ai.TextResult{
			ID:       "review-completion",
			Provider: "fake",
			Model:    "fake-text-v1",
			Content:  `{"overall_score":86,"summary":"Clear answers with useful examples.","conclusions":[{"key":"overall","category":"STRUCTURE","message":"The answers are clear and evidence based.","suggestion":"Make each result more measurable."}]}`,
		}, nil
	}
	call := generator.questionCalls.Add(1)
	return ai.TextResult{
		ID:       fmt.Sprintf("question-completion-%d", call),
		Provider: "fake",
		Model:    "fake-text-v1",
		Content:  fmt.Sprintf("Tell me about professional example %d?", call),
	}, nil
}

func (generator *voiceTextGenerator) ReviewCalls() int64 {
	return generator.reviewCalls.Load()
}

func (generator *voiceTextGenerator) FailNextReviews(count int64) {
	generator.reviewFailuresPending.Store(count)
}

func (generator *voiceTextGenerator) FailNextQuotaReview() {
	generator.quotaReviewPending.Store(true)
}

type voiceRecognizer struct {
	calls atomic.Int64
}

func (recognizer *voiceRecognizer) Transcribe(
	_ context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	if err := ai.ValidateTranscriptionRequest(request); err != nil {
		return ai.TranscriptionResult{}, err
	}
	call := recognizer.calls.Add(1)
	return ai.TranscriptionResult{
		ID:         fmt.Sprintf("asr-result-%d", call),
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: fmt.Sprintf("Confirmed answer number %d.", call),
	}, nil
}

func newVoiceTestVault(t *testing.T) *platformmedia.TemporaryAudioVault {
	t.Helper()
	vault, err := platformmedia.NewTemporaryAudioVault(
		platformmedia.TemporaryAudioVaultConfig{
			ScratchDirectory: t.TempDir(),
			Lifetime:         time.Minute,
			MaxItems:         1,
			MaxBytes:         platformmedia.MaxAudioBytes,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	return vault
}

func testWAV() []byte {
	const (
		sampleRate    = 16_000
		bitsPerSample = 16
		channels      = 1
		samples       = 1600
	)
	dataSize := samples * channels * bitsPerSample / 8
	payload := make([]byte, 44+dataSize)
	copy(payload[0:4], "RIFF")
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(payload)-8))
	copy(payload[8:12], "WAVE")
	copy(payload[12:16], "fmt ")
	binary.LittleEndian.PutUint32(payload[16:20], 16)
	binary.LittleEndian.PutUint16(payload[20:22], 1)
	binary.LittleEndian.PutUint16(payload[22:24], channels)
	binary.LittleEndian.PutUint32(payload[24:28], sampleRate)
	byteRate := sampleRate * channels * bitsPerSample / 8
	binary.LittleEndian.PutUint32(payload[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(
		payload[32:34],
		channels*bitsPerSample/8,
	)
	binary.LittleEndian.PutUint16(payload[34:36], bitsPerSample)
	copy(payload[36:40], "data")
	binary.LittleEndian.PutUint32(payload[40:44], uint32(dataSize))
	return payload
}

func voiceIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	adminConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(context.Background(), adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "voice_vertical_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+identifier,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		)
	})
	scoped, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := scoped.Query()
	query.Set("search_path", schema)
	scoped.RawQuery = query.Encode()
	runner, err := migration.Open(scoped.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations 000001-000008: %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), scoped.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
