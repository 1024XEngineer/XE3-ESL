package apperror

// Code 表示稳定且与传输协议无关的错误分类。
type Code string

const (
	CodeInvalidArgument    Code = "invalid_argument"
	CodeUnauthenticated    Code = "unauthenticated"
	CodePermissionDenied   Code = "permission_denied"
	CodeNotFound           Code = "not_found"
	CodeAlreadyExists      Code = "already_exists"
	CodeConflict           Code = "conflict"
	CodeFailedPrecondition Code = "failed_precondition"
	CodeResourceExhausted  Code = "resource_exhausted"
	CodeDeadlineExceeded   Code = "deadline_exceeded"
	CodeUnimplemented      Code = "unimplemented"
	CodeUnavailable        Code = "unavailable"
	CodeInternal           Code = "internal"
)

var validCodes = map[Code]struct{}{
	CodeInvalidArgument:    {},
	CodeUnauthenticated:    {},
	CodePermissionDenied:   {},
	CodeNotFound:           {},
	CodeAlreadyExists:      {},
	CodeConflict:           {},
	CodeFailedPrecondition: {},
	CodeResourceExhausted:  {},
	CodeDeadlineExceeded:   {},
	CodeUnimplemented:      {},
	CodeUnavailable:        {},
	CodeInternal:           {},
}

// IsValidCode 判断 code 是否属于公共错误码集合。
func IsValidCode(code Code) bool {
	_, ok := validCodes[code]
	return ok
}
