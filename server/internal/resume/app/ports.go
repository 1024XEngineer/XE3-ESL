// 本文件定义应用层依赖的持久化、文件存储、解析器和标识生成端口。
package app

import (
	"context"
	"io"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// Repository 定义简历应用层需要的原子持久化能力。
type Repository interface {
	// CreateWithinLimit 在同一事务内校验数量上限并创建简历。
	CreateWithinLimit(context.Context, resume.Resume, int) error
	// ListByOwner 分页列出指定用户拥有的活动简历。
	ListByOwner(context.Context, string, ListQuery) ([]resume.Resume, error)
	// FindByOwnerAndID 按所有者和简历标识查询元数据。
	FindByOwnerAndID(context.Context, string, string) (resume.Resume, error)
	// FindDetailByOwnerAndID 按所有者和简历标识查询详情。
	FindDetailByOwnerAndID(context.Context, string, string) (Detail, error)
	// UpdateMetadata 使用期望版本更新简历元数据。
	UpdateMetadata(context.Context, string, UpdateMetadataCommand) (resume.Resume, error)
	// ReplaceFileRecord 原子替换原始文件记录并重置解析状态。
	ReplaceFileRecord(context.Context, string, ReplaceFileRecordCommand) (resume.Resume, error)
	// MarkFileAvailableAndQueueParse 标记文件可用并提交解析任务。
	MarkFileAvailableAndQueueParse(context.Context, string, string) error
	// SaveManualRevision 保存用户手动编辑的不可变内容修订。
	SaveManualRevision(context.Context, string, UpdateContentCommand) (resume.Revision, error)
	// MarkDeleting 使用期望版本把简历标记为删除中。
	MarkDeleting(context.Context, string, DeleteCommand) (resume.Resume, error)
	// MarkDeleted 完成简历软删除状态写入。
	MarkDeleted(context.Context, string, string) error
	// QueueParse 重新提交当前用户指定简历的解析任务。
	QueueParse(context.Context, string, string) error
	// ClaimNextQueuedParse 领取下一份可处理的解析任务。
	ClaimNextQueuedParse(context.Context) (resume.Resume, bool, error)
	// CompleteParse 原子保存解析修订并完成解析任务。
	CompleteParse(context.Context, resume.Resume, resume.Revision) error
	// FailParse 保存解析失败状态和稳定失败码。
	FailParse(context.Context, resume.Resume, string) error
}

// FileStorage 定义原始 PDF 文件的保存、读取、授权访问和删除能力。
type FileStorage interface {
	// Put 保存一份受保护的原始 PDF 对象。
	Put(context.Context, string, io.Reader, int64, string) error
	// Open 打开一份受保护的原始 PDF 对象。
	Open(context.Context, string) (io.ReadCloser, error)
	// SignedReadURL 创建一条短时有效的受保护读取地址。
	SignedReadURL(context.Context, string, time.Duration) (string, time.Time, error)
	// Delete 删除一份受保护的原始 PDF 对象。
	Delete(context.Context, string) error
}

// Parser 定义把文本型 PDF 转换为结构化简历内容的能力。
type Parser interface {
	// Parse 把文本型 PDF 转换为结构化简历内容。
	Parse(context.Context, io.Reader) (resume.Content, error)
	// Version 返回生成解析修订时记录的解析器版本。
	Version() string
}

// IDGenerator 定义 Resume 模块生成资源标识和对象键的能力。
type IDGenerator interface {
	// NewResumeID 生成一条新的简历资源标识。
	NewResumeID() string
	// NewObjectKey 为用户简历生成不可猜测的存储对象键。
	NewObjectKey(string, string) string
}
