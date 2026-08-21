package voicehttp

import (
	"net/http"
	"time"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpinput"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

func (handler *Handler) transcribeRetryCandidate(c *gin.Context) {
	setRetryPrivateHeaders(c)
	key, body, actor, cleanup, ok := handler.prepareAudio(c)
	if !ok {
		return
	}
	defer cleanup()
	candidate, err := handler.retry.Transcribe(
		c.Request.Context(),
		actor,
		practiceinteraction.RetryTranscriptionCommand{
			RetryTurnID:    c.Param("retry_turn_id"),
			IdempotencyKey: key,
			ContentType:    "audio/wav",
			Audio:          body,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusCreated, retryCandidateResponse(candidate))
}

func (handler *Handler) confirmRetryCandidate(c *gin.Context) {
	setRetryPrivateHeaders(c)
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		handler.write(c, invalidRequest(nil))
		return
	}
	key, ok := httpinput.IdempotencyKey(c.Request)
	if !ok {
		handler.write(c, invalidRequest(nil))
		return
	}
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	turn, err := handler.retry.Confirm(
		c.Request.Context(),
		actor,
		practiceinteraction.ConfirmRetryTranscriptionCommand{
			RetryTurnID:    c.Param("retry_turn_id"),
			CandidateID:    c.Param("candidate_id"),
			IdempotencyKey: key,
		},
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.JSON(http.StatusOK, confirmedRetryTurnResponse(turn))
}

func retryCandidateResponse(
	result practiceinteraction.RetryTranscriptionCandidate,
) gin.H {
	candidate := result.Candidate
	return gin.H{
		"candidate_id":              candidate.ID,
		"retry_turn_id":             result.RetryTurnID,
		"practice_session_id":       candidate.SessionID,
		"question_id":               candidate.QuestionID,
		"respondent_participant_id": candidate.RespondentParticipantID,
		"candidate_status":          "READY",
		"transcript_id":             candidate.TranscriptID,
		"evidence_version":          candidate.EvidenceVersion,
		"transcript":                candidate.Transcript,
		"created_at":                candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func confirmedRetryTurnResponse(
	result practiceinteraction.ConfirmedRetryTurn,
) gin.H {
	turn := result.Turn
	response := gin.H{
		"turn_id":                   turn.ID,
		"original_turn_id":          result.OriginalTurnID,
		"practice_session_id":       turn.SessionID,
		"question_id":               turn.QuestionID,
		"respondent_participant_id": turn.RespondentParticipantID,
		"candidate_id":              turn.CandidateID,
		"interaction_mode":          "PUSH_TO_TALK",
		"answer_text":               turn.AnswerText,
		"evidence_version":          turn.EvidenceVersion,
		"turn_kind":                 "RETRY",
		"turn_status":               "CONFIRMED",
		"counts_toward_turn_limit":  false,
		"created_at":                result.CreatedAt.UTC().Format(time.RFC3339Nano),
		"confirmed_at":              result.ConfirmedAt.UTC().Format(time.RFC3339Nano),
	}
	if turn.AudioAssetID != "" {
		response["audio_asset_id"] = turn.AudioAssetID
	}
	return response
}

func setRetryPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}
