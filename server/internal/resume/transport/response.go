// 本文件定义 Resume HTTP 接口的稳定响应 DTO，不直接序列化内部持久化对象。
package transport

import (
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

// resumeResponse 表示一份简历的公开元数据和处理状态。
type resumeResponse struct {
	ResumeID         string     `json:"resume_id"`
	Title            string     `json:"title"`
	OriginalFilename string     `json:"original_filename"`
	ContentType      string     `json:"content_type"`
	SizeBytes        int64      `json:"size_bytes"`
	FileStatus       string     `json:"file_status"`
	ParseStatus      string     `json:"parse_status"`
	CurrentRevision  int64      `json:"current_revision,omitempty"`
	Version          int64      `json:"version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// resumeListResponse 表示当前用户的简历列表和可选下一页游标。
type resumeListResponse struct {
	Items      []resumeResponse `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type temporaryResumeResponse struct {
	resumeResponse
	ExpiresAt time.Time `json:"expires_at"`
}

type temporaryResumeDetailResponse struct {
	Resume          temporaryResumeResponse `json:"resume"`
	CurrentRevision *resumeRevisionResponse `json:"current_revision,omitempty"`
}

// contentURLResponse 表示原始 PDF 的短时读取地址和过期时间。
type contentURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// resumeRevisionResponse 表示一次不可变结构化内容修订。
type resumeRevisionResponse struct {
	ResumeID      string         `json:"resume_id"`
	Revision      int64          `json:"revision"`
	Source        string         `json:"source"`
	ParserVersion string         `json:"parser_version,omitempty"`
	Content       resume.Content `json:"content"`
	CreatedAt     time.Time      `json:"created_at"`
}

// resumeDetailResponse 表示简历元数据和可选当前修订。
type resumeDetailResponse struct {
	Resume          resumeResponse          `json:"resume"`
	CurrentRevision *resumeRevisionResponse `json:"current_revision,omitempty"`
}

// newResumeResponse 把领域对象投影为公开元数据。
func newResumeResponse(item resume.Resume) resumeResponse {
	return resumeResponse{
		ResumeID: item.ID, Title: item.Title, OriginalFilename: item.OriginalFilename,
		ContentType: item.ContentType, SizeBytes: item.SizeBytes,
		FileStatus: string(item.FileStatus), ParseStatus: string(item.ParseStatus),
		CurrentRevision: item.CurrentRevision, Version: item.Version,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, DeletedAt: item.DeletedAt,
	}
}

func newTemporaryResumeResponse(item resume.Resume) temporaryResumeResponse {
	response := temporaryResumeResponse{resumeResponse: newResumeResponse(item)}
	if item.ExpiresAt != nil {
		response.ExpiresAt = item.ExpiresAt.UTC()
	}
	return response
}

func newTemporaryDetailResponse(detail app.Detail) temporaryResumeDetailResponse {
	response := temporaryResumeDetailResponse{Resume: newTemporaryResumeResponse(detail.Resume)}
	if detail.Revision != nil {
		revision := newRevisionResponse(*detail.Revision)
		response.CurrentRevision = &revision
	}
	return response
}

// newRevisionResponse 把修订领域对象投影为公开响应。
func newRevisionResponse(item resume.Revision) resumeRevisionResponse {
	return resumeRevisionResponse{
		ResumeID: item.ResumeID, Revision: item.Revision, Source: string(item.Source),
		ParserVersion: item.ParserVersion, Content: item.Content, CreatedAt: item.CreatedAt,
	}
}

// newDetailResponse 把应用详情投影为公开响应。
func newDetailResponse(detail app.Detail) resumeDetailResponse {
	response := resumeDetailResponse{Resume: newResumeResponse(detail.Resume)}
	if detail.Revision != nil {
		revision := newRevisionResponse(*detail.Revision)
		response.CurrentRevision = &revision
	}
	return response
}
