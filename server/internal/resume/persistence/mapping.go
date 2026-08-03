// 本文件声明 GORM Record 与 Resume 领域模型之间的转换边界。
package persistence

import "github.com/1024XEngineer/XE3-ESL/server/internal/resume"

// resumeToRecord 把 Resume 领域模型转换为持久化 Record。
func resumeToRecord(resume.Resume) resumeRecord {
	// TODO(issue-320): 实现领域模型到数据库字段的完整转换。
	return resumeRecord{}
}

// resumeFromRecord 把持久化 Record 转换为 Resume 领域模型。
func resumeFromRecord(resumeRecord) (resume.Resume, error) {
	// TODO(issue-320): 实现数据库字段校验和领域模型恢复。
	return resume.Resume{}, nil
}

// revisionToRecord 把结构化内容修订转换为持久化 Record。
func revisionToRecord(resume.Revision) (revisionRecord, error) {
	// TODO(issue-320): 实现 JSON 编码和修订元数据转换。
	return revisionRecord{}, nil
}

// revisionFromRecord 把持久化 Record 转换为结构化内容修订。
func revisionFromRecord(revisionRecord) (resume.Revision, error) {
	// TODO(issue-320): 实现 JSON 解码和修订内容校验。
	return resume.Revision{}, nil
}
