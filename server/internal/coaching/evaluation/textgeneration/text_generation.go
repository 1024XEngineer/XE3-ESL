package textgeneration

import "context"

type Request struct {
	SystemPrompt string
	UserPrompt   string
}

type Result struct {
	RequestID string
	Content   string
	Provider  string
	Model     string
}

type Generator interface {
	Generate(context.Context, Request) (Result, error)
}
