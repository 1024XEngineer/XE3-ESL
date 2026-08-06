// Package app 定义简历模块的应用用例、输入命令和外部端口。
package app

import (
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// CreateCommand 表示上传一份新简历的应用输入。
type CreateCommand struct {
	Title          string
	Filename       string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	File           io.Reader
	IdempotencyKey string
}

// ListQuery 表示查询当前用户简历列表的分页输入。
type ListQuery struct {
	Cursor string
	Limit  int
}

// ListResult 表示一页简历和继续读取时使用的下一游标。
type ListResult struct {
	Items      []resume.Resume
	NextCursor string
}

// UpdateMetadataCommand 表示修改简历展示名称的应用输入。
type UpdateMetadataCommand struct {
	ResumeID        string
	Title           string
	ExpectedVersion int64
}

// UpdateContentCommand 表示手动保存结构化简历字段的应用输入。
type UpdateContentCommand struct {
	ResumeID        string
	Content         resume.Content
	ExpectedVersion int64
}

// ReplaceFileCommand 表示替换一份简历原始 PDF 的应用输入。
type ReplaceFileCommand struct {
	ResumeID        string
	Filename        string
	ContentType     string
	SizeBytes       int64
	ChecksumSHA256  string
	File            io.Reader
	ExpectedVersion int64
}

// ReplaceFileRecordCommand 表示文件保存成功后更新数据库记录的持久化输入。
type ReplaceFileRecordCommand struct {
	ResumeID        string
	Filename        string
	ContentType     string
	SizeBytes       int64
	ChecksumSHA256  string
	ObjectKey       string
	ExpectedVersion int64
}

// DeleteCommand 表示删除一份简历的应用输入。
type DeleteCommand struct {
	ResumeID        string
	ExpectedVersion int64
}

// Detail 表示简历元数据与当前内容修订组成的详情结果。
type Detail struct {
	Resume   resume.Resume
	Revision *resume.Revision
}

// ContentURL 表示查看原始 PDF 的短时有效地址。
type ContentURL struct {
	URL       string
	ExpiresAt time.Time
}
