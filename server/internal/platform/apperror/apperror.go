// Package apperror defines transport-independent application errors.
//
// Business modules own their stable delivery codes. This package owns only the
// common categories used by delivery adapters to select protocol semantics.
package apperror

import "errors"

// Category classifies an application error without coupling it to a delivery
// protocol such as HTTP.
type Category string

const (
	InvalidArgument     Category = "invalid_argument"
	Unauthenticated     Category = "unauthenticated"
	PermissionDenied    Category = "permission_denied"
	NotFound            Category = "not_found"
	AlreadyExists       Category = "already_exists"
	Conflict            Category = "conflict"
	FailedPrecondition  Category = "failed_precondition"
	UnprocessableEntity Category = "unprocessable_entity"
	ResourceExhausted   Category = "resource_exhausted"
	DeadlineExceeded    Category = "deadline_exceeded"
	Unimplemented       Category = "unimplemented"
	Unavailable         Category = "unavailable"
	Internal            Category = "internal"
)

// Valid reports whether category is one of the common categories owned by this
// package.
func (category Category) Valid() bool {
	switch category {
	case InvalidArgument,
		Unauthenticated,
		PermissionDenied,
		NotFound,
		AlreadyExists,
		Conflict,
		FailedPrecondition,
		UnprocessableEntity,
		ResourceExhausted,
		DeadlineExceeded,
		Unimplemented,
		Unavailable,
		Internal:
		return true
	default:
		return false
	}
}

// Detail is a sanitized, field-specific explanation suitable for a delivery
// adapter. It must not contain internal causes or diagnostic data.
type Detail struct {
	Field  string
	Reason string
}

// AppError contains transport-independent application error semantics.
//
// Its fields are intentionally private so callers cannot mutate an error after
// construction. Accessors that return slices also return defensive copies.
type AppError struct {
	category  Category
	code      string
	message   string
	retryable bool
	details   []Detail
	cause     error
}

// Option configures an AppError.
type Option func(*AppError)

// New constructs an AppError. Validation that is specific to a delivery
// protocol belongs to that delivery adapter.
func New(category Category, code, message string, options ...Option) *AppError {
	appError := &AppError{
		category: category,
		code:     code,
		message:  message,
	}

	for _, option := range options {
		if option != nil {
			option(appError)
		}
	}

	appError.details = cloneDetails(appError.details)
	return appError
}

// WithRetryable records whether retrying the operation can be useful.
func WithRetryable(retryable bool) Option {
	return func(appError *AppError) {
		appError.retryable = retryable
	}
}

// WithDetails records sanitized field details. The supplied slice is copied
// both when this option is created and when it is applied.
func WithDetails(details ...Detail) Option {
	snapshot := cloneDetails(details)
	return func(appError *AppError) {
		appError.details = cloneDetails(snapshot)
	}
}

// WithCause preserves an internal cause for errors.Is/errors.As without making
// it part of the public delivery payload.
func WithCause(cause error) Option {
	return func(appError *AppError) {
		appError.cause = cause
	}
}

// Error returns only the sanitized public message. It never appends the
// internal cause.
func (appError *AppError) Error() string {
	if appError == nil || appError.message == "" {
		return "application error"
	}
	return appError.message
}

// Unwrap exposes the internal cause to the standard error chain.
func (appError *AppError) Unwrap() error {
	if appError == nil {
		return nil
	}
	return appError.cause
}

// Category returns the common application error category.
func (appError *AppError) Category() Category {
	if appError == nil {
		return ""
	}
	return appError.category
}

// Code returns the stable code owned by the calling module or delivery mapper.
func (appError *AppError) Code() string {
	if appError == nil {
		return ""
	}
	return appError.code
}

// Message returns the sanitized public message.
func (appError *AppError) Message() string {
	if appError == nil {
		return ""
	}
	return appError.message
}

// Retryable reports whether the operation can be retried.
func (appError *AppError) Retryable() bool {
	return appError != nil && appError.retryable
}

// Details returns a defensive copy of the sanitized field details.
func (appError *AppError) Details() []Detail {
	if appError == nil {
		return nil
	}
	return cloneDetails(appError.details)
}

// From finds the first AppError in err's chain.
func From(err error) (*AppError, bool) {
	var appError *AppError
	if !errors.As(err, &appError) || appError == nil {
		return nil, false
	}
	return appError, true
}

// IsCategory reports whether err's chain contains an AppError with category.
func IsCategory(err error, category Category) bool {
	appError, ok := From(err)
	return ok && appError.Category() == category
}

func cloneDetails(details []Detail) []Detail {
	if len(details) == 0 {
		return nil
	}

	clone := make([]Detail, len(details))
	copy(clone, details)
	return clone
}
