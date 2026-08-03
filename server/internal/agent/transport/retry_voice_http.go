package transport

import (
	"errors"
	"net/http"
	"strings"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/voice"
	"github.com/gin-gonic/gin"
)

func (h *HTTPHandler) transcribeRetryVoiceCandidate(c *gin.Context) {
	setRetryVoicePrivateHeaders(c)
	key, ok := voiceIdempotencyKey(c)
	if !ok || c.Request.Body == nil ||
		c.Request.ContentLength > platformmedia.MaxAudioBytes ||
		!strings.EqualFold(
			strings.TrimSpace(c.GetHeader("Content-Type")),
			platformmedia.ContentTypeWAV,
		) {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(
		time.Now().Add(h.voiceReadTimeout),
	); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.writeError(c, http.StatusInternalServerError, "internal_error", true)
		return
	}
	defer func() {
		_ = controller.SetReadDeadline(time.Time{})
	}()
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		platformmedia.MaxAudioBytes,
	)
	candidate, err := h.sameQuestionRetry.Transcribe(
		c.Request.Context(),
		actor,
		practicevoice.RetryTranscriptionCommand{
			RetryTurnID:    c.Param("retry_turn_id"),
			IdempotencyKey: key,
			ContentType:    platformmedia.ContentTypeWAV,
			Audio:          body,
		},
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	c.JSON(
		http.StatusCreated,
		retryTranscriptionCandidateResponse(candidate),
	)
}

func (h *HTTPHandler) confirmRetryVoiceCandidate(c *gin.Context) {
	setRetryVoicePrivateHeaders(c)
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	key, ok := voiceIdempotencyKey(c)
	if !ok {
		h.writeError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	actor, ok := trustedActor(c)
	if !ok {
		h.writeAuthenticationRequired(c)
		return
	}
	turn, err := h.sameQuestionRetry.Confirm(
		c.Request.Context(),
		actor,
		practicevoice.ConfirmRetryTranscriptionCommand{
			RetryTurnID:    c.Param("retry_turn_id"),
			CandidateID:    c.Param("candidate_id"),
			IdempotencyKey: key,
		},
	)
	if err != nil {
		h.writeVoiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, confirmedRetryTurnResponse(turn))
}

func retryTranscriptionCandidateResponse(
	result practicevoice.RetryTranscriptionCandidate,
) gin.H {
	candidate := result.Candidate
	return gin.H{
		"candidate_id":              candidate.ID,
		"retry_turn_id":             result.RetryTurnID,
		"retry_request_id":          result.RetryRequestID,
		"practice_session_id":       candidate.SessionID,
		"question_id":               candidate.QuestionID,
		"respondent_participant_id": candidate.RespondentParticipantID,
		"candidate_status":          "READY",
		"transcript_id":             candidate.TranscriptID,
		"evidence_version":          candidate.EvidenceVersion,
		"transcript":                candidate.Transcript,
		"created_at": candidate.CreatedAt.UTC().
			Format(time.RFC3339Nano),
	}
}

func confirmedRetryTurnResponse(
	result practicevoice.ConfirmedRetryTurn,
) gin.H {
	turn := result.Turn
	response := gin.H{
		"turn_id":                   turn.ID,
		"retry_request_id":          result.RetryRequestID,
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
		"created_at": result.CreatedAt.UTC().
			Format(time.RFC3339Nano),
		"confirmed_at": result.ConfirmedAt.UTC().
			Format(time.RFC3339Nano),
	}
	if turn.AudioAssetID != "" {
		response["audio_asset_id"] = turn.AudioAssetID
	}
	return response
}

func setRetryVoicePrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}
