// Package transport 实现 Resume 模块的 Gin HTTP 传输边界和受保护路由。
package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

const (
	maximumJSONBodyBytes      = 1 << 20
	multipartEnvelopeOverhead = 1 << 20
)

var (
	transportUUIDPattern    = regexp.MustCompile(`\A[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\z`)
	transportIdempotencyKey = regexp.MustCompile(`\A[A-Za-z0-9._~+/-]{8,128}\z`)
)

// Application 定义 Resume HTTP Handler 可以调用的应用用例。
type Application interface {
	// Create 创建当前用户的一份新简历。
	Create(context.Context, requestcontext.Actor, app.CreateCommand) (resume.Resume, error)
	CreateTemporary(context.Context, requestcontext.Actor, app.CreateCommand) (resume.Resume, error)
	// List 列出当前用户拥有的活动简历。
	List(context.Context, requestcontext.Actor, app.ListQuery) (app.ListResult, error)
	// Get 获取当前用户指定简历的详情。
	Get(context.Context, requestcontext.Actor, string) (app.Detail, error)
	GetTemporary(context.Context, requestcontext.Actor, string) (app.Detail, error)
	// UpdateMetadata 修改当前用户指定简历的元数据。
	UpdateMetadata(context.Context, requestcontext.Actor, app.UpdateMetadataCommand) (resume.Resume, error)
	// UpdateContent 保存当前用户指定简历的手动内容修订。
	UpdateContent(context.Context, requestcontext.Actor, app.UpdateContentCommand) (resume.Revision, error)
	// ReplaceFile 替换当前用户指定简历的原始 PDF。
	ReplaceFile(context.Context, requestcontext.Actor, app.ReplaceFileCommand) (resume.Resume, error)
	// GetContentURL 获取当前用户指定简历的短时读取地址。
	GetContentURL(context.Context, requestcontext.Actor, string) (app.ContentURL, error)
	// RetryParse 重新提交当前用户指定简历的解析任务。
	RetryParse(context.Context, requestcontext.Actor, string) (resume.Resume, error)
	RetryTemporaryParse(context.Context, requestcontext.Actor, string) (resume.Resume, error)
	// Delete 删除当前用户指定的简历。
	Delete(context.Context, requestcontext.Actor, app.DeleteCommand) error
	DeleteTemporary(context.Context, requestcontext.Actor, app.DeleteCommand) error
}

// HTTPHandler 接收已通过 Identity 认证中间件的 Resume HTTP 请求。
type HTTPHandler struct {
	application      Application
	errors           *httpresponse.Renderer
	maximumFileBytes int64
}

// NewHTTPHandler 创建 Resume HTTP Handler。
func NewHTTPHandler(application Application, maximumFileBytes int64) (*HTTPHandler, error) {
	if application == nil || maximumFileBytes < 1 ||
		maximumFileBytes > app.DefaultMaximumFileBytes {
		return nil, errors.New("resume: invalid HTTP configuration")
	}
	return &HTTPHandler{
		application:      application,
		errors:           httpresponse.NewRenderer(nil),
		maximumFileBytes: maximumFileBytes,
	}, nil
}

// RegisterRoutes 注册 Resume REST 路由；调用方必须挂载到 Identity 认证路由组。
func (h *HTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/v1/resumes", h.create)
	routes.POST("/v1/temporary-resumes", h.createTemporary)
	routes.GET("/v1/temporary-resumes/:resume_id", h.getTemporary)
	routes.POST("/v1/temporary-resumes/:resume_id/parse-retries", h.retryTemporaryParse)
	routes.DELETE("/v1/temporary-resumes/:resume_id", h.deleteTemporary)
	routes.GET("/v1/resumes", h.list)
	routes.GET("/v1/resumes/:resume_id", h.get)
	routes.PATCH("/v1/resumes/:resume_id", h.updateMetadata)
	routes.PATCH("/v1/resumes/:resume_id/content", h.updateContent)
	routes.PUT("/v1/resumes/:resume_id/file", h.replaceFile)
	routes.GET("/v1/resumes/:resume_id/content-url", h.getContentURL)
	routes.POST("/v1/resumes/:resume_id/parse-retries", h.retryParse)
	routes.DELETE("/v1/resumes/:resume_id", h.delete)
}

func (h *HTTPHandler) createTemporary(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	idempotencyKey, ok := h.idempotencyKey(c)
	if !ok {
		return
	}
	upload, ok := h.multipartPDF(c, false, false)
	if !ok {
		return
	}
	title := strings.TrimSuffix(upload.filename, ".pdf")
	if title == upload.filename {
		title = strings.TrimSuffix(upload.filename, ".PDF")
	}
	if strings.TrimSpace(title) == "" {
		title = "Temporary resume"
	}
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	item, err := h.application.CreateTemporary(c.Request.Context(), actor, app.CreateCommand{
		Title: title, Filename: upload.filename, ContentType: "application/pdf",
		SizeBytes: int64(len(upload.body)), ChecksumSHA256: upload.checksum,
		File: bytes.NewReader(upload.body), IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validTemporaryProjection(item, actor.UserID, "") {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusAccepted, newTemporaryResumeResponse(item))
}

func (h *HTTPHandler) getTemporary(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok {
		return
	}
	detail, err := h.application.GetTemporary(c.Request.Context(), actor, resumeID)
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validTemporaryProjection(detail.Resume, actor.UserID, resumeID) ||
		(detail.Revision != nil && detail.Revision.ResumeID != resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, newTemporaryDetailResponse(detail))
}

func (h *HTTPHandler) retryTemporaryParse(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	item, err := h.application.RetryTemporaryParse(c.Request.Context(), actor, resumeID)
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validTemporaryProjection(item, actor.UserID, resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusAccepted, newTemporaryResumeResponse(item))
}

func (h *HTTPHandler) deleteTemporary(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	expectedVersion, err := strconv.ParseInt(c.Query("expected_version"), 10, 64)
	if err != nil || expectedVersion < 1 {
		h.errors.Write(c, app.InvalidResumeError())
		return
	}
	if err := h.application.DeleteTemporary(c.Request.Context(), actor, app.DeleteCommand{
		ResumeID: resumeID, ExpectedVersion: expectedVersion,
	}); err != nil {
		h.errors.Write(c, err)
		return
	}
	setPrivateHeaders(c)
	c.Status(http.StatusNoContent)
}

// create 接收新简历 PDF 上传请求。
func (h *HTTPHandler) create(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	idempotencyKey, ok := h.idempotencyKey(c)
	if !ok {
		return
	}
	upload, ok := h.multipartPDF(c, true, false)
	if !ok {
		return
	}
	item, err := h.application.Create(c.Request.Context(), actor, app.CreateCommand{
		Title: upload.title, Filename: upload.filename, ContentType: "application/pdf",
		SizeBytes: int64(len(upload.body)), ChecksumSHA256: upload.checksum,
		File: bytes.NewReader(upload.body), IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validResumeProjection(item, actor.UserID, "") {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusAccepted, newResumeResponse(item))
}

// list 返回当前用户拥有的简历列表。
func (h *HTTPHandler) list(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	limit := app.MaxResumesPerUser
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > app.MaxResumesPerUser {
			h.errors.Write(c, app.InvalidResumeError())
			return
		}
		limit = parsed
	}
	cursor := c.Query("cursor")
	if cursor != "" && !transportUUIDPattern.MatchString(cursor) {
		h.errors.Write(c, app.InvalidResumeError())
		return
	}
	page, err := h.application.List(c.Request.Context(), actor, app.ListQuery{Cursor: cursor, Limit: limit})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if len(page.Items) > limit ||
		(page.NextCursor != "" && !transportUUIDPattern.MatchString(page.NextCursor)) {
		h.writeProjectionError(c)
		return
	}
	response := resumeListResponse{
		Items:      make([]resumeResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		if !validResumeProjection(item, actor.UserID, "") {
			h.writeProjectionError(c)
			return
		}
		response.Items = append(response.Items, newResumeResponse(item))
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, response)
}

// get 返回当前用户指定简历的元数据和当前结构化内容。
func (h *HTTPHandler) get(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok {
		return
	}
	detail, err := h.application.Get(c.Request.Context(), actor, resumeID)
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validResumeProjection(detail.Resume, actor.UserID, resumeID) ||
		(detail.Revision != nil && detail.Revision.ResumeID != resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, newDetailResponse(detail))
}

// updateMetadata 修改指定简历的展示名称。
func (h *HTTPHandler) updateMetadata(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	var request updateMetadataRequest
	if !h.decodeJSON(c, &request) {
		return
	}
	item, err := h.application.UpdateMetadata(c.Request.Context(), actor, app.UpdateMetadataCommand{
		ResumeID: resumeID, Title: request.Title, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validResumeProjection(item, actor.UserID, resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, newResumeResponse(item))
}

// updateContent 手动保存指定简历的结构化内容修订。
func (h *HTTPHandler) updateContent(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	var request updateContentRequest
	if !h.decodeJSON(c, &request) {
		return
	}
	if !request.hasRequiredArrays() {
		h.errors.Write(c, app.InvalidResumeError())
		return
	}
	revision, err := h.application.UpdateContent(c.Request.Context(), actor, app.UpdateContentCommand{
		ResumeID: resumeID, Content: request.Content, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if revision.ResumeID != resumeID || revision.Revision < 1 {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, newRevisionResponse(revision))
}

// replaceFile 替换指定简历的原始 PDF。
func (h *HTTPHandler) replaceFile(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	upload, ok := h.multipartPDF(c, false, true)
	if !ok {
		return
	}
	item, err := h.application.ReplaceFile(c.Request.Context(), actor, app.ReplaceFileCommand{
		ResumeID: resumeID, Filename: upload.filename, ContentType: "application/pdf",
		SizeBytes: int64(len(upload.body)), ChecksumSHA256: upload.checksum,
		File: bytes.NewReader(upload.body), ExpectedVersion: upload.expectedVersion,
	})
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validResumeProjection(item, actor.UserID, resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusAccepted, newResumeResponse(item))
}

// getContentURL 返回指定简历原始 PDF 的短时读取地址。
func (h *HTTPHandler) getContentURL(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok {
		return
	}
	content, err := h.application.GetContentURL(c.Request.Context(), actor, resumeID)
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	parsedURL, parseErr := url.ParseRequestURI(content.URL)
	if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" ||
		parsedURL.User != nil || parsedURL.Fragment != "" || content.ExpiresAt.IsZero() {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusOK, contentURLResponse{URL: content.URL, ExpiresAt: content.ExpiresAt})
}

// retryParse 重新提交一份解析失败的简历。
func (h *HTTPHandler) retryParse(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	item, err := h.application.RetryParse(c.Request.Context(), actor, resumeID)
	if err != nil {
		h.errors.Write(c, err)
		return
	}
	if !validResumeProjection(item, actor.UserID, resumeID) {
		h.writeProjectionError(c)
		return
	}
	setPrivateHeaders(c)
	c.JSON(http.StatusAccepted, newResumeResponse(item))
}

// delete 删除当前用户指定的简历。
func (h *HTTPHandler) delete(c *gin.Context) {
	actor, resumeID, ok := h.actorAndResumeID(c)
	if !ok || !h.requireIdempotencyKey(c) {
		return
	}
	expectedVersion, err := strconv.ParseInt(c.Query("expected_version"), 10, 64)
	if err != nil || expectedVersion < 1 {
		h.errors.Write(c, app.InvalidResumeError())
		return
	}
	if err := h.application.Delete(c.Request.Context(), actor, app.DeleteCommand{
		ResumeID: resumeID, ExpectedVersion: expectedVersion,
	}); err != nil {
		h.errors.Write(c, err)
		return
	}
	setPrivateHeaders(c)
	c.Status(http.StatusNoContent)
}

// uploadPayload 保存已限制并校验的 multipart PDF 请求。
type uploadPayload struct {
	title           string
	filename        string
	body            []byte
	checksum        string
	expectedVersion int64
}

// multipartPDF 严格解析 multipart 字段并限制总请求和文件字节数。
func (h *HTTPHandler) multipartPDF(c *gin.Context, requireTitle bool, requireVersion bool) (uploadPayload, bool) {
	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		h.maximumFileBytes+multipartEnvelopeOverhead,
	)
	if err := c.Request.ParseMultipartForm(h.maximumFileBytes + multipartEnvelopeOverhead); err != nil {
		var maximumError *http.MaxBytesError
		if errors.As(err, &maximumError) {
			h.errors.Write(c, app.ResumeFileTooLargeError())
		} else {
			h.errors.Write(c, app.InvalidResumeError())
		}
		return uploadPayload{}, false
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	allowedValues := map[string]bool{"title": requireTitle, "expected_version": requireVersion}
	for key, values := range c.Request.MultipartForm.Value {
		if _, allowed := allowedValues[key]; !allowed || len(values) != 1 {
			h.errors.Write(c, app.InvalidResumeError())
			return uploadPayload{}, false
		}
	}
	for key, files := range c.Request.MultipartForm.File {
		if key != "file" || len(files) != 1 {
			h.errors.Write(c, app.InvalidResumeError())
			return uploadPayload{}, false
		}
	}
	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 || files[0].Size < 1 {
		h.errors.Write(c, app.UnsupportedResumeFormatError())
		return uploadPayload{}, false
	}
	if files[0].Size > h.maximumFileBytes {
		h.errors.Write(c, app.ResumeFileTooLargeError())
		return uploadPayload{}, false
	}
	if contentType := files[0].Header.Get("Content-Type"); contentType != "" &&
		contentType != "application/pdf" && contentType != "application/octet-stream" {
		h.errors.Write(c, app.UnsupportedResumeFormatError())
		return uploadPayload{}, false
	}
	body, ok := h.readMultipartFile(c, files[0])
	if !ok {
		return uploadPayload{}, false
	}
	result := uploadPayload{
		title: c.Request.FormValue("title"), filename: files[0].Filename, body: body,
	}
	if requireTitle && result.title == "" {
		h.errors.Write(c, app.InvalidResumeError())
		return uploadPayload{}, false
	}
	if requireVersion {
		version, err := strconv.ParseInt(c.Request.FormValue("expected_version"), 10, 64)
		if err != nil || version < 1 {
			h.errors.Write(c, app.InvalidResumeError())
			return uploadPayload{}, false
		}
		result.expectedVersion = version
	}
	sum := sha256.Sum256(body)
	result.checksum = hex.EncodeToString(sum[:])
	return result, true
}

// readMultipartFile 读取单个受限 PDF 并校验魔数。
func (h *HTTPHandler) readMultipartFile(c *gin.Context, header *multipart.FileHeader) ([]byte, bool) {
	file, err := header.Open()
	if err != nil {
		h.errors.Write(c, app.InvalidResumeError())
		return nil, false
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, h.maximumFileBytes+1))
	if err != nil {
		h.errors.Write(c, app.InvalidResumeError())
		return nil, false
	}
	if int64(len(body)) > h.maximumFileBytes {
		h.errors.Write(c, app.ResumeFileTooLargeError())
		return nil, false
	}
	if len(body) < 5 || !bytes.Equal(body[:5], []byte("%PDF-")) {
		h.errors.Write(c, app.UnsupportedResumeFormatError())
		return nil, false
	}
	return body, true
}

// decodeJSON 严格读取一个 JSON 对象并拒绝未知字段和尾随内容。
func (h *HTTPHandler) decodeJSON(c *gin.Context, destination any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumJSONBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		h.errors.Write(c, app.InvalidResumeError())
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.errors.Write(c, app.InvalidResumeError())
		return false
	}
	return true
}

// actor 返回认证中间件写入的可信 Actor。
func (h *HTTPHandler) actor(c *gin.Context) (requestcontext.Actor, bool) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		c.Header("WWW-Authenticate", "Bearer")
		h.errors.Write(c, apperror.New(
			apperror.Unauthenticated,
			"authentication_required",
			"Authentication is required.",
		))
		return requestcontext.Actor{}, false
	}
	return actor, true
}

// actorAndResumeID 同时读取可信 Actor 和规范资源标识。
func (h *HTTPHandler) actorAndResumeID(c *gin.Context) (requestcontext.Actor, string, bool) {
	actor, ok := h.actor(c)
	if !ok {
		return requestcontext.Actor{}, "", false
	}
	resumeID := c.Param("resume_id")
	if !transportUUIDPattern.MatchString(resumeID) {
		h.errors.Write(c, app.ResumeNotFoundError())
		return requestcontext.Actor{}, "", false
	}
	return actor, resumeID, true
}

// idempotencyKey 读取并校验公开幂等头。
func (h *HTTPHandler) idempotencyKey(c *gin.Context) (string, bool) {
	value := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !transportIdempotencyKey.MatchString(value) {
		h.errors.Write(c, app.InvalidResumeError())
		return "", false
	}
	return value, true
}

// requireIdempotencyKey 仅校验不需要传入 Service 的写操作幂等头。
func (h *HTTPHandler) requireIdempotencyKey(c *gin.Context) bool {
	_, ok := h.idempotencyKey(c)
	return ok
}

// setPrivateHeaders 禁止简历和短时 URL 被共享缓存保存。
func setPrivateHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
}

// validResumeProjection 防止应用层错误地把其他 Actor 或其他资源投影到响应。
func validResumeProjection(item resume.Resume, ownerUserID string, expectedResumeID string) bool {
	return transportUUIDPattern.MatchString(item.ID) && item.OwnerUserID == ownerUserID &&
		(expectedResumeID == "" || item.ID == expectedResumeID) && item.Version >= 1 &&
		item.Title != "" && item.OriginalFilename != "" && item.ContentType == "application/pdf" &&
		item.SizeBytes > 0 && item.FileStatus != resume.FileDeleted &&
		!item.CreatedAt.IsZero() && !item.UpdatedAt.IsZero()
}

func validTemporaryProjection(item resume.Resume, ownerUserID string, expectedResumeID string) bool {
	return validResumeProjection(item, ownerUserID, expectedResumeID) && item.Temporary &&
		item.ExpiresAt != nil && item.ExpiresAt.After(item.CreatedAt)
}

// writeProjectionError 返回不会暴露错误投影内容的内部错误。
func (h *HTTPHandler) writeProjectionError(c *gin.Context) {
	h.errors.Write(c, app.RepositoryError(errors.New("resume: invalid application projection")))
}

var _ Application = (*app.Service)(nil)
