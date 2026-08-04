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
	TargetPosition       string                `json:"target_position"`
	ProfessionalSummary  string                `json:"professional_summary"`
	WorkExperiences      []WorkExperience      `json:"work_experiences"`
	ProjectExperiences   []ProjectExperience   `json:"project_experiences"`
	EducationExperiences []EducationExperience `json:"education_experiences"`
	Skills               []string              `json:"skills"`
	Awards               []string              `json:"awards"`
}

// WorkExperience 表示一段工作经历。
type WorkExperience struct {
	Company      string   `json:"company"`
	Position     string   `json:"position"`
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

// ProjectExperience 表示一段项目经历。
type ProjectExperience struct {
	ProjectName  string   `json:"project_name"`
	Role         string   `json:"role"`
	Description  string   `json:"description"`
	Technologies []string `json:"technologies"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

// EducationExperience 表示一段教育经历。
type EducationExperience struct {
	School    string `json:"school"`
	Major     string `json:"major"`
	Degree    string `json:"degree"`
	GPA       string `json:"gpa,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
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
