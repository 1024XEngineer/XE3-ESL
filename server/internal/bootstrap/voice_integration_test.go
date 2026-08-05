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

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testReviewHistoryCursorKey = []byte(
	"0123456789abcdef0123456789abcdef",
)

func TestVoiceInterviewFollowUpTextAnswerKeepsEffectiveTurn(t *testing.T) {
	pool := voiceIntegrationDatabase(t)
	text := &followUpVoiceTextGenerator{}
	catalog, err := scene.NewPostgresCatalog(pool)
	if err != nil {
		t.Fatalf("build Preparation catalog: %v", err)
	}
	server := newVoiceProductionIntegrationServer(
		t,
		pool,
		catalog,
		text,
		VoiceConfiguration{
			Recognizer: &voiceRecognizer{},
			Synthesizer: newFailingTestSpeechSynthesizer(
				fmt.Errorf("tts unavailable"),
			),
			TemporaryAudio:         newVoiceTestVault(t),
			ObjectStore:            newVoiceObjectStore(),
			AudioStagedTTL:         time.Hour,
			ASRLease:               5 * time.Second,
			ReviewHistoryCursorKey: testReviewHistoryCursorKey,
		},
	)

	token := registerAndLoginVoiceUser(
		t,
		server.URL,
		"voice-follow-up@example.com",
	)
	formalContext := createVoiceInterviewFormalContext(
		t,
		server.URL,
		token,
		"follow-up",
	)
	state := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-sessions/"+formalContext.SessionID+
			"/voice-activation",
		"",
		"start-follow-up-session",
		http.StatusOK,
	)
	primary := state["current_question"].(map[string]any)
	if primary["question_type"] != "PRIMARY" {
		t.Fatalf("initial Question = %#v, want PRIMARY", primary)
	}

	state = submitVoiceTextAnswer(
		t,
		server.URL,
		token,
		state,
		"I led the migration and coordinated the rollout.",
		"follow-up-primary-answer",
	)
	followUp := state["current_question"].(map[string]any)
	if state["effective_turns"] != float64(1) ||
		followUp["question_type"] != "FOLLOW_UP" ||
		followUp["parent_question_id"] != primary["question_id"] {
		t.Fatalf("first follow-up state = %#v", state)
	}

	state = submitVoiceTextAnswer(
		t,
		server.URL,
		token,
		state,
		"The main trade-off was delivery speed versus rollback safety.",
		"follow-up-answer",
	)
	nextPrimary := state["current_question"].(map[string]any)
	currentTurn := state["current_turn"].(map[string]any)
	if state["effective_turns"] != float64(1) ||
		currentTurn["counts_toward_effective_turn_limit"] != false ||
		nextPrimary["question_type"] != "PRIMARY" {
		t.Fatalf("confirmed follow-up state = %#v", state)
	}
	if _, found := nextPrimary["parent_question_id"]; found {
		t.Fatalf("next PRIMARY retained parent: %#v", nextPrimary)
	}

	var storedType string
	var storedCounts bool
	var storedEffectiveTurns int
	if err := pool.QueryRow(
		context.Background(),
		`SELECT question.question_type,
		        turn.counts_toward_effective_turn_limit,
		        turn.effective_turns
		 FROM practice_turns AS turn
		 JOIN practice_questions AS question
		   ON question.owner_user_id = turn.owner_user_id
		  AND question.question_id = turn.question_id
		 WHERE turn.practice_session_id = $1
		   AND question.question_type = 'FOLLOW_UP'`,
		formalContext.SessionID,
	).Scan(&storedType, &storedCounts, &storedEffectiveTurns); err != nil {
		t.Fatalf("read stored follow-up Turn: %v", err)
	}
	if storedType != "FOLLOW_UP" || storedCounts || storedEffectiveTurns != 1 {
		t.Fatalf(
			"stored follow-up = (%q, %v, %d)",
			storedType,
			storedCounts,
			storedEffectiveTurns,
		)
	}
}

func TestVoiceProductionCompositionBearerConcurrencyAndRestart(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	text := &voiceTextGenerator{}
	recognizer := &voiceRecognizer{}
	synthesizer := newFailingTestSpeechSynthesizer(
		fmt.Errorf("tts unavailable"),
	)
	objects := newVoiceObjectStore()
	vault := newVoiceTestVault(t)
	catalog, err := scene.NewPostgresCatalog(pool)
	if err != nil {
		t.Fatalf("build Preparation catalog: %v", err)
	}
	server := newVoiceProductionIntegrationServer(
		t,
		pool,
		catalog,
		text,
		VoiceConfiguration{
			Recognizer:             recognizer,
			Synthesizer:            synthesizer,
			TemporaryAudio:         vault,
			ObjectStore:            objects,
			AudioStagedTTL:         time.Hour,
			ASRLease:               5 * time.Second,
			ReviewHistoryCursorKey: testReviewHistoryCursorKey,
		},
	)

	token := registerAndLoginVoiceUser(t, server.URL, "voice-a@example.com")
	goalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Customer renewal"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	threadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, goalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	formalContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		threadID,
		goalID,
		"primary",
	)

	startPath := "/v1/practice-sessions/" + formalContext.SessionID +
		"/voice-activation"
	archivedGoalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Archived scenario"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	archivedThreadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, archivedGoalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	archivedContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		archivedThreadID,
		archivedGoalID,
		"archived",
	)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPatch,
		"/v1/goals/"+archivedGoalID,
		`{"status":"archived","expected_version":1}`,
		"",
		http.StatusOK,
	)
	archivedStart, err := voiceRawRequest(
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-sessions/"+archivedContext.SessionID+
			"/voice-activation",
		nil,
		"start-archived-session",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = archivedStart.Body.Close()
	if archivedStart.StatusCode != http.StatusOK {
		t.Fatalf("frozen Session Start status = %d", archivedStart.StatusCode)
	}
	replayGoalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Replay after archive"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	replayThreadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, replayGoalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	replayContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		replayThreadID,
		replayGoalID,
		"archive-replay",
	)
	replayPath := "/v1/practice-sessions/" + replayContext.SessionID +
		"/voice-activation"
	firstReplay := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		replayPath,
		"",
		"start-replay-after-archive",
		http.StatusOK,
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
		"/v1/goals/"+replayGoalID,
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
		http.StatusOK,
	)
	if replayedAfterArchive["practice_session_id"] != replayContext.SessionID {
		t.Fatalf(
			"archived Goal replay lost original Session: %#v",
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
		http.StatusOK,
	)
	sessionID := state["practice_session_id"].(string)
	if sessionID != formalContext.SessionID ||
		state["practice_plan_id"] != formalContext.PlanID {
		t.Fatalf("voice Session lost formal Context binding: %#v", state)
	}
	if _, found := state["goal"]; found {
		t.Fatalf("voice state exposed Goal: %#v", state)
	}
	if _, found := state["thread_id"]; found {
		t.Fatalf("voice state exposed Thread: %#v", state)
	}
	replayedStart := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusOK,
	)
	if replayedStart["practice_session_id"] != sessionID {
		t.Fatal("same Start idempotency key created a different Session")
	}
	nextGoalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Leadership transition"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPut,
		"/v1/agent-threads/"+threadID+"/active-goal",
		fmt.Sprintf(`{"goal_id":%q}`, nextGoalID),
		"",
		http.StatusOK,
	)
	replayedAfterGoalSwitch := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusOK,
	)
	if replayedAfterGoalSwitch["practice_session_id"] != sessionID {
		t.Fatalf(
			"Start replay after active Goal switch = %#v",
			replayedAfterGoalSwitch,
		)
	}
	resumedAfterGoalSwitch := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/practice-sessions/"+sessionID+"/voice-state",
		"",
		"",
		http.StatusOK,
	)
	if resumedAfterGoalSwitch["practice_session_id"] != sessionID {
		t.Fatalf(
			"GET recovery reinterpreted active Goal: %#v",
			resumedAfterGoalSwitch,
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
	if conflictingStart.StatusCode != http.StatusOK {
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
		"/v1/agent-threads/"+threadID+"/active-goal",
		fmt.Sprintf(`{"goal_id":%q}`, goalID),
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
		"/v1/practice-sessions/"+sessionID+"/voice-state",
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
		"/v1/practice-sessions/"+sessionID+"/voice-state",
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
	completedState := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodGet,
		"/v1/practice-sessions/"+sessionID+"/voice-state",
		"",
		"",
		http.StatusOK,
	)
	if completedState["effective_turns"] != float64(3) ||
		completedState["session_completed"] != true {
		t.Fatalf("completed Practice state = %#v", completedState)
	}
	if _, present := completedState["review"]; present {
		t.Fatalf("Practice state leaked Review: %#v", completedState)
	}
	completedTurn := completedState["current_turn"].(map[string]any)
	if completedTurn["turn_id"] != thirdTurnID {
		t.Fatalf("completed Practice lost final Turn: %#v", completedState)
	}
	if _, present := completedTurn["review_id"]; present {
		t.Fatalf("Practice Turn leaked Review identity: %#v", completedTurn)
	}
	assertSinglePracticeCompletion(t, pool, sessionID, thirdTurnID)
	if text.ReviewCalls() != 0 {
		t.Fatalf("Practice confirmation invoked Review %d times", text.ReviewCalls())
	}

	replayedAfterCompletion := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		startPath,
		"",
		"start-voice-session-0001",
		http.StatusOK,
	)
	if replayedAfterCompletion["practice_session_id"] != sessionID ||
		replayedAfterCompletion["session_completed"] != true {
		t.Fatalf("completed Start replay did not return original Session: %#v", replayedAfterCompletion)
	}
	assertSinglePracticeCompletion(t, pool, sessionID, thirdTurnID)

	playback := voiceJSONRequest(t, server.URL, token, http.MethodGet, "/v1/audio-assets/"+completedAudioAssetID+"/playback", "", "", http.StatusOK)
	if !strings.HasPrefix(playback["playback_url"].(string), "https://private-audio.example.invalid/") {
		t.Fatalf("unexpected protected playback capability: %#v", playback)
	}
	otherToken := registerAndLoginVoiceUser(t, server.URL, "voice-b@example.com")
	for _, path := range []string{"/v1/practice-sessions/" + sessionID + "/voice-state", "/v1/audio-assets/" + completedAudioAssetID + "/playback"} {
		response, requestErr := voiceRawRequest(server.URL, otherToken, http.MethodGet, path, nil, "", "")
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
	restartedServer := newVoiceProductionIntegrationServer(t, pool, catalog, text, VoiceConfiguration{
		Recognizer: recognizer, Synthesizer: synthesizer, TemporaryAudio: restartedVault,
		ObjectStore: objects, AudioStagedTTL: time.Hour, ASRLease: 5 * time.Second,
		ReviewHistoryCursorKey: testReviewHistoryCursorKey,
	})
	recovered := voiceJSONRequest(t, restartedServer.URL, token, http.MethodGet, "/v1/practice-sessions/"+sessionID+"/voice-state", "", "", http.StatusOK)
	if recovered["practice_session_id"] != sessionID || recovered["effective_turns"] != float64(3) || recovered["session_completed"] != true {
		t.Fatalf("restart recovery failed: %#v", recovered)
	}
	if _, present := recovered["review"]; present {
		t.Fatalf("restarted Practice state leaked Review: %#v", recovered)
	}
	recoveredTurn := recovered["current_turn"].(map[string]any)
	if recoveredTurn["turn_id"] != thirdTurnID || recoveredTurn["audio_asset_id"] != completedAudioAssetID {
		t.Fatalf("restart lost final Turn or AudioAsset: %#v", recovered)
	}
	assertSinglePracticeCompletion(t, pool, sessionID, thirdTurnID)

	voiceJSONRequest(t, restartedServer.URL, token, http.MethodDelete, "/v1/audio-assets/"+completedAudioAssetID, "", "", http.StatusNoContent)
	replayedAfterAudioDelete := voiceJSONRequest(t, restartedServer.URL, token, http.MethodPost, confirmPath, "", "confirm-round-3-shared", http.StatusOK)
	replayedDeletedTurn := replayedAfterAudioDelete["current_turn"].(map[string]any)
	if replayedDeletedTurn["turn_id"] != thirdTurnID {
		t.Fatalf("deleted recording replay changed durable Turn: %#v", replayedAfterAudioDelete)
	}
	if _, present := replayedDeletedTurn["audio_asset_id"]; present {
		t.Fatalf("deleted recording replay exposed AudioAsset: %#v", replayedAfterAudioDelete)
	}
	if _, present := replayedDeletedTurn["review_id"]; present {
		t.Fatalf("deleted recording replay leaked Review: %#v", replayedDeletedTurn)
	}
	assertSinglePracticeCompletion(t, pool, sessionID, thirdTurnID)
}

func assertSinglePracticeCompletion(t *testing.T, pool *pgxpool.Pool, sessionID string, finalTurnID string) {
	t.Helper()
	var count int
	var storedFinalTurnID string
	var completionToken string
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)::int, COALESCE(max(final_turn_id), ''), COALESCE(max(completion_token), '')
		FROM practice_completed WHERE session_id = $1
	`, sessionID).Scan(&count, &storedFinalTurnID, &completionToken); err != nil {
		t.Fatalf("read Practice completion: %v", err)
	}
	if count != 1 || storedFinalTurnID != finalTurnID || completionToken == "" {
		t.Fatalf("Practice completion = (%d, %q, %q), want one for %q", count, storedFinalTurnID, completionToken, finalTurnID)
	}
}

func TestVoiceRecordingCleanupWinNeverLeavesRecoverableTurn(
	t *testing.T,
) {
	pool := voiceIntegrationDatabase(t)
	text := &voiceTextGenerator{}
	recognizer := &voiceRecognizer{}
	synthesizer := newFailingTestSpeechSynthesizer(
		fmt.Errorf("tts unavailable"),
	)
	objects := newVoiceObjectStore()
	catalog, err := scene.NewPostgresCatalog(pool)
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
				Recognizer:             recognizer,
				Synthesizer:            synthesizer,
				TemporaryAudio:         vault,
				ObjectStore:            objects,
				AudioStagedTTL:         time.Hour,
				ASRLease:               5 * time.Second,
				ReviewHistoryCursorKey: testReviewHistoryCursorKey,
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
	goalID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/goals",
		`{"title":"Cleanup race"}`,
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	threadID := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, goalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	formalContext := createVoiceFormalContext(
		t,
		server.URL,
		token,
		threadID,
		goalID,
		"cleanup-race",
	)
	state := voiceJSONRequest(
		t,
		server.URL,
		token,
		http.MethodPost,
		"/v1/practice-sessions/"+formalContext.SessionID+
			"/voice-activation",
		"",
		"start-cleanup-race",
		http.StatusOK,
	)
	if state["practice_session_id"] != formalContext.SessionID ||
		state["practice_plan_id"] != formalContext.PlanID {
		t.Fatalf(
			"cleanup-race Start lost formal Context binding: %#v",
			state,
		)
	}
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
		 FROM practice_audio_assets AS assets
		 JOIN practice_transcript_candidates AS candidates
		   ON candidates.owner_user_id = assets.owner_user_id
		  AND candidates.reservation_id = assets.upload_request_id
		 WHERE candidates.candidate_id = $1`,
		candidateID,
	).Scan(&audioAssetID); err != nil {
		t.Fatalf("find staged recording: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`UPDATE practice_audio_assets
		 SET created_at = transaction_timestamp() - interval '2 hours',
		     updated_at = transaction_timestamp() - interval '2 hours',
		     staged_until = transaction_timestamp() - interval '1 hour'
		 WHERE audio_asset_id = $1`,
		audioAssetID,
	); err != nil {
		t.Fatalf("expire staged recording: %v", err)
	}
	audioRepository, err := practicevoicepostgres.NewAudioAssetRepository(pool)
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
			     FROM practice_turns
			     WHERE candidate_id = $1),
			    (SELECT count(*)::int
			     FROM practice_turn_confirmations confirmations
			     JOIN practice_turns turns
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
	deleted.Status = practicevoice.AudioAssetDeleted
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
		"/v1/practice-sessions/"+formalContext.SessionID+
			"/voice-state",
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
	GoalID                string
}

func newVoiceProductionIntegrationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog scene.CatalogReader,
	generator agentrun.TextGenerator,
	configuration VoiceConfiguration,
) *httptest.Server {
	t.Helper()
	if configuration.PracticeRecognizer == nil {
		configuration.PracticeRecognizer = practiceVoiceRecognizerAdapter{
			recognizer: configuration.Recognizer,
		}
	}
	if configuration.PracticeSynthesizer == nil {
		configuration.PracticeSynthesizer = practiceVoiceSynthesizerAdapter{
			synthesizer: configuration.Synthesizer,
		}
	}
	if configuration.QuestionGenerator == nil {
		configuration.QuestionGenerator = practiceVoiceQuestionGeneratorAdapter{
			generator: generator,
		}
	}
	composition, err := NewIdentityAgentAndPracticeComposition(
		context.Background(),
		pool,
		nil,
		"",
		testAgentModelProviders(generator),
		agentrun.Configuration{
			Provider:           "fake",
			Model:              "fake-text-v1",
			MaxOutputTokens:    256,
			MaxInputCharacters: 12000,
		},
		emptyBootstrapMemorySearcher{},
		catalog,
		testJobTargetGenerator(generator),
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
	RegisterSceneCatalog(router, catalog)
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
	goalID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/goals",
		fmt.Sprintf(`{"title":%q}`, title),
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	threadID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, goalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	return createVoiceFormalContext(
		t,
		baseURL,
		token,
		threadID,
		goalID,
		key,
	)
}

func createVoiceInterviewFormalContext(
	t *testing.T,
	baseURL string,
	token string,
	key string,
) voiceFormalContext {
	t.Helper()
	goalID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/goals",
		fmt.Sprintf(`{"title":"Interview %s"}`, key),
		"",
		http.StatusCreated,
	)["goal_id"].(string)
	threadID := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/agent-threads",
		fmt.Sprintf(`{"active_goal_id":%q}`, goalID),
		"",
		http.StatusCreated,
	)["thread_id"].(string)
	profile := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/preparation-profiles",
		fmt.Sprintf(`{
			"job_description_ref":"job-%s",
			"background_summary":"Interview follow-up context %s."
		}`, key, key),
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
	formalContext := voiceFormalContext{
		PreparationSnapshotID: snapshot["preparation_snapshot_id"].(string),
		ThreadID:              threadID,
		GoalID:                goalID,
	}
	plan := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		fmt.Sprintf(`{
			"source_thread_id":%q,
			"goal_id":%q,
			"preparation_snapshot_id":%q,
			"scene_id":%q,
			"scene_version":1,
			"selected_role_ids":[%q],
			"practice_option_id":%q
		}`,
			threadID,
			goalID,
			formalContext.PreparationSnapshotID,
			testProgrammerInterviewSceneID,
			testTechnicalInterviewerRoleID,
			testTechnicalFocusOptionID,
		),
		"voice-"+key+"-plan",
		http.StatusCreated,
	)
	formalContext.PlanID = plan["practice_plan_id"].(string)
	bootstrap := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/practice-plans/"+formalContext.PlanID+"/practice-sessions",
		`{
			"expected_plan_revision":1,
			"user_confirmed":true
		}`,
		"voice-"+key+"-context-session",
		http.StatusCreated,
	)
	session := bootstrap["practice_session"].(map[string]any)
	formalContext.SessionID = session["practice_session_id"].(string)
	return formalContext
}

func createVoiceFormalContext(
	t *testing.T,
	baseURL string,
	token string,
	threadID string,
	goalID string,
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
			"job_description_ref":"job-%s",
			"background_summary":"Voice integration context %s."
		}`, key, key),
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
		GoalID:                goalID,
	}
	plan := voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/practice-plans",
		fmt.Sprintf(`{
			"source_thread_id":%q,
			"goal_id":%q,
			"preparation_snapshot_id":%q,
			"scene_id":%q,
			"scene_version":1,
			"selected_role_ids":[%q],
			"practice_option_id":%q
		}`,
			threadID,
			goalID,
			context.PreparationSnapshotID,
			testWorkplaceProgressSceneID,
			testDirectManagerRoleID,
			testDirectManagerFocusOptionID,
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
		`{
			"expected_plan_revision":1,
			"user_confirmed":true
		}`,
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

func submitVoiceTextAnswer(
	t *testing.T,
	baseURL string,
	token string,
	state map[string]any,
	answer string,
	key string,
) map[string]any {
	t.Helper()
	sessionID := state["practice_session_id"].(string)
	question := state["current_question"].(map[string]any)
	return voiceJSONRequest(
		t,
		baseURL,
		token,
		http.MethodPost,
		"/v1/voice-practice-sessions/"+sessionID+
			"/questions/"+question["question_id"].(string)+
			"/text-answers",
		fmt.Sprintf(`{"answer_text":%q}`, answer),
		key,
		http.StatusOK,
	)
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
	sessionID string,
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
			"/v1/practice-sessions/"+sessionID+"/voice-state",
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

type practiceVoiceRecognizerAdapter struct {
	recognizer agentvoice.SpeechRecognizer
}

func (adapter practiceVoiceRecognizerAdapter) Transcribe(
	ctx context.Context,
	request practicevoice.TranscriptionRequest,
) (practicevoice.TranscriptionResult, error) {
	result, err := adapter.recognizer.Transcribe(
		ctx,
		agentvoice.TranscriptionRequest{Audio: request.Audio},
	)
	return practicevoice.TranscriptionResult{
		ID:         result.ID,
		Provider:   result.Provider,
		Model:      result.Model,
		Transcript: result.Transcript,
	}, err
}

type practiceVoiceSynthesizerAdapter struct {
	synthesizer agentvoice.SpeechSynthesizer
}

func (adapter practiceVoiceSynthesizerAdapter) Synthesize(
	ctx context.Context,
	request practicevoice.SynthesisRequest,
) (practicevoice.SynthesisResult, error) {
	result, err := adapter.synthesizer.Synthesize(
		ctx,
		agentvoice.SynthesisRequest{Text: request.Text},
	)
	return practicevoice.SynthesisResult{
		RequestID: result.RequestID,
		Provider:  result.Provider,
		Model:     result.Model,
		AudioID:   result.AudioID,
		Audio:     result.Audio,
	}, err
}

type practiceVoiceQuestionGeneratorAdapter struct {
	generator agentrun.TextGenerator
}

func (adapter practiceVoiceQuestionGeneratorAdapter) GenerateQuestion(
	ctx context.Context,
	request practicevoice.QuestionGenerationRequest,
) (string, error) {
	result, err := adapter.generator.Generate(ctx, agentrun.TextRequest{
		Messages: []agentrun.TextMessage{
			{Role: agentrun.TextRoleSystem, Content: request.SystemPrompt},
			{Role: agentrun.TextRoleUser, Content: request.UserPrompt},
		},
	})
	return result.Content, err
}

type voiceTextGenerator struct {
	questionCalls         atomic.Int64
	reviewCalls           atomic.Int64
	reviewFailuresPending atomic.Int64
	quotaReviewPending    atomic.Bool
}

type followUpVoiceTextGenerator struct {
	questionCalls atomic.Int64
}

func (generator *followUpVoiceTextGenerator) Generate(
	_ context.Context,
	_ agentrun.TextRequest,
) (agentrun.TextResult, error) {
	call := generator.questionCalls.Add(1)
	content := "Tell me about a project you led."
	if call == 2 {
		content = `{"question_type":"FOLLOW_UP","content":"What trade-off did you make in that project?"}`
	} else if call > 2 {
		content = `{"question_type":"PRIMARY","content":"Tell me about another difficult decision."}`
	}
	return agentrun.TextResult{
		ID:       fmt.Sprintf("follow-up-question-completion-%d", call),
		Provider: "fake",
		Model:    "fake-text-v1",
		Content:  content,
	}, nil
}

func (generator *voiceTextGenerator) Generate(
	_ context.Context,
	request agentrun.TextRequest,
) (agentrun.TextResult, error) {
	last := request.Messages[len(request.Messages)-1].Content
	if strings.HasPrefix(last, "RUBRIC=") {
		generator.reviewCalls.Add(1)
		if generator.quotaReviewPending.CompareAndSwap(true, false) {
			return agentrun.TextResult{}, agentrun.NewGenerationError(
				agentrun.ErrorQuotaExhausted,
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
				return agentrun.TextResult{}, fmt.Errorf(
					"review provider unavailable",
				)
			}
		}
		content, err := voiceReviewFixture(last)
		if err != nil {
			return agentrun.TextResult{}, err
		}
		return agentrun.TextResult{
			ID:       "review-completion",
			Provider: "fake",
			Model:    "fake-text-v1",
			Content:  content,
		}, nil
	}
	call := generator.questionCalls.Add(1)
	return agentrun.TextResult{
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
	if strings.Contains(prompt, "workplace.progress_risk_update") {
		dimensions = []string{
			"progress_clarity",
			"risk_specificity",
			"impact_priority",
			"next_step_ask",
			"language_clarity",
		}
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
	request agentvoice.TranscriptionRequest,
) (agentvoice.TranscriptionResult, error) {
	if err := agentvoice.ValidateTranscriptionRequest(request); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	call := recognizer.calls.Add(1)
	return agentvoice.TranscriptionResult{
		ID:         fmt.Sprintf("asr-result-%d", call),
		Provider:   "fake",
		Model:      "fake-asr-v1",
		Transcript: fmt.Sprintf("Confirmed answer number %d.", call),
	}, nil
}

func (recognizer *voiceRecognizer) TranscribeStream(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	result, err := recognizer.Transcribe(ctx, request)
	if err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{
			Transcript: result.Transcript,
			Final:      true,
		},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	return result, nil
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
