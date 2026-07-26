package preparation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const maxJobTargetHTTPRequestBody = 128 * 1024

type JobTargetHTTPApplication interface {
	Create(
		context.Context,
		requestcontext.Actor,
		string,
		CreateJobTargetRequest,
	) (JobTarget, bool, error)
	Get(
		context.Context,
		requestcontext.Actor,
		string,
	) (JobTarget, error)
	Update(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		UpdateJobTargetRequest,
	) (JobTarget, bool, error)
	Analyze(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		AnalyzeJobTargetRequest,
	) (JobTarget, bool, error)
	Confirm(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		ConfirmJobTargetRequest,
	) (JobTarget, bool, error)
	Discard(
		context.Context,
		requestcontext.Actor,
		string,
		string,
		DiscardJobTargetRequest,
	) (JobTarget, bool, error)
}

type JobTargetHTTPHandler struct {
	application JobTargetHTTPApplication
}

func NewJobTargetHTTPHandler(
	application JobTargetHTTPApplication,
) (*JobTargetHTTPHandler, error) {
	if application == nil {
		return nil, errors.New(
			"preparation: job target HTTP application is required",
		)
	}
	return &JobTargetHTTPHandler{application: application}, nil
}

func (h *JobTargetHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/job-targets", h.create)
	routes.GET("/v1/job-targets/:job_target_id", h.get)
	routes.PUT("/v1/job-targets/:job_target_id", h.update)
	routes.POST(
		"/v1/job-targets/:job_target_id/analyses",
		h.analyze,
	)
	routes.POST(
		"/v1/job-targets/:job_target_id/confirmations",
		h.confirm,
	)
	routes.POST(
		"/v1/job-targets/:job_target_id/discard",
		h.discard,
	)
}

func (h *JobTargetHTTPHandler) create(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, ok := jobTargetActorFromContext(c)
	if !ok {
		return
	}
	key, ok := profileIdempotencyKey(c)
	if !ok {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	var request CreateJobTargetRequest
	if !decodeJobTargetJSONObject(c, &request) {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	target, _, err := h.application.Create(
		c.Request.Context(),
		actor,
		key,
		request,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, target)
}

func (h *JobTargetHTTPHandler) get(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, ok := jobTargetActorFromContext(c)
	if !ok {
		return
	}
	targetID, ok := jobTargetPathID(c)
	if !ok {
		writeJobTargetHTTPError(
			c,
			http.StatusNotFound,
			"job_target_not_found",
			false,
		)
		return
	}
	target, err := h.application.Get(
		c.Request.Context(),
		actor,
		targetID,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, target)
}

func (h *JobTargetHTTPHandler) update(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, targetID, key, ok := jobTargetWriteContext(c)
	if !ok {
		return
	}
	var request UpdateJobTargetRequest
	if !decodeJobTargetJSONObject(c, &request) {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	target, _, err := h.application.Update(
		c.Request.Context(),
		actor,
		targetID,
		key,
		request,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, target)
}

func (h *JobTargetHTTPHandler) analyze(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, targetID, key, ok := jobTargetWriteContext(c)
	if !ok {
		return
	}
	var request AnalyzeJobTargetRequest
	if !decodeJobTargetJSONObject(c, &request) {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	target, _, err := h.application.Analyze(
		c.Request.Context(),
		actor,
		targetID,
		key,
		request,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	status := http.StatusOK
	if target.Stage == JobTargetStageParsing {
		status = http.StatusAccepted
	}
	c.JSON(status, target)
}

func (h *JobTargetHTTPHandler) confirm(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, targetID, key, ok := jobTargetWriteContext(c)
	if !ok {
		return
	}
	var request ConfirmJobTargetRequest
	if !decodeJobTargetJSONObject(c, &request) ||
		!validJobTargetCandidateJSONSize(request.Candidate) {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	target, _, err := h.application.Confirm(
		c.Request.Context(),
		actor,
		targetID,
		key,
		request,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, target)
}

func (h *JobTargetHTTPHandler) discard(c *gin.Context) {
	setJobTargetPrivateHeaders(c)
	actor, targetID, key, ok := jobTargetWriteContext(c)
	if !ok {
		return
	}
	var request DiscardJobTargetRequest
	if !decodeJobTargetJSONObject(c, &request) {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return
	}
	target, _, err := h.application.Discard(
		c.Request.Context(),
		actor,
		targetID,
		key,
		request,
	)
	if err != nil {
		writeJobTargetServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, target)
}

func jobTargetActorFromContext(
	c *gin.Context,
) (requestcontext.Actor, bool) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		writeProfileAuthenticationRequired(c)
		return requestcontext.Actor{}, false
	}
	return actor, true
}

func jobTargetWriteContext(
	c *gin.Context,
) (requestcontext.Actor, string, string, bool) {
	actor, ok := jobTargetActorFromContext(c)
	if !ok {
		return requestcontext.Actor{}, "", "", false
	}
	targetID, ok := jobTargetPathID(c)
	if !ok {
		writeJobTargetHTTPError(
			c,
			http.StatusNotFound,
			"job_target_not_found",
			false,
		)
		return requestcontext.Actor{}, "", "", false
	}
	key, ok := profileIdempotencyKey(c)
	if !ok {
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
		return requestcontext.Actor{}, "", "", false
	}
	return actor, targetID, key, true
}

func jobTargetPathID(c *gin.Context) (string, bool) {
	targetID := c.Param("job_target_id")
	return targetID, utf8.ValidString(targetID) &&
		validResourceIdentifier(targetID)
}

func setJobTargetPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func decodeJobTargetJSONObject(c *gin.Context, target any) bool {
	if c.Request.Body == nil ||
		c.Request.ContentLength > maxJobTargetHTTPRequestBody ||
		!validProfileJSONContentType(c.GetHeader("Content-Type")) {
		return false
	}
	body := http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxJobTargetHTTPRequestBody,
	)
	raw, err := io.ReadAll(body)
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	var shape any
	shapeDecoder := json.NewDecoder(bytes.NewReader(raw))
	shapeDecoder.UseNumber()
	if shapeDecoder.Decode(&shape) != nil || jsonContainsNull(shape) {
		return false
	}
	var trailing any
	if !errors.Is(shapeDecoder.Decode(&trailing), io.EOF) {
		return false
	}
	if _, object := shape.(map[string]any); !object {
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func jsonContainsNull(value any) bool {
	switch value := value.(type) {
	case nil:
		return true
	case []any:
		for _, item := range value {
			if jsonContainsNull(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if jsonContainsNull(item) {
				return true
			}
		}
	}
	return false
}

func writeJobTargetServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrJobTargetInvalid):
		writeJobTargetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			false,
		)
	case errors.Is(err, ErrJobTargetNotFound):
		writeJobTargetHTTPError(
			c,
			http.StatusNotFound,
			"job_target_not_found",
			false,
		)
	case errors.Is(err, ErrJobTargetIdempotencyConflict):
		writeJobTargetHTTPError(
			c,
			http.StatusConflict,
			"idempotency_key_conflict",
			false,
		)
	case errors.Is(err, ErrJobTargetConflict):
		writeJobTargetHTTPError(
			c,
			http.StatusConflict,
			"job_target_version_conflict",
			false,
		)
	case errors.Is(err, ErrJobTargetAnalysisClaimLost):
		writeJobTargetHTTPError(
			c,
			http.StatusConflict,
			"job_target_analysis_claim_lost",
			true,
		)
	case errors.Is(err, ErrJobTargetAnalysisFailed):
		writeJobTargetHTTPError(
			c,
			http.StatusServiceUnavailable,
			"job_target_analysis_failed",
			true,
		)
	default:
		writeJobTargetHTTPError(
			c,
			http.StatusInternalServerError,
			"internal_error",
			false,
		)
	}
}

func writeJobTargetHTTPError(
	c *gin.Context,
	status int,
	code string,
	retryable bool,
) {
	messages := map[string]string{
		"invalid_request":                "Request validation failed.",
		"job_target_not_found":           "Job target was not found.",
		"job_target_version_conflict":    "Job target version conflicts with this operation.",
		"idempotency_key_conflict":       "Idempotency key conflicts with the original request.",
		"job_target_analysis_claim_lost": "Job target analysis was superseded.",
		"job_target_analysis_failed":     "Job target analysis failed.",
		"internal_error":                 "An internal error occurred.",
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        messages[code],
			"retryable":      retryable,
			"correlation_id": newCatalogCorrelationID(),
		},
	})
}

var _ JobTargetHTTPApplication = (*JobTargetService)(nil)
