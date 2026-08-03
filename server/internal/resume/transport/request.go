// 本文件定义 Resume HTTP 接口使用的请求 DTO，避免传输字段进入领域模型。
package transport

import "github.com/1024XEngineer/XE3-ESL/server/internal/resume"

// updateMetadataRequest 表示修改简历展示名称的 JSON 请求体。
type updateMetadataRequest struct {
	Title           string `json:"title"`
	ExpectedVersion int64  `json:"expected_version"`
}

// updateContentRequest 表示手动修改结构化简历字段的 JSON 请求体。
type updateContentRequest struct {
	Content         resume.Content `json:"content"`
	ExpectedVersion int64          `json:"expected_version"`
}
