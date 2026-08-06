package model

import "errors"

var (
	ErrInvalidContext      = errors.New("preparation: invalid context")
	ErrUnsupportedKind     = errors.New("preparation: unsupported kind")
	ErrContextTypeMismatch = errors.New("preparation: context type mismatch")
)
