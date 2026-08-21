package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

type InterviewPreparationHTTPApplication interface {
	Create(context.Context, requestcontext.Actor, string, CreateInterviewPreparationRequest) (InterviewPreparation, bool, error)
	Get(context.Context, requestcontext.Actor, string) (InterviewPreparation, error)
	Patch(context.Context, requestcontext.Actor, string, string, PatchInterviewPreparationRequest) (InterviewPreparation, bool, error)
}

type InterviewPreparationHTTPHandler struct {
	application InterviewPreparationHTTPApplication
}

func NewInterviewPreparationHTTPHandler(application InterviewPreparationHTTPApplication) (*InterviewPreparationHTTPHandler, error) {
	if application == nil {
		return nil, errors.New("preparation: interview preparation HTTP application is required")
	}
	return &InterviewPreparationHTTPHandler{application: application}, nil
}

func (h *InterviewPreparationHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/interview-preparations", h.create)
	routes.GET("/v1/interview-preparations/:interview_preparation_id", h.get)
	routes.PATCH("/v1/interview-preparations/:interview_preparation_id", h.patch)
}

func (h *InterviewPreparationHTTPHandler) create(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	requestID, ok := clientRequestID(c)
	if !ok {
		writeHTTPError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	var request CreateInterviewPreparationRequest
	if !decodeInterviewCreate(c, &request) {
		writeHTTPError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	value, _, err := h.application.Create(c.Request.Context(), actor, requestID, request)
	if err != nil {
		writeInterviewError(c, err)
		return
	}
	c.JSON(http.StatusCreated, value)
}

func decodeInterviewCreate(c *gin.Context, target *CreateInterviewPreparationRequest) bool {
	if c == nil || target == nil || c.Request.Body == nil {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return false
	}
	const maximumPDFBytes = 10 * 1024 * 1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumPDFBytes+maxRequestBody)
	if err := c.Request.ParseMultipartForm(maximumPDFBytes + maxRequestBody); err != nil {
		return false
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	if len(c.Request.MultipartForm.Value) != 1 ||
		len(c.Request.MultipartForm.Value["input"]) != 1 ||
		len(c.Request.MultipartForm.File) > 1 {
		return false
	}
	for key := range c.Request.MultipartForm.File {
		if key != "resume" {
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewBufferString(c.Request.MultipartForm.Value["input"][0]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&target.Input) != nil {
		return false
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return false
	}
	files := c.Request.MultipartForm.File["resume"]
	if len(files) == 0 {
		return true
	}
	if len(files) != 1 || files[0].Size < 5 || files[0].Size > maximumPDFBytes {
		return false
	}
	file, err := files[0].Open()
	if err != nil {
		return false
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maximumPDFBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(body)) != files[0].Size ||
		!bytes.HasPrefix(body, []byte("%PDF-")) {
		return false
	}
	sum := sha256.Sum256(body)
	target.Resume = &preparation.InterviewResumeUpload{
		Body: bytes.NewReader(body), Size: int64(len(body)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
	}
	return true
}

func (h *InterviewPreparationHTTPHandler) get(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	id := c.Param("interview_preparation_id")
	if !validResourceIdentifier(id) {
		writeHTTPError(c, http.StatusNotFound, "resource_not_found", false)
		return
	}
	value, err := h.application.Get(c.Request.Context(), actor, id)
	if err != nil {
		writeInterviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (h *InterviewPreparationHTTPHandler) patch(c *gin.Context) {
	setPrivateHeaders(c)
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	id := c.Param("interview_preparation_id")
	requestID, idOK := clientRequestID(c)
	var request PatchInterviewPreparationRequest
	if !validResourceIdentifier(id) || !idOK || !decodeJSONObject(c, &request) {
		writeHTTPError(c, http.StatusBadRequest, "invalid_request", false)
		return
	}
	value, _, err := h.application.Patch(c.Request.Context(), actor, id, requestID, request)
	if err != nil {
		writeInterviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func writeInterviewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInterviewPreparationInvalid):
		writeHTTPError(c, http.StatusBadRequest, "invalid_request", false)
	case errors.Is(err, ErrInterviewPreparationNotFound):
		writeHTTPError(c, http.StatusNotFound, "resource_not_found", false)
	case errors.Is(err, ErrInterviewPreparationRequestReuse):
		writeHTTPError(c, http.StatusConflict, "idempotency_key_conflict", false)
	case errors.Is(err, ErrInterviewPreparationConflict):
		writeHTTPError(c, http.StatusConflict, "resource_conflict", false)
	case errors.Is(err, ErrInterviewPreparationGeneration):
		writeHTTPError(c, http.StatusServiceUnavailable, "provider_unavailable", true)
	default:
		writeHTTPError(c, http.StatusInternalServerError, "internal_error", false)
	}
}

var _ InterviewPreparationHTTPApplication = (*InterviewPreparationService)(nil)
