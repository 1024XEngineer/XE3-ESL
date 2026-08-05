// Package ocr defines the supplier-neutral PDF OCR boundary used by Resume.
package ocr

import "context"

const (
	FailureTimeout         = "ocr_timeout"
	FailureProvider        = "ocr_provider_failed"
	FailureOutputInvalid   = "ocr_output_invalid"
	FailurePageLimit       = "ocr_page_limit_exceeded"
	MaximumRecognizedPages = 5
)

// Client recognizes a remotely readable PDF without exposing supplier DTOs.
type Client interface {
	RecognizePDF(context.Context, string) (Result, error)
}

// Result contains ordered OCR pages.
type Result struct {
	PageCount int
	Pages     []Page
}

// Page contains the recognized words for one PDF page.
type Page struct {
	Number int
	Words  []Word
}

// Word contains text and its provider-reported position.
type Word struct {
	Text   string
	X      int64
	Y      int64
	Width  int64
	Height int64
}

// Failure exposes only a stable, non-sensitive worker failure code.
type Failure struct {
	code string
}

// NewFailure creates a sanitized OCR failure.
func NewFailure(code string) *Failure {
	switch code {
	case FailureTimeout, FailureProvider, FailureOutputInvalid, FailurePageLimit:
		return &Failure{code: code}
	default:
		return &Failure{code: FailureProvider}
	}
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "resume OCR failed"
	}
	return "resume OCR failed: " + failure.FailureCode()
}

// FailureCode returns a stable code without provider details.
func (failure *Failure) FailureCode() string {
	if failure == nil || failure.code == "" {
		return FailureProvider
	}
	return failure.code
}
