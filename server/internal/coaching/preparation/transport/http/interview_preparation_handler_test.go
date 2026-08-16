package http

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPreparationErrorsUseCanonicalPublicCodes(t *testing.T) {
	tests := []struct {
		name      string
		write     func(*gin.Context)
		status    int
		code      string
		retryable bool
	}{
		{"interview not found", func(c *gin.Context) { writeInterviewError(c, ErrInterviewPreparationNotFound) }, http.StatusNotFound, "resource_not_found", false},
		{"interview request reuse", func(c *gin.Context) { writeInterviewError(c, ErrInterviewPreparationRequestReuse) }, http.StatusConflict, "idempotency_key_conflict", false},
		{"interview conflict", func(c *gin.Context) { writeInterviewError(c, ErrInterviewPreparationConflict) }, http.StatusConflict, "resource_conflict", false},
		{"interview generation", func(c *gin.Context) { writeInterviewError(c, ErrInterviewPreparationGeneration) }, http.StatusServiceUnavailable, "provider_unavailable", true},
		{"plan not found", func(c *gin.Context) { writePlanServiceError(c, ErrPlanNotFound) }, http.StatusNotFound, "practice_plan_not_found", false},
		{"plan request reuse", func(c *gin.Context) { writePlanServiceError(c, ErrPlanIdempotencyConflict) }, http.StatusConflict, "idempotency_key_conflict", false},
		{"plan conflict", func(c *gin.Context) { writePlanServiceError(c, ErrPlanConflict) }, http.StatusConflict, "resource_conflict", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			test.write(context)
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d", response.Code, test.status)
			}
			var body struct {
				Error struct {
					Code      string `json:"code"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Error.Code != test.code || body.Error.Retryable != test.retryable {
				t.Fatalf("error=%#v, want code=%q retryable=%t", body.Error, test.code, test.retryable)
			}
		})
	}
}

func TestDecodeInterviewCreateReadsOptionalPDF(t *testing.T) {
	body, contentType := interviewCreateMultipart(t, false)
	request := httptest.NewRequest("POST", "/v1/interview-preparations", body)
	request.Header.Set("Content-Type", contentType)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	var decoded CreateInterviewPreparationRequest
	if !decodeInterviewCreate(context, &decoded) {
		t.Fatal("decodeInterviewCreate rejected valid multipart request")
	}
	if decoded.Input.JobTitle != "Backend Engineer" || decoded.Resume == nil {
		t.Fatalf("decoded=%#v", decoded)
	}
	pdf, err := io.ReadAll(decoded.Resume.Body)
	if err != nil {
		t.Fatalf("read decoded PDF: %v", err)
	}
	if string(pdf) != "%PDF-1.7" || decoded.Resume.Size != int64(len(pdf)) ||
		len(decoded.Resume.ChecksumSHA256) != 64 {
		t.Fatalf("resume=%#v pdf=%q", decoded.Resume, pdf)
	}
}

func TestDecodeInterviewCreateRejectsUnknownMultipartField(t *testing.T) {
	body, contentType := interviewCreateMultipart(t, true)
	request := httptest.NewRequest("POST", "/v1/interview-preparations", body)
	request.Header.Set("Content-Type", contentType)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	var decoded CreateInterviewPreparationRequest
	if decodeInterviewCreate(context, &decoded) {
		t.Fatal("decodeInterviewCreate accepted unknown multipart field")
	}
}

func interviewCreateMultipart(t *testing.T, withUnknownField bool) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("input", `{"source":"quick_start","job_title":"Backend Engineer"}`); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if withUnknownField {
		if err := writer.WriteField("unknown", "value"); err != nil {
			t.Fatalf("write unknown field: %v", err)
		}
	}
	part, err := writer.CreateFormFile("resume", "resume.pdf")
	if err != nil {
		t.Fatalf("create PDF part: %v", err)
	}
	if _, err := part.Write([]byte("%PDF-1.7")); err != nil {
		t.Fatalf("write PDF: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}
