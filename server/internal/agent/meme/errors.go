package meme

import "errors"

var (
	ErrInvalidRequest = errors.New("agent meme: invalid request")
	ErrNotFound       = errors.New("agent meme: not found")
	ErrRepository     = errors.New("agent meme: repository failure")
)
