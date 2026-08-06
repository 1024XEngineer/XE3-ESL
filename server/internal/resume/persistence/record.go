// 本文件定义 Resume 数据表的 GORM Record；Record 不作为领域对象向上层暴露。
package persistence

import (
	"time"

	"gorm.io/gorm"
)

// resumeRecord 映射 resumes 表的一行。
type resumeRecord struct {
	ResumeID         string         `gorm:"column:resume_id;type:uuid;primaryKey"`
	OwnerUserID      string         `gorm:"column:owner_user_id;type:uuid;index"`
	Title            string         `gorm:"column:title"`
	OriginalFilename string         `gorm:"column:original_filename"`
	ContentType      string         `gorm:"column:content_type"`
	SizeBytes        int64          `gorm:"column:size_bytes"`
	ChecksumSHA256   string         `gorm:"column:checksum_sha256"`
	ObjectKey        string         `gorm:"column:object_key"`
	FileStatus       string         `gorm:"column:file_status"`
	ParseStatus      string         `gorm:"column:parse_status"`
	ParseFailureCode *string        `gorm:"column:parse_failure_code"`
	CurrentRevision  *int64         `gorm:"column:current_revision"`
	Temporary        bool           `gorm:"column:is_temporary"`
	ExpiresAt        *time.Time     `gorm:"column:expires_at"`
	Version          int64          `gorm:"column:version"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName 返回 Resume 元数据表的稳定名称。
func (resumeRecord) TableName() string {
	return "resumes"
}

// revisionRecord 映射 resume_revisions 表的一行。
type revisionRecord struct {
	OwnerUserID   string    `gorm:"column:owner_user_id;type:uuid"`
	ResumeID      string    `gorm:"column:resume_id;type:uuid;primaryKey"`
	Revision      int64     `gorm:"column:revision;primaryKey"`
	Source        string    `gorm:"column:source"`
	ParserVersion *string   `gorm:"column:parser_version"`
	Content       []byte    `gorm:"column:content;type:jsonb"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

// TableName 返回结构化简历修订表的稳定名称。
func (revisionRecord) TableName() string {
	return "resume_revisions"
}
