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

	agent "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/fake"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testReviewHistoryCursorKey = []byte(
	"0123456789abcdef0123456789abcdef",
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
	objects := newVoiceObjectStore()
	vault := newVoiceTestVault(t)
	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("build Preparation catalog: %v", err)
	}
	server := newVoiceProductionIntegrationServer(
		t,
		pool,
		catalog,
		text,
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          vault,
			ObjectStore:             objects,
			AudioStagedTTL:          time.Hour,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
			ReviewHistoryCursorKey:  testReviewHistoryCursorKey,
		},
	)

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
	formalContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		threadID,
		matterID,
		"primary",
	)

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
	archivedContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		archivedThreadID,
		archivedMatterID,
		"archived",
	)
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
	if archivedStart.StatusCode != http.StatusNotFound {
		t.Fatalf("archived Matter Start status = %d", archivedStart.StatusCode)
	}
	assertNoLegacyVoiceSession(
		t,
		pool,
		archivedContext.SessionID,
		archivedThreadID,
	)
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
	replayContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		replayThreadID,
		replayMatterID,
		"archive-replay",
	)
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
	if firstReplay["practice_session_id"] != replayContext.SessionID {
		t.Fatalf(
			"voice Start did not activate formal Context Session: %#v",
			firstReplay,
		)
	}
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
	if replayedAfterArchive["practice_session_id"] != replayContext.SessionID {
		t.Fatalf(
			"archived Matter replay lost original Session: %#v",
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
	if sessionID != formalContext.SessionID ||
		state["practice_plan_id"] != formalContext.PlanID ||
		state["matter"].(map[string]any)["matter_id"] != matterID ||
		state["thread_id"] != threadID {
		t.Fatalf("voice Session lost formal Context binding: %#v", state)
	}
	assertNoLegacyVoiceSession(t, pool, sessionID, threadID)
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
			"Start replay after active Matter switch = %#v",
			replayedAfterMatterSwitch,
		)
	}
	resumedAfterMatterSwitch := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if resumedAfterMatterSwitch["practice_session_id"] != sessionID ||
		resumedAfterMatterSwitch["matter"].(map[string]any)["matter_id"] !=
			matterID {
		t.Fatalf(
			"GET recovery reinterpreted active Matter: %#v",
			resumedAfterMatterSwitch,
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
	if conflictingStart.StatusCode != http.StatusNotFound {
		t.Fatalf(
			"new key without exact Context status = %d",
			conflictingStart.StatusCode,
		)
	}
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPut,
		"/v1/agent-threads/"+threadID+"/active-matter",
		fmt.Sprintf(`{"matter_id":%q}`, matterID),
		"",
		http.StatusOK,
	)

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
	firstAudioAssetID, _ := state["current_turn"].(map[string]any)["audio_asset_id"].(string)
	if firstAudioAssetID == "" {
		t.Fatalf("first confirmed Turn has no AudioAsset: %#v", state)
	}
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
	var completedAudioAssetID string
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
		currentAudioAssetID, _ := turn["audio_asset_id"].(string)
		if currentAudioAssetID == "" {
			t.Errorf("current Turn does not link its AudioAsset: %#v", result)
		}
		if completedAudioAssetID == "" {
			completedAudioAssetID = currentAudioAssetID
		} else if completedAudioAssetID != currentAudioAssetID {
			t.Errorf("concurrent confirmations returned different AudioAssets")
		}
	}
	completedState := waitForVoicePracticeState(
		t,
		server.URL,
		token,
		threadID,
		func(state map[string]any) bool {
			sessionReview, ok := state["review"].(map[string]any)
			return ok && sessionReview["status"] == "completed"
		},
	)
	completedReview := completedState["review"].(map[string]any)
	reviewID, _ := completedReview["review_id"].(string)
	completedTurn := completedState["current_turn"].(map[string]any)
	if reviewID == "" || completedTurn["review_id"] != reviewID {
		t.Fatalf(
			"async Review was not linked to completed Turn: %#v",
			completedState,
		)
	}
	if text.ReviewCalls() != 1 {
		t.Fatalf("Review generator calls = %d, want 1", text.ReviewCalls())
	}
	replayedAfterCompletion := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusCreated,
	)
	if replayedAfterCompletion["practice_session_id"] != sessionID ||
		replayedAfterCompletion["session_completed"] != true {
		t.Fatalf(
			"completed Start replay did not return original Session: %#v",
			replayedAfterCompletion,
		)
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
	var reviewOwnerID string
	var reviewCreatedAt time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT owner_user_id::text, created_at
		FROM reviews
		WHERE id = $1
	`, reviewID).Scan(&reviewOwnerID, &reviewCreatedAt); err != nil {
		t.Fatalf("read formal Review history key: %v", err)
	}
	olderHistoryReview := completeBootstrapHistoryReview(
		t,
		pool,
		reviewOwnerID,
		"restart-cursor-older",
	)
	legacyHistorySummary := strings.Repeat("legacy summary ", 1100)
	legacyHistoryResult := *olderHistoryReview.Result
	legacyHistoryResult.Summary = legacyHistorySummary
	legacyHistoryPayload, err := json.Marshal(legacyHistoryResult)
	if err != nil {
		t.Fatalf("marshal legacy HTTP Review: %v", err)
	}
	if len(legacyHistoryPayload) <= 12*1024 {
		t.Fatalf(
			"legacy HTTP Review bytes = %d, want over 12 KiB",
			len(legacyHistoryPayload),
		)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE reviews SET result = $1::jsonb WHERE id = $2`,
		legacyHistoryPayload,
		olderHistoryReview.ID,
	); err != nil {
		t.Fatalf("stage legacy HTTP Review: %v", err)
	}
	legacyHistoryItem := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews/"+olderHistoryReview.ID,
		"",
		"",
		http.StatusOK,
	)
	legacyHistoryHTTPResult, ok :=
		legacyHistoryItem["result"].(map[string]any)
	if !ok ||
		legacyHistoryHTTPResult["summary"] != legacyHistorySummary {
		t.Fatalf(
			"legacy HTTP Review result = %#v",
			legacyHistoryItem,
		)
	}
	oldestHistoryReview := completeBootstrapHistoryReview(
		t,
		pool,
		reviewOwnerID,
		"restart-cursor-oldest",
	)
	for index, item := range []review.FormalReview{
		olderHistoryReview,
		oldestHistoryReview,
	} {
		if _, err := pool.Exec(
			context.Background(),
			`UPDATE reviews SET created_at = $1 WHERE id = $2`,
			reviewCreatedAt.Add(-time.Duration(index+1)*time.Minute),
			item.ID,
		); err != nil {
			t.Fatalf("set restart cursor Review %d key: %v", index, err)
		}
	}
	historyResult := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews?limit=1",
		"",
		"",
		http.StatusOK,
	)
	historyItems, ok := historyResult["items"].([]any)
	if !ok || len(historyItems) != 1 {
		t.Fatalf("formal Review history items = %#v, want one", historyResult)
	}
	historyReview, ok := historyItems[0].(map[string]any)
	if !ok || historyReview["review_id"] != reviewID ||
		historyReview["source_turn_id"] != thirdTurnID {
		t.Fatalf("formal Review history lost source identity: %#v", historyResult)
	}
	restartHistoryCursor, ok := historyResult["next_cursor"].(string)
	if !ok || restartHistoryCursor == "" {
		t.Fatalf(
			"formal Review history omitted restart cursor: %#v",
			historyResult,
		)
	}
	var evidenceCount int
	var matchedEvidenceCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
		    count(*)::int,
		    count(*) FILTER (
		        WHERE evidence.source_type = 'conversation_turn'
		          AND evidence.target_kind IN ('conclusion', 'feedback_item')
		          AND evidence.field = 'answer_text'
		          AND evidence.anchor_kind = 'exact_quote'
		          AND evidence.quote <> ''
		          AND evidence.start_utf8_byte >= 0
		          AND evidence.end_utf8_byte > evidence.start_utf8_byte
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
	const expectedEvidenceCount = 6
	if evidenceCount != expectedEvidenceCount ||
		matchedEvidenceCount != expectedEvidenceCount {
		t.Fatalf(
			"formal Review evidence = %d/%d, want %d matched",
			matchedEvidenceCount,
			evidenceCount,
			expectedEvidenceCount,
		)
	}
	playback := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/audio-assets/"+completedAudioAssetID+"/playback",
		"",
		"",
		http.StatusOK,
	)
	if !strings.HasPrefix(
		playback["playback_url"].(string),
		"https://private-audio.example.invalid/",
	) {
		t.Fatalf("unexpected protected playback capability: %#v", playback)
	}
	otherToken := registerAndLoginVoiceUser(
		t,
		server.URL,
		"voice-b@example.com",
	)
	otherHistory := voiceJSONRequest(
		t,
		server.URL,
		otherToken,
		http.MethodGet,
		"/v1/formal-reviews",
		"",
		"",
		http.StatusOK,
	)
	if items, ok := otherHistory["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("foreign user history = %#v, want empty", otherHistory)
	}
	for _, path := range []string{
		"/v1/agent-threads/" + threadID + "/voice-practice-session",
		"/v1/formal-reviews/" + reviewID,
		"/v1/audio-assets/" + completedAudioAssetID + "/playback",
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
	restartedServer := newVoiceProductionIntegrationServer(
		t,
		pool,
		catalog,
		text,
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          restartedVault,
			ObjectStore:             objects,
			AudioStagedTTL:          time.Hour,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
			ReviewHistoryCursorKey:  testReviewHistoryCursorKey,
		},
	)
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
	recoveredHistory := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews",
		"",
		"",
		http.StatusOK,
	)
	recoveredHistoryItems, ok := recoveredHistory["items"].([]any)
	if !ok || len(recoveredHistoryItems) != 3 {
		t.Fatalf(
			"restart lost formal Review history: %#v",
			recoveredHistory,
		)
	}
	recoveredHistoryReview, ok :=
		recoveredHistoryItems[0].(map[string]any)
	if !ok || recoveredHistoryReview["review_id"] != reviewID ||
		recoveredHistoryReview["source_turn_id"] != thirdTurnID {
		t.Fatalf(
			"restart changed formal Review history: %#v",
			recoveredHistory,
		)
	}
	for index, reviewID := range []string{
		olderHistoryReview.ID,
		oldestHistoryReview.ID,
	} {
		item, ok := recoveredHistoryItems[index+1].(map[string]any)
		if !ok || item["review_id"] != reviewID {
			t.Fatalf(
				"restart history item %d = %#v, want %s",
				index+1,
				recoveredHistoryItems[index+1],
				reviewID,
			)
		}
	}
	restartedContinuation := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews?limit=1&cursor="+
			url.QueryEscape(restartHistoryCursor),
		"",
		"",
		http.StatusOK,
	)
	continuationItems, ok :=
		restartedContinuation["items"].([]any)
	if !ok || len(continuationItems) != 1 {
		t.Fatalf(
			"restart cursor continuation = %#v",
			restartedContinuation,
		)
	}
	continuedReview, ok := continuationItems[0].(map[string]any)
	if !ok ||
		continuedReview["review_id"] != olderHistoryReview.ID {
		t.Fatalf(
			"restart cursor first continuation item = %#v",
			restartedContinuation,
		)
	}
	continuedResult, ok := continuedReview["result"].(map[string]any)
	if !ok || continuedResult["summary"] != legacyHistorySummary {
		t.Fatalf(
			"restart cursor lost legacy Review result: %#v",
			restartedContinuation,
		)
	}
	restartedTailCursor, ok :=
		restartedContinuation["next_cursor"].(string)
	if !ok || restartedTailCursor == "" {
		t.Fatalf(
			"restart cursor first continuation omitted tail: %#v",
			restartedContinuation,
		)
	}
	restartedTail := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/formal-reviews?limit=1&cursor="+
			url.QueryEscape(restartedTailCursor),
		"",
		"",
		http.StatusOK,
	)
	tailItems, ok := restartedTail["items"].([]any)
	if !ok || len(tailItems) != 1 {
		t.Fatalf("restart cursor tail = %#v", restartedTail)
	}
	tailReview, ok := tailItems[0].(map[string]any)
	if !ok || tailReview["review_id"] != oldestHistoryReview.ID {
		t.Fatalf("restart cursor tail item = %#v", restartedTail)
	}
	if _, present := restartedTail["next_cursor"]; present {
		t.Fatalf(
			"restart cursor tail exposed another cursor: %#v",
			restartedTail,
		)
	}
	recoveredTurn := recovered["current_turn"].(map[string]any)
	if recoveredTurn["audio_asset_id"] != completedAudioAssetID {
		t.Fatalf("restart lost AudioAsset checkpoint: %#v", recovered)
	}
	voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodDelete,
		"/v1/audio-assets/"+completedAudioAssetID,
		"",
		"",
		http.StatusNoContent,
	)
	deletedPlayback, err := voiceRawRequest(
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/audio-assets/"+completedAudioAssetID+"/playback",
		nil,
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = deletedPlayback.Body.Close()
	if deletedPlayback.StatusCode != http.StatusNotFound {
		t.Fatalf(
			"deleted AudioAsset playback status = %d",
			deletedPlayback.StatusCode,
		)
	}
	replayedAfterAudioDelete := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		confirmPath,
		"",
		"confirm-round-3-shared",
		http.StatusOK,
	)
	replayedDeletedTurn := replayedAfterAudioDelete["current_turn"].(map[string]any)
	if replayedDeletedTurn["turn_id"] != thirdTurnID ||
		replayedDeletedTurn["review_id"] != reviewID {
		t.Fatalf(
			"deleted recording replay changed durable Turn: %#v",
			replayedAfterAudioDelete,
		)
	}
	if _, present := replayedDeletedTurn["audio_asset_id"]; present {
		t.Fatalf(
			"deleted recording replay exposed AudioAsset: %#v",
			replayedAfterAudioDelete,
		)
	}
	afterAudioDelete := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if _, present := afterAudioDelete["current_turn"].(map[string]any)["audio_asset_id"]; present {
		t.Fatalf("deleted AudioAsset remained in restored state: %#v", afterAudioDelete)
	}
	retryContext := createVoiceFormalThreadContext(
		t,
		restartedServer.URL,
		token,
		"Review retry",
		"retry-review",
	)
	retryStartPath := "/v1/agent-threads/" + retryContext.ThreadID +
		"/voice-practice-sessions"
	nextSession := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		retryStartPath,
		"",
		"start-voice-session-0002",
		http.StatusCreated,
	)
	if nextSession["practice_session_id"] != retryContext.SessionID {
		t.Fatalf(
			"voice Start did not use next formal Context Session: %#v",
			nextSession,
		)
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
	failedConfirm := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			retryCandidate["candidate_id"].(string)+"/confirmations",
		"",
		"confirm-retry-review-round-3",
		http.StatusOK,
	)
	if failedConfirm["session_completed"] != true {
		t.Fatalf("failed Review blocked answer submission: %#v", failedConfirm)
	}
	retrySessionID := nextSession["practice_session_id"].(string)
	var failedReviewID string
	var failedReviewStatus string
	var failedAttempts int
	waitForVoiceCondition(t, "failed Review attempt", func() bool {
		err := pool.QueryRow(
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
		)
		return err == nil &&
			failedReviewStatus == "failed" &&
			failedAttempts == 1
	})
	if failedReviewStatus != "failed" || failedAttempts != 1 {
		t.Fatalf(
			"failed Review state = %s/%d attempts",
			failedReviewStatus,
			failedAttempts,
		)
	}
	retried := waitForVoicePracticeState(
		t,
		restartedServer.URL,
		token,
		retryContext.ThreadID,
		func(state map[string]any) bool {
			sessionReview, ok := state["review"].(map[string]any)
			return ok && sessionReview["status"] == "completed"
		},
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

	quotaContext := createVoiceFormalThreadContext(
		t,
		restartedServer.URL,
		token,
		"Quota terminal review",
		"quota-review",
	)
	quotaStartPath := "/v1/agent-threads/" + quotaContext.ThreadID +
		"/voice-practice-sessions"
	quotaSession := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		quotaStartPath,
		"",
		"start-voice-session-quota",
		http.StatusCreated,
	)
	if quotaSession["practice_session_id"] != quotaContext.SessionID {
		t.Fatalf(
			"quota voice Start did not use formal Context Session: %#v",
			quotaSession,
		)
	}
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
	quotaConfirm := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodPost,
		"/v1/transcription-candidates/"+
			quotaCandidate["candidate_id"].(string)+"/confirmations",
		"",
		"confirm-quota-review-round-3",
		http.StatusOK,
	)
	if quotaConfirm["session_completed"] != true {
		t.Fatalf("quota Review blocked answer submission: %#v", quotaConfirm)
	}
	waitForVoiceCondition(t, "terminal quota Review", func() bool {
		var status string
		var category string
		err := pool.QueryRow(
			context.Background(),
			`SELECT status, coalesce(stable_error_category, '')
FROM reviews
WHERE practice_session_id = $1`,
			quotaSession["practice_session_id"].(string),
		).Scan(&status, &category)
		return err == nil &&
			status == "failed" &&
			category == "quota_exhausted"
	})
	quotaReviewCalls := text.ReviewCalls()

	// A fresh composition root exercises a real service restart. The failed
	// Review row must remain terminal across every subsequent Resume without
	// blocking access to the completed Practice Session.
	restartedServer.Close()
	if err := restartedVault.Close(); err != nil {
		t.Fatalf("close retry audio vault: %v", err)
	}
	terminalVault := newVoiceTestVault(t)
	terminalServer := newVoiceProductionIntegrationServer(
		t,
		pool,
		catalog,
		text,
		VoiceConfiguration{
			Recognizer:              recognizer,
			Synthesizer:             synthesizer,
			TemporaryAudio:          terminalVault,
			ObjectStore:             objects,
			AudioStagedTTL:          time.Hour,
			ASRLease:                5 * time.Second,
			ReviewGenerationTimeout: 2 * time.Second,
			ReviewHistoryCursorKey:  testReviewHistoryCursorKey,
		},
	)
	for resume := 0; resume < 3; resume++ {
		resumed := voiceJSONRequest(
			t,
			terminalServer.URL,
			token,
			http.MethodGet,
			"/v1/agent-threads/"+quotaContext.ThreadID+
				"/voice-practice-session",
			"",
			"",
			http.StatusOK,
		)
		if resumed["session_completed"] != true {
			t.Fatalf(
				"terminal Review Resume did not restore completed Session: %#v",
				resumed,
			)
		}
	}
	if got := text.ReviewCalls(); got != quotaReviewCalls {
		t.Fatalf(
			"terminal Review Resume calls = %d, want unchanged %d",
			got,
			quotaReviewCalls,
		)
	}

	if _, err := pool.Exec(context.Background(), `
		UPDATE reviews
		SET result = jsonb_set(
			result,
			'{summary}',
			to_jsonb($1::text)
		)
		WHERE id = $2
	`, " \t\n", reviewID); err != nil {
		t.Fatalf("stage corrupt persisted Review for HTTP boundary: %v", err)
	}
	for _, path := range []string{
		"/v1/formal-reviews/" + reviewID,
		"/v1/formal-reviews",
	} {
		failure := voiceJSONRequest(
			t,
			terminalServer.URL,
			token,
			http.MethodGet,
			path,
			"",
			"",
			http.StatusInternalServerError,
		)
		errorObject, ok := failure["error"].(map[string]any)
		if !ok ||
			errorObject["code"] != "internal_error" ||
			errorObject["retryable"] != true {
			t.Fatalf(
				"corrupt persisted Review response for %s = %#v",
				path,
				failure,
			)
		}
	}
}

func TestVoiceRecordingCleanupWinNeverLeavesRecoverableTurn(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	text := &voiceTextGenerator{}
	recognizer := &voiceRecognizer{}
	synthesizer := fake.NewFailingSpeechSynthesizer(
		fmt.Errorf("tts unavailable"),
	)
	objects := newVoiceObjectStore()
	catalog, err := preparation.NewBuiltinCatalog()
	if err != nil {
		t.Fatalf("build cleanup-race Preparation catalog: %v", err)
	}
	buildServer := func(vault *platformmedia.TemporaryAudioVault) *httptest.Server {
		t.Helper()
		return newVoiceProductionIntegrationServer(
			t,
			pool,
			catalog,
			text,
			VoiceConfiguration{
				Recognizer:              recognizer,
				Synthesizer:             synthesizer,
				TemporaryAudio:          vault,
				ObjectStore:             objects,
				AudioStagedTTL:          time.Hour,
				ASRLease:                5 * time.Second,
				ReviewGenerationTimeout: 2 * time.Second,
				ReviewHistoryCursorKey:  testReviewHistoryCursorKey,
			},
		)
	}

	vault := newVoiceTestVault(t)
	server := buildServer(vault)
	token := registerAndLoginVoiceUser(
		t,
		server.URL,
		"voice-cleanup-race@example.com",
	)
	matterID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Cleanup race"}`,
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
	formalContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		threadID,
		matterID,
		"cleanup-race",
	)
	state := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads/"+threadID+"/voice-practice-sessions",
		"",
		"start-cleanup-race",
		http.StatusCreated,
	)
	if state["practice_session_id"] != formalContext.SessionID ||
		state["practice_plan_id"] != formalContext.PlanID {
		t.Fatalf(
			"cleanup-race Start lost formal Context binding: %#v",
			state,
		)
	}
	assertNoLegacyVoiceSession(t, pool, formalContext.SessionID, threadID)
	candidate := createVoiceCandidate(
		t,
		server.URL,
		token,
		state,
		"cleanup-race",
	)
	candidateID := candidate["candidate_id"].(string)
	var audioAssetID string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT assets.audio_asset_id
		 FROM conversation_audio_assets AS assets
		 JOIN conversation_transcript_candidates AS candidates
		   ON candidates.owner_user_id = assets.owner_user_id
		  AND candidates.reservation_id = assets.upload_request_id
		 WHERE candidates.candidate_id = $1`,
		candidateID,
	).Scan(&audioAssetID); err != nil {
		t.Fatalf("find staged recording: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE conversation_audio_assets
		 SET created_at = transaction_timestamp() - interval '2 hours',
		     updated_at = transaction_timestamp() - interval '2 hours',
		     staged_until = transaction_timestamp() - interval '1 hour'
		 WHERE audio_asset_id = $1`,
		audioAssetID,
	); err != nil {
		t.Fatalf("expire staged recording: %v", err)
	}
	audioRepository, err := conversationpostgres.NewAudioAssetRepository(pool)
	if err != nil {
		t.Fatalf("create cleanup repository: %v", err)
	}
	claims, err := audioRepository.ClaimExpiredUnconfirmed(
		context.Background(),
		time.Minute,
		10,
	)
	if err != nil || len(claims) != 1 ||
		claims[0].Asset.ID != audioAssetID {
		t.Fatalf("cleanup claim = %#v, %v", claims, err)
	}

	confirmPath := "/v1/transcription-candidates/" + candidateID +
		"/confirmations"
	assertFailedConfirm := func(baseURL string) {
		t.Helper()
		response, requestErr := voiceRawRequest(
			baseURL,
			token,
			http.MethodPost,
			confirmPath,
			nil,
			"confirm-cleanup-race",
			"",
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		raw, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusConflict {
			t.Fatalf(
				"cleanup-won confirmation status = %d: %s",
				response.StatusCode,
				raw,
			)
		}
		var failure struct {
			Error struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &failure); err != nil {
			t.Fatalf("decode cleanup-won confirmation: %v", err)
		}
		if failure.Error.Code != "resource_conflict" ||
			failure.Error.Retryable {
			t.Fatalf("cleanup-won confirmation error = %#v", failure)
		}
		var turns int
		var confirmations int
		if err := pool.QueryRow(
			context.Background(),
			`SELECT
			    (SELECT count(*)::int
			     FROM conversation_confirmed_turns
			     WHERE candidate_id = $1),
			    (SELECT count(*)::int
			     FROM conversation_turn_confirmations confirmations
			     JOIN conversation_confirmed_turns turns
			       ON turns.owner_user_id = confirmations.owner_user_id
			      AND turns.turn_id = confirmations.turn_id
			     WHERE turns.candidate_id = $1)`,
			candidateID,
		).Scan(&turns, &confirmations); err != nil {
			t.Fatalf("count cleanup-race confirmations: %v", err)
		}
		if turns != 0 || confirmations != 0 {
			t.Fatalf(
				"cleanup-won confirmation persisted %d Turns and %d keys",
				turns,
				confirmations,
			)
		}
	}
	assertFailedConfirm(server.URL)

	if err := objects.Delete(
		context.Background(),
		claims[0].Asset.ObjectKey,
	); err != nil {
		t.Fatalf("delete claimed recording object: %v", err)
	}
	var deletedAt time.Time
	if err := pool.QueryRow(
		context.Background(),
		`SELECT transaction_timestamp()`,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("read database deletion time: %v", err)
	}
	deleted := claims[0].Asset
	deleted.Status = conversation.AudioAssetDeleted
	deleted.DeletedAt = deletedAt.UTC()
	deleted.UpdatedAt = deleted.DeletedAt
	deleted.Version++
	if err := audioRepository.SaveCleanupClaim(
		context.Background(),
		deleted,
		claims[0].Asset.Version,
		claims[0].FencingToken,
	); err != nil {
		t.Fatalf("finish recording cleanup claim: %v", err)
	}
	purged, err := audioRepository.PurgeOwnerDeletedAssets(
		context.Background(),
		deleted.OwnerID,
		10,
	)
	if err != nil || purged != 1 {
		t.Fatalf("purge deleted recording metadata = %d, %v", purged, err)
	}

	server.Close()
	if err := vault.Close(); err != nil {
		t.Fatalf("close cleanup-race vault: %v", err)
	}
	restartedVault := newVoiceTestVault(t)
	restartedServer := buildServer(restartedVault)
	assertFailedConfirm(restartedServer.URL)
	resumed := voiceJSONRequest(
		t,
		restartedServer.URL,
		token,
		http.MethodGet,
		"/v1/agent-threads/"+threadID+"/voice-practice-session",
		"",
		"",
		http.StatusOK,
	)
	if resumed["effective_turns"] != float64(0) ||
		resumed["session_completed"] != false {
		t.Fatalf("cleanup-won Resume advanced Session: %#v", resumed)
	}
	if turn, present := resumed["current_turn"]; present && turn != nil {
		t.Fatalf("cleanup-won Resume exposed damaged Turn: %#v", resumed)
	}
}

type voiceFormalContext struct {
	PlanID                string
	PreparationSnapshotID string
	SessionID             string
	ThreadID              string
	MatterID              string
}

func newVoiceProductionIntegrationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog preparation.CatalogReader,
	generator ai.TextGenerator,
	configuration VoiceConfiguration,
) *httptest.Server {
	t.Helper()
	composition, err := NewIdentityAgentAndPracticeComposition(
		context.Background(),
		pool,
		nil,
		"",
		generator,
		agent.RunConfiguration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		emptyBootstrapMemorySearcher{},
		catalog,
		configuration,
	)
	if err != nil {
		t.Fatalf("build production Practice/Voice composition: %v", err)
	}
	protectedRoutes, err := composition.ProtectedRoutes()
	if err != nil {
		t.Fatalf("build protected Practice routes: %v", err)
	}
	router := NewRouterWithReadinessAndRoutes(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pool,
		[]RouteRegistrar{
			composition.IdentityModule(),
			composition.AgentModule(),
			protectedRoutes,
		},
	)
	RegisterPreparationCatalog(router, catalog)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func createVoiceFormalThreadContext(
	t *testing.T,
	baseURL string,
	token string,
	title string,
	key string,
) voiceFormalContext {
	t.Helper()
	matterID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/matters",
		fmt.Sprintf(`{"title":%q}`, title),
		"",
		http.StatusCreated,
	)["matter_id"].(string)
	threadID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_matter_id":%q}`, matterID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	return createVoiceFormalContext(
		t,
		baseURL,
		token,
		threadID,
		matterID,
		key,
	)
}

func createVoiceFormalContext(
	t *testing.T,
	baseURL string,
	token string,
	threadID string,
	matterID string,
	key string,
) voiceFormalContext {
	t.Helper()
	profile := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/preparation-profiles",
		fmt.Sprintf(`{
			"resume_ref":"resume-%s",
			"job_description_ref":"job-%s",
			"background_summary":"Voice integration context %s."
		}`, key, key, key),
		"voice-"+key+"-profile",
		http.StatusCreated,
	)
	profileID := profile["preparation_profile_id"].(string)
	snapshot := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/preparation-profiles/"+profileID+"/snapshots",
		`{"source_version":1}`,
		"voice-"+key+"-preparation-snapshot",
		http.StatusCreated,
	)
	context := voiceFormalContext{
		PreparationSnapshotID: snapshot["preparation_snapshot_id"].(string),
		ThreadID:              threadID,
		MatterID:              matterID,
	}
	plan := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		fmt.Sprintf(`{
			"agent_thread_id":%q,
			"matter_id":%q,
			"scenario_definition_id":%q,
			"scenario_definition_version":1,
			"scenario_config_id":%q,
			"scenario_config_version":1,
			"preparation_profile_id":%q,
			"selected_role_ids":[%q]
		}`,
			threadID,
			matterID,
			preparation.ProgrammerInterviewScenarioID,
			preparation.BackendEngineerConfigID,
			profileID,
			preparation.TechnicalInterviewerRoleID,
		),
		"voice-"+key+"-plan",
		http.StatusCreated,
	)
	context.PlanID = plan["practice_plan_id"].(string)
	context.SessionID = createVoiceFormalContextSession(
		t,
		baseURL,
		token,
		context,
		key+"-initial",
	)
	return context
}

func createVoiceFormalContextSession(
	t *testing.T,
	baseURL string,
	token string,
	formalContext voiceFormalContext,
	key string,
) string {
	t.Helper()
	bootstrap := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/practice-plans/"+formalContext.PlanID+"/practice-sessions",
		fmt.Sprintf(`{
			"expected_plan_revision":1,
			"user_confirmed":true,
			"preparation_snapshot_id":%q,
			"practice_option_id":%q,
			"role_definition_ids":[%q]
		}`,
			formalContext.PreparationSnapshotID,
			preparation.TechnicalFocusOptionID,
			preparation.TechnicalInterviewerRoleID,
		),
		"voice-"+key+"-context-session",
		http.StatusCreated,
	)
	session, ok := bootstrap["practice_session"].(map[string]any)
	if !ok {
		t.Fatalf("formal Context bootstrap lost Session: %#v", bootstrap)
	}
	sessionID, _ := session["practice_session_id"].(string)
	if sessionID == "" {
		t.Fatalf("formal Context bootstrap has no Session ID: %#v", bootstrap)
	}
	return sessionID
}

func assertNoLegacyVoiceSession(
	t *testing.T,
	pool *pgxpool.Pool,
	formalSessionID string,
	threadID string,
) {
	t.Helper()
	var formalCount, legacyCount int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT
		    count(*) FILTER (
		        WHERE session_id = $1
		          AND context_plan_id IS NOT NULL
		    )::int,
		    count(*) FILTER (
		        WHERE plan_id = $2
		          AND context_plan_id IS NULL
		    )::int
		 FROM practice_sessions`,
		formalSessionID,
		"agent-thread:"+threadID,
	).Scan(&formalCount, &legacyCount); err != nil {
		t.Fatalf("read formal/legacy voice Sessions: %v", err)
	}
	if formalCount != 1 || legacyCount != 0 {
		t.Fatalf(
			"formal/legacy voice Sessions = %d/%d, want 1/0",
			formalCount,
			legacyCount,
		)
	}
}

func completeBootstrapHistoryReview(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
	sessionID string,
) review.FormalReview {
	t.Helper()
	repository := review.NewPostgresRepository(pool)
	actor := review.Actor{UserID: ownerUserID}
	sourceTurnID := "turn-" + sessionID
	sourceTurnVersion := "conversation-turn:evidence-v1"
	pending, err := repository.EnsurePending(
		context.Background(),
		review.EnsureReviewCommand{
			Actor:                     actor,
			PracticeSessionID:         sessionID,
			ImplementationVersion:     "qianwen-voice-review-v1",
			SourceTurnID:              sourceTurnID,
			SourceTurnVersion:         sourceTurnVersion,
			SourceManifestFingerprint: "manifest-" + sessionID,
		},
	)
	if err != nil {
		t.Fatalf("ensure bootstrap history Review %s: %v", sessionID, err)
	}
	_, claim, claimed, err := repository.ClaimGeneration(
		context.Background(),
		actor,
		pending.ID,
		time.Minute,
	)
	if err != nil || !claimed {
		t.Fatalf(
			"claim bootstrap history Review %s: claimed=%v err=%v",
			sessionID,
			claimed,
			err,
		)
	}
	completed, err := repository.CompleteGeneration(
		context.Background(),
		claim,
		review.ReviewResult{
			OverallScore: 80,
			Summary:      "Persisted restart cursor fixture.",
			Conclusions: []review.ReviewConclusion{{
				Key:        "summary",
				Category:   "clarity",
				Message:    "The response is clear.",
				Suggestion: "Add one concrete outcome.",
			}},
		},
		[]review.ReviewEvidence{{
			ConclusionKey: "summary",
			SourceType:    review.SourceTypeConversationTurn,
			SourceID:      sourceTurnID,
			SourceVersion: sourceTurnVersion,
		}},
	)
	if err != nil {
		t.Fatalf("complete bootstrap history Review %s: %v", sessionID, err)
	}
	return completed
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

func waitForVoicePracticeState(
	t *testing.T,
	baseURL string,
	token string,
	threadID string,
	ready func(map[string]any) bool,
) map[string]any {
	t.Helper()
	var state map[string]any
	waitForVoiceCondition(t, "voice Practice state", func() bool {
		state = voiceJSONRequest(
			t,
			baseURL,
			token,
			http.MethodGet,
			"/v1/agent-threads/"+threadID+"/voice-practice-session",
			"",
			"",
			http.StatusOK,
		)
		return ready(state)
	})
	return state
}

func waitForVoiceCondition(
	t *testing.T,
	description string,
	ready func() bool,
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	if strings.HasPrefix(last, "RUBRIC=") {
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
		content, err := voiceReviewFixture(last)
		if err != nil {
			return ai.TextResult{}, err
		}
		return ai.TextResult{
			ID:       "review-completion",
			Provider: "fake",
			Model:    "fake-text-v1",
			Content:  content,
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

func voiceReviewFixture(prompt string) (string, error) {
	const marker = "\nCONFIRMED_EVIDENCE="
	index := strings.LastIndex(prompt, marker)
	if index < 0 {
		return "", errors.New("review prompt has no confirmed evidence")
	}
	var sources []struct {
		SourceID      string `json:"source_id"`
		SourceVersion string `json:"source_version"`
		Answer        string `json:"answer"`
	}
	if err := json.Unmarshal(
		[]byte(prompt[index+len(marker):]),
		&sources,
	); err != nil || len(sources) == 0 {
		return "", errors.New("review prompt has invalid confirmed evidence")
	}
	dimensions := []string{
		"relevance_structure",
		"technical_depth",
		"ownership_decisions",
		"evidence_impact",
		"language_clarity",
	}
	conclusions := make([]map[string]any, len(dimensions))
	for dimensionIndex, dimension := range dimensions {
		source := sources[dimensionIndex%len(sources)]
		conclusions[dimensionIndex] = map[string]any{
			"key":        "conclusion-" + dimension,
			"category":   dimension,
			"score":      80,
			"message":    "The answer is grounded in the confirmed response.",
			"suggestion": "Make the result more measurable.",
			"evidence": []map[string]any{{
				"source_id":      source.SourceID,
				"source_version": source.SourceVersion,
				"quote":          source.Answer,
				"occurrence":     1,
			}},
		}
	}
	feedbackSource := sources[len(sources)-1]
	payload := map[string]any{
		"summary":     "Clear answers with useful examples.",
		"conclusions": conclusions,
		"feedback_items": []map[string]any{{
			"key":        "feedback-impact",
			"kind":       "improvement",
			"message":    "The impact can be more specific.",
			"suggestion": "Add one measurable outcome.",
			"evidence": []map[string]any{{
				"source_id":      feedbackSource.SourceID,
				"source_version": feedbackSource.SourceVersion,
				"quote":          feedbackSource.Answer,
				"occurrence":     1,
			}},
		}},
		"repractice_suggestion_refs": []string{"feedback-impact"},
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
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

type voiceObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newVoiceObjectStore() *voiceObjectStore {
	return &voiceObjectStore{objects: make(map[string][]byte)}
}

func (store *voiceObjectStore) Put(
	_ context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	audio, err := io.ReadAll(request.Body)
	if err != nil || int64(len(audio)) != request.Size {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	if existing, found := store.objects[request.Key]; found {
		if !bytes.Equal(existing, audio) {
			return objectstore.PutResult{}, objectstore.ErrAlreadyExists
		}
		return objectstore.PutResult{ETag: "voice-etag"}, nil
	}
	store.objects[request.Key] = bytes.Clone(audio)
	return objectstore.PutResult{ETag: "voice-etag"}, nil
}

func (store *voiceObjectStore) SignedGet(
	_ context.Context,
	key string,
) (objectstore.SignedGetResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, found := store.objects[key]; !found {
		return objectstore.SignedGetResult{},
			objectstore.ErrOperationFailed
	}
	return objectstore.SignedGetResult{
		URL: "https://private-audio.example.invalid/" +
			url.PathEscape(key),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}, nil
}

func (store *voiceObjectStore) Delete(
	_ context.Context,
	key string,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.objects, key)
	return nil
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
