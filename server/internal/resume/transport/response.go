// 本文件定义 Resume HTTP 接口的稳定响应 DTO，不直接序列化内部持久化对象。
package transport

import "time"

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

// contentURLResponse 表示原始 PDF 的短时读取地址和过期时间。
type contentURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
