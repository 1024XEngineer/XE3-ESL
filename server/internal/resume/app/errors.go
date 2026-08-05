// 本文件基于公共 apperror 定义 Resume 模块拥有的稳定业务错误码。
package app

import "github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"

// InvalidResumeError 返回简历请求字段不合法错误。
func InvalidResumeError() error {
	return apperror.New(apperror.InvalidArgument, "invalid_request", "Request validation failed.")
}

// ResumeNotFoundError 返回当前用户不可见指定简历的错误。
func ResumeNotFoundError() error {
	return apperror.New(apperror.NotFound, "resume_not_found", "Resume was not found.")
}

// ResumeLimitExceededError 返回当前用户已达到三份简历上限的错误。
func ResumeLimitExceededError() error {
	return apperror.New(apperror.Conflict, "resume_limit_exceeded", "Resume limit was reached.")
}

// ResumeVersionConflictError 返回简历乐观锁版本冲突错误。
func ResumeVersionConflictError() error {
	return apperror.New(apperror.Conflict, "resume_version_conflict", "Resume changed before this operation.")
}

// ResumeRevisionUnavailableError reports that a Resume has no parsed Revision
// that can be bound to a Preparation Profile yet.
func ResumeRevisionUnavailableError() error {
	return apperror.New(
		apperror.FailedPrecondition,
		"resume_revision_unavailable",
		"Resume parsing has not produced an available revision.",
	)
}

// UnsupportedResumeFormatError 返回上传文件不是受支持文本型 PDF 的错误。
func UnsupportedResumeFormatError() error {
	return apperror.New(apperror.InvalidArgument, "unsupported_resume_format", "Only text-based PDF resumes are supported.")
}

// ResumeFileTooLargeError 返回上传文件超过十兆字节限制的错误。
func ResumeFileTooLargeError() error {
	return apperror.New(apperror.InvalidArgument, "resume_file_too_large", "Resume file exceeds the allowed size.")
}

// ResumeParseFailedError 返回简历解析暂时失败的错误。
func ResumeParseFailedError() error {
	return apperror.New(apperror.Unavailable, "resume_parse_failed", "Resume parsing failed.", apperror.WithRetryable(true))
}

// RepositoryError 返回不会泄露数据库细节的内部持久化错误。
func RepositoryError(cause error) error {
	return apperror.New(
		apperror.Internal,
		"internal_error",
		"An internal error occurred.",
		apperror.WithCause(cause),
	)
}
