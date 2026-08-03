// 本文件基于公共 apperror 定义 Resume 模块拥有的稳定业务错误码。
package app

import "github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"

// InvalidResumeError 返回简历请求字段不合法错误。
func InvalidResumeError() error {
	// TODO(issue-320): 后续实现字段级错误详情。
	return apperror.New(apperror.InvalidArgument, "invalid_request", "Request validation failed.")
}

// ResumeNotFoundError 返回当前用户不可见指定简历的错误。
func ResumeNotFoundError() error {
	// TODO(issue-320): 后续由 Repository 统一映射不存在和跨用户访问。
	return apperror.New(apperror.NotFound, "resume_not_found", "Resume was not found.")
}

// ResumeLimitExceededError 返回当前用户已达到三份简历上限的错误。
func ResumeLimitExceededError() error {
	// TODO(issue-320): 后续由原子创建事务返回该错误。
	return apperror.New(apperror.Conflict, "resume_limit_exceeded", "Resume limit was reached.")
}

// ResumeVersionConflictError 返回简历乐观锁版本冲突错误。
func ResumeVersionConflictError() error {
	// TODO(issue-320): 后续由条件更新的受影响行数触发该错误。
	return apperror.New(apperror.Conflict, "resume_version_conflict", "Resume changed before this operation.")
}

// UnsupportedResumeFormatError 返回上传文件不是受支持文本型 PDF 的错误。
func UnsupportedResumeFormatError() error {
	// TODO(issue-320): 后续结合文件魔数和 PDF 文本能力检测。
	return apperror.New(apperror.InvalidArgument, "unsupported_resume_format", "Only text-based PDF resumes are supported.")
}

// ResumeFileTooLargeError 返回上传文件超过十兆字节限制的错误。
func ResumeFileTooLargeError() error {
	// TODO(issue-320): 后续在读取请求体和文件流时双重限制大小。
	return apperror.New(apperror.InvalidArgument, "resume_file_too_large", "Resume file exceeds the allowed size.")
}

// ResumeParseFailedError 返回简历解析暂时失败的错误。
func ResumeParseFailedError() error {
	// TODO(issue-320): 后续根据解析失败类型决定是否允许重试。
	return apperror.New(apperror.Unavailable, "resume_parse_failed", "Resume parsing failed.", apperror.WithRetryable(true))
}

// NotImplementedError 返回当前接口尚未完成真实实现的错误。
func NotImplementedError() error {
	// TODO(issue-320): 各接口完成真实实现后删除该占位错误。
	return apperror.New(apperror.Unimplemented, "resume_not_implemented", "Resume operation is not implemented yet.")
}
