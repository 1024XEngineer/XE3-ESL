// Package resume 定义简历模块稳定的领域模型，不依赖 HTTP、GORM 或外部解析器。
package resume

import "time"

// Resume 表示一份归属于单个用户的简历聚合根。
type Resume struct {
	ID               string
	OwnerUserID      string
	Title            string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	ChecksumSHA256   string
	ObjectKey        string
	FileStatus       FileStatus
	ParseStatus      ParseStatus
	ParseFailureCode string
	CurrentRevision  int64
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

// Content 表示可由解析器生成、也可由用户手动修改的结构化简历内容。
type Content struct {
	TargetPosition       string
	ProfessionalSummary  string
	WorkExperiences      []WorkExperience
	ProjectExperiences   []ProjectExperience
	EducationExperiences []EducationExperience
	Skills               []string
}

// WorkExperience 表示一段工作经历。
type WorkExperience struct {
	Company      string
	Position     string
	StartDate    string
	EndDate      string
	Duties       []string
	Achievements []string
}

// ProjectExperience 表示一段项目经历。
type ProjectExperience struct {
	ProjectName  string
	Role         string
	Description  string
	Technologies []string
	Duties       []string
	Achievements []string
}

// EducationExperience 表示一段教育经历。
type EducationExperience struct {
	School    string
	Major     string
	Degree    string
	StartDate string
	EndDate   string
}

// Revision 表示一次不可变的结构化简历内容修订。
type Revision struct {
	ResumeID      string
	Revision      int64
	Source        RevisionSource
	ParserVersion string
	Content       Content
	CreatedAt     time.Time
}
