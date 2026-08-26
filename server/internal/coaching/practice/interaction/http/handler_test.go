package voicehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestDeferredTranscriptionHTTPStagesAndReadsStatus(t *testing.T) {
	application := &deferredHTTPApplication{
		practiceRealtimeHTTPApplication: practiceRealtimeHTTPApplication{},
		submission: practiceinteraction.DeferredTranscriptionSubmission{
			ID: "transcription-1", SessionID: "session-1",
			QuestionID: "question-1", Status: practiceinteraction.TranscriptionProcessing,
		},
	}
	router := newPracticeRealtimeHTTPRouter(t, application)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/practice-sessions/session-1/questions/question-1/deferred-transcriptions",
		bytes.NewReader([]byte("RIFF durable wav")),
	)
	request.Header.Set("Authorization", "Bearer voice-token-a")
	request.Header.Set("Content-Type", "audio/wav")
	request.Header.Set("Idempotency-Key", "part-2-recording-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("stage status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if application.stageCommand.SessionID != "session-1" ||
		application.stageCommand.QuestionID != "question-1" ||
		application.stageCommand.IdempotencyKey != "part-2-recording-1" ||
		string(application.stageAudio) != "RIFF durable wav" {
		t.Fatalf("stage command = %#v, audio = %q", application.stageCommand, application.stageAudio)
	}
	assertDeferredHTTPResponse(t, recorder, http.StatusAccepted)

	request = httptest.NewRequest(
		http.MethodGet,
		"/v1/practice-sessions/session-1/deferred-transcriptions/transcription-1",
		nil,
	)
	request.Header.Set("Authorization", "Bearer voice-token-a")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || application.statusID != "transcription-1" {
		t.Fatalf("status response = %d, id = %q, body = %s", recorder.Code, application.statusID, recorder.Body.String())
	}
	assertDeferredHTTPResponse(t, recorder, http.StatusOK)
}

func TestDeferredTranscriptionHTTPRejectsInvalidRequests(t *testing.T) {
	application := &deferredHTTPApplication{
		practiceRealtimeHTTPApplication: practiceRealtimeHTTPApplication{},
		submission: practiceinteraction.DeferredTranscriptionSubmission{
			ID: "transcription-1", SessionID: "another-session",
			QuestionID: "question-1", Status: practiceinteraction.TranscriptionProcessing,
		},
	}
	router := newPracticeRealtimeHTTPRouter(t, application)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/practice-sessions/session-1/questions/question-1/deferred-transcriptions",
		bytes.NewReader([]byte("not accepted without wav headers")),
	)
	request.Header.Set("Authorization", "Bearer voice-token-a")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid stage status = %d", recorder.Code)
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/v1/practice-sessions/session-1/deferred-transcriptions/transcription-1",
		nil,
	)
	request.Header.Set("Authorization", "Bearer voice-token-a")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("mismatched status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/v1/practice-sessions/session-1/deferred-transcriptions/transcription-1",
		nil,
	)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", recorder.Code)
	}
}

func assertDeferredHTTPResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantStatus int,
) {
	t.Helper()
	if recorder.Code != wantStatus || recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response status = %d, headers = %#v", recorder.Code, recorder.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["transcription_id"] != "transcription-1" ||
		body["practice_session_id"] != "session-1" ||
		body["question_id"] != "question-1" ||
		body["status"] != string(practiceinteraction.TranscriptionProcessing) ||
		body["status_url"] != "/v1/practice-sessions/session-1/deferred-transcriptions/transcription-1" {
		t.Fatalf("response body = %#v", body)
	}
}

type deferredHTTPApplication struct {
	practiceRealtimeHTTPApplication
	submission   practiceinteraction.DeferredTranscriptionSubmission
	stageCommand practiceinteraction.TranscribeVoiceCommand
	stageAudio   []byte
	statusID     string
}

func (application *deferredHTTPApplication) StageDeferredTranscription(
	_ context.Context,
	actor requestcontext.Actor,
	command practiceinteraction.TranscribeVoiceCommand,
) (practiceinteraction.DeferredTranscriptionSubmission, error) {
	if actor.UserID != "user-a" {
		return practiceinteraction.DeferredTranscriptionSubmission{}, practiceinteraction.ErrNotFound
	}
	audio, err := io.ReadAll(command.Audio)
	if err != nil {
		return practiceinteraction.DeferredTranscriptionSubmission{}, err
	}
	application.stageCommand = command
	application.stageAudio = audio
	return application.submission, nil
}

func (application *deferredHTTPApplication) DeferredTranscriptionStatus(
	_ context.Context,
	actor requestcontext.Actor,
	reservationID string,
) (practiceinteraction.DeferredTranscriptionSubmission, error) {
	if actor.UserID != "user-a" {
		return practiceinteraction.DeferredTranscriptionSubmission{}, practiceinteraction.ErrNotFound
	}
	application.statusID = reservationID
	return application.submission, nil
}

func TestSessionStateResponseContainsOnlyPracticeRuntimeState(t *testing.T) {
	question := practice.Question{
		ID:                      "question-1",
		SessionID:               "session-1",
		Type:                    "PRIMARY",
		Content:                 "Tell me about yourself.",
		SpeakerParticipantID:    "facilitator-1",
		AddresseeParticipantIDs: []string{"learner-1"},
	}
	turn := practice.Turn{
		ID:                    "turn-1",
		SessionID:             "session-1",
		QuestionID:            question.ID,
		AnswerText:            "I build reliable systems.",
		EffectiveTurns:        1,
		CountsTowardTurnLimit: true,
	}
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{
			ID:             "session-1",
			PlanID:         "plan-1",
			SceneID:        "scene-1",
			SceneVersion:   1,
			SessionVersion: 2,
			EffectiveTurns: 1,
			TurnLimit:      3,
		},
		Question: &question,
		Turn:     &turn,
	})
	if response["practice_session_id"] != "session-1" ||
		response["current_question"] == nil ||
		response["current_turn"] == nil {
		t.Fatalf("response = %#v", response)
	}
	if _, leaked := response["review"]; leaked {
		t.Fatalf("Practice response contains Review: %#v", response)
	}
}

func TestConfirmedTurnResponseHasNoReviewCheckpoint(t *testing.T) {
	response := ConfirmedTurnResponse(practice.Turn{
		ID:             "turn-1",
		SessionID:      "session-1",
		QuestionID:     "question-1",
		AnswerText:     "answer",
		EffectiveTurns: 1,
	})
	if _, leaked := response["review_id"]; leaked {
		t.Fatalf("Turn response contains Review checkpoint: %#v", response)
	}
}

func TestSessionStateResponseIncludesFrozenIELTSAssignment(t *testing.T) {
	assignment := &practice.IELTSAssignment{
		BankID: "ielts-bank-1",
		Season: "2026-05",
		Mode:   practice.PracticeModePart3,
		Parts: []practice.IELTSPart{{
			Part:           practice.PracticeModePart3,
			SourceID:       "topic-group-1",
			TopicTitle:     "Technology",
			TurnBlueprints: []string{"Part 3 question: Why does it matter?"},
		}},
	}
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{IELTSAssignment: assignment},
	})
	if response["ielts_assignment"] != assignment {
		t.Fatalf("response = %#v", response)
	}
}

func TestSessionStateResponseMarksEndedEarlySessionTerminal(t *testing.T) {
	response := SessionStateResponse(practiceinteraction.SessionState{
		Session: practiceinteraction.Session{
			ID:             "session-1",
			PlanID:         "plan-1",
			SceneID:        "scene-1",
			SceneVersion:   1,
			SessionVersion: 2,
			TurnLimit:      3,
			Status:         string(practice.SessionEndedEarly),
		},
	})

	if response["session_completed"] != true {
		t.Fatalf("session_completed = %#v, want true", response["session_completed"])
	}
}
