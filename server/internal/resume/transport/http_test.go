// 本文件逐路由验证 Resume HTTP 契约、可信 Actor 和请求防护。
package transport

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

const transportTestResumeID = "30000000-0000-4000-8000-000000000003"

var transportTestActor = requestcontext.Actor{
	UserID: "10000000-0000-4000-8000-000000000001", SessionID: "session-1",
}

// TestCreateUsesTrustedActorAndValidatesPDF 验证上传 Handler 只使用可信 Actor 并传递已校验 PDF。
func TestCreateUsesTrustedActorAndValidatesPDF(t *testing.T) {
	application := newTransportApplicationFake()
	router := newResumeRouter(t, application, &transportTestActor, 1024)
	body, contentType := multipartRequest(t, map[string]string{"title": "Backend Resume"}, "%PDF-1.4 test")
	request := httptest.NewRequest(http.MethodPost, "/v1/resumes", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Idempotency-Key", "request-key-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted || application.calls["create"] != 1 ||
		application.actor != transportTestActor || application.createCommand.IdempotencyKey != "request-key-1" {
		t.Fatalf("status = %d, calls = %#v, actor = %#v, command = %#v, body = %s",
			response.Code, application.calls, application.actor, application.createCommand, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(response.Body.String(), `"resume_id"`) {
		t.Fatalf("unsafe or invalid response: headers=%v body=%s", response.Header(), response.Body.String())
	}
}

// TestResumeRoutesCoverCRUDContract 验证其余公开路由调用正确应用用例和状态码。
func TestResumeRoutesCoverCRUDContract(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        io.Reader
		idempotent  bool
		status      int
		call        string
	}{
		{name: "list", method: http.MethodGet, path: "/v1/resumes?limit=3", status: 200, call: "list"},
		{name: "get", method: http.MethodGet, path: "/v1/resumes/" + transportTestResumeID, status: 200, call: "get"},
		{name: "metadata", method: http.MethodPatch, path: "/v1/resumes/" + transportTestResumeID,
			contentType: "application/json", body: strings.NewReader(`{"title":"Updated","expected_version":2}`),
			idempotent: true, status: 200, call: "metadata"},
		{name: "content", method: http.MethodPatch, path: "/v1/resumes/" + transportTestResumeID + "/content",
			contentType: "application/json", body: strings.NewReader(`{"content":{"target_position":"Backend Engineer","professional_summary":"","work_experiences":[],"project_experiences":[],"education_experiences":[],"skills":[]},"expected_version":2}`),
			idempotent: true, status: 200, call: "content"},
		{name: "content URL", method: http.MethodGet, path: "/v1/resumes/" + transportTestResumeID + "/content-url",
			status: 200, call: "content_url"},
		{name: "retry", method: http.MethodPost, path: "/v1/resumes/" + transportTestResumeID + "/parse-retries",
			idempotent: true, status: 202, call: "retry"},
		{name: "delete", method: http.MethodDelete, path: "/v1/resumes/" + transportTestResumeID + "?expected_version=2",
			idempotent: true, status: 204, call: "delete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := newTransportApplicationFake()
			router := newResumeRouter(t, application, &transportTestActor, 1024)
			request := httptest.NewRequest(test.method, test.path, test.body)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.idempotent {
				request.Header.Set("Idempotency-Key", "request-key-1")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || application.calls[test.call] != 1 {
				t.Fatalf("status = %d, calls = %#v, body = %s", response.Code, application.calls, response.Body.String())
			}
		})
	}
}

// TestReplaceAndRequestGuards 验证替换 multipart、认证、幂等键和大小限制。
func TestReplaceAndRequestGuards(t *testing.T) {
	application := newTransportApplicationFake()
	router := newResumeRouter(t, application, &transportTestActor, 32)
	body, contentType := multipartRequest(t, map[string]string{"expected_version": "2"}, "%PDF-1.4 replacement")
	request := httptest.NewRequest(http.MethodPut, "/v1/resumes/"+transportTestResumeID+"/file", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Idempotency-Key", "request-key-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || application.calls["replace"] != 1 {
		t.Fatalf("replace status = %d, calls = %#v, body = %s", response.Code, application.calls, response.Body.String())
	}

	unauthenticated := newResumeRouter(t, application, nil, 32)
	response = httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/resumes", nil))
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "authentication_required") {
		t.Fatalf("unauthenticated response = %d %s", response.Code, response.Body.String())
	}

	largeBody, largeType := multipartRequest(t, map[string]string{"title": "Resume"}, "%PDF-1.4 "+strings.Repeat("x", 64))
	request = httptest.NewRequest(http.MethodPost, "/v1/resumes", largeBody)
	request.Header.Set("Content-Type", largeType)
	request.Header.Set("Idempotency-Key", "request-key-1")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "resume_file_too_large") {
		t.Fatalf("large response = %d %s", response.Code, response.Body.String())
	}
}

// newResumeRouter 创建带可选可信 Actor 的 Gin 测试路由。
func newResumeRouter(
	t *testing.T,
	application Application,
	actor *requestcontext.Actor,
	maximumFileBytes int64,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if actor != nil {
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(requestcontext.WithActor(c.Request.Context(), *actor))
			c.Next()
		})
	}
	handler, err := NewHTTPHandler(application, maximumFileBytes)
	if err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	handler.RegisterRoutes(router)
	return router
}

// multipartRequest 创建一个只包含声明字段和单份 PDF 的请求体。
func multipartRequest(t *testing.T, fields map[string]string, pdfBody string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="resume.pdf"`)
	header.Set("Content-Type", "application/pdf")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	_, _ = part.Write([]byte(pdfBody))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}
	return &body, writer.FormDataContentType()
}

// transportApplicationFake 返回稳定投影并记录每个 HTTP 用例调用。
type transportApplicationFake struct {
	calls         map[string]int
	actor         requestcontext.Actor
	createCommand app.CreateCommand
	item          resume.Resume
}

// newTransportApplicationFake 创建可覆盖全部路由的应用替身。
func newTransportApplicationFake() *transportApplicationFake {
	now := time.Now().UTC()
	return &transportApplicationFake{
		calls: make(map[string]int),
		item: resume.Resume{
			ID: transportTestResumeID, OwnerUserID: transportTestActor.UserID,
			Title: "Backend Resume", OriginalFilename: "resume.pdf", ContentType: "application/pdf",
			SizeBytes: 20, FileStatus: resume.FileAvailable, ParseStatus: resume.ParseFailed,
			Version: 2, CreatedAt: now, UpdatedAt: now,
		},
	}
}

// Create 记录创建请求并返回测试简历。
func (application *transportApplicationFake) Create(_ context.Context, actor requestcontext.Actor, command app.CreateCommand) (resume.Resume, error) {
	application.calls["create"]++
	application.actor = actor
	application.createCommand = command
	return application.item, nil
}

// List 返回单份测试简历。
func (application *transportApplicationFake) List(_ context.Context, actor requestcontext.Actor, _ app.ListQuery) (app.ListResult, error) {
	application.calls["list"]++
	application.actor = actor
	return app.ListResult{Items: []resume.Resume{application.item}}, nil
}

// Get 返回带当前修订的测试详情。
func (application *transportApplicationFake) Get(_ context.Context, actor requestcontext.Actor, _ string) (app.Detail, error) {
	application.calls["get"]++
	application.actor = actor
	revision := resume.Revision{
		ResumeID: application.item.ID, Revision: 1, Source: resume.RevisionParser,
		ParserVersion: "test/v1", Content: emptyTransportContent(), CreatedAt: time.Now().UTC(),
	}
	return app.Detail{Resume: application.item, Revision: &revision}, nil
}

// UpdateMetadata 返回更新后的测试元数据。
func (application *transportApplicationFake) UpdateMetadata(_ context.Context, actor requestcontext.Actor, command app.UpdateMetadataCommand) (resume.Resume, error) {
	application.calls["metadata"]++
	application.actor = actor
	application.item.Title = command.Title
	return application.item, nil
}

// UpdateContent 返回测试手动修订。
func (application *transportApplicationFake) UpdateContent(_ context.Context, actor requestcontext.Actor, command app.UpdateContentCommand) (resume.Revision, error) {
	application.calls["content"]++
	application.actor = actor
	return resume.Revision{ResumeID: command.ResumeID, Revision: 2, Source: resume.RevisionManual, Content: command.Content, CreatedAt: time.Now().UTC()}, nil
}

// ReplaceFile 返回测试替换结果。
func (application *transportApplicationFake) ReplaceFile(_ context.Context, actor requestcontext.Actor, _ app.ReplaceFileCommand) (resume.Resume, error) {
	application.calls["replace"]++
	application.actor = actor
	return application.item, nil
}

// GetContentURL 返回测试短时地址。
func (application *transportApplicationFake) GetContentURL(_ context.Context, actor requestcontext.Actor, _ string) (app.ContentURL, error) {
	application.calls["content_url"]++
	application.actor = actor
	return app.ContentURL{URL: "https://files.example.test/resume.pdf", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

// RetryParse 返回重新排队的测试状态。
func (application *transportApplicationFake) RetryParse(_ context.Context, actor requestcontext.Actor, _ string) (resume.Resume, error) {
	application.calls["retry"]++
	application.actor = actor
	application.item.ParseStatus = resume.ParseQueued
	return application.item, nil
}

// Delete 记录测试删除请求。
func (application *transportApplicationFake) Delete(_ context.Context, actor requestcontext.Actor, _ app.DeleteCommand) error {
	application.calls["delete"]++
	application.actor = actor
	return nil
}

// emptyTransportContent 返回满足公开必填数组约束的空内容。
func emptyTransportContent() resume.Content {
	return resume.Content{
		WorkExperiences: []resume.WorkExperience{}, ProjectExperiences: []resume.ProjectExperience{},
		EducationExperiences: []resume.EducationExperience{}, Skills: []string{},
	}
}

var _ Application = (*transportApplicationFake)(nil)
