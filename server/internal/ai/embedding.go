package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	MaxEmbeddingInputs     = 10
	MaxEmbeddingInputBytes = 16 * 1024
	MaxEmbeddingDimensions = 2048
)

// Embedder is the application-facing boundary for one bounded embedding
// batch. Implementations must preserve input order and must not retry calls
// implicitly.
type Embedder interface {
	Embed(context.Context, EmbeddingRequest) (EmbeddingResult, error)
}

type EmbeddingRequest struct {
	Inputs     []string
	Dimensions int
}

type EmbeddingResult struct {
	Provider    string
	Model       string
	Dimensions  int
	Vectors     [][]float32
	InputTokens int
	TotalTokens int
}

func ValidateEmbeddingRequest(request EmbeddingRequest) error {
	if len(request.Inputs) < 1 || len(request.Inputs) > MaxEmbeddingInputs {
		return fmt.Errorf(
			"embedding requires between 1 and %d inputs",
			MaxEmbeddingInputs,
		)
	}
	if request.Dimensions < 1 ||
		request.Dimensions > MaxEmbeddingDimensions {
		return fmt.Errorf(
			"embedding dimensions must be between 1 and %d",
			MaxEmbeddingDimensions,
		)
	}
	for index, input := range request.Inputs {
		if input == "" || input != strings.TrimSpace(input) {
			return fmt.Errorf("embedding input %d is empty or untrimmed", index)
		}
		if len(input) > MaxEmbeddingInputBytes {
			return fmt.Errorf("embedding input %d exceeds the byte limit", index)
		}
	}
	return nil
}

func ValidateEmbeddingResult(
	request EmbeddingRequest,
	result EmbeddingResult,
) error {
	if err := ValidateEmbeddingRequest(request); err != nil {
		return err
	}
	if result.Provider == "" ||
		result.Model == "" ||
		result.Dimensions != request.Dimensions ||
		len(result.Vectors) != len(request.Inputs) ||
		result.InputTokens < 0 ||
		result.TotalTokens < result.InputTokens {
		return errors.New("embedding result metadata is invalid")
	}
	for _, vector := range result.Vectors {
		if len(vector) != result.Dimensions {
			return errors.New("embedding result dimension is invalid")
		}
		nonZero := false
		for _, value := range vector {
			if math.IsNaN(float64(value)) ||
				math.IsInf(float64(value), 0) {
				return errors.New("embedding result contains a non-finite value")
			}
			nonZero = nonZero || value != 0
		}
		if !nonZero {
			return errors.New("embedding result contains a zero vector")
		}
	}
	return nil
}

// EmbeddingError exposes stable failure semantics without carrying provider
// payloads, user inputs, vectors, or credentials.
type EmbeddingError struct {
	Kind         ErrorKind
	StatusCode   int
	ProviderCode string
	RequestID    string
	cause        error
}

func NewEmbeddingError(
	kind ErrorKind,
	statusCode int,
	providerCode string,
	requestID string,
	cause error,
) *EmbeddingError {
	return &EmbeddingError{
		Kind:         kind,
		StatusCode:   statusCode,
		ProviderCode: providerCode,
		RequestID:    requestID,
		cause:        cause,
	}
}

func (e *EmbeddingError) Error() string {
	if e == nil {
		return "embedding failed"
	}
	return "embedding failed: " + string(e.Kind)
}

func (e *EmbeddingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *EmbeddingError) Retryable() bool {
	return e != nil && e.Kind.Retryable()
}

func (e *EmbeddingError) StableCategory() string {
	if e == nil {
		return ""
	}
	return string(e.Kind)
}
