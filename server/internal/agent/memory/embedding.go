package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	maxEmbeddingInputBytes = 16 * 1024
	maxEmbeddingDimensions = 2048
)

// EmbeddingRequest is intentionally single-input because Memory never embeds
// unrelated records as one provider batch.
type EmbeddingRequest struct {
	Input      string
	Dimensions int
}

type EmbeddingResult struct {
	Provider    string
	Model       string
	Dimensions  int
	Vector      []float32
	InputTokens int
	TotalTokens int
}

type Embedder interface {
	Embed(context.Context, EmbeddingRequest) (EmbeddingResult, error)
}

func ValidateEmbeddingRequest(request EmbeddingRequest) error {
	if request.Input == "" || request.Input != strings.TrimSpace(request.Input) {
		return errors.New("memory embedding input is empty or untrimmed")
	}
	if len(request.Input) > maxEmbeddingInputBytes {
		return errors.New("memory embedding input exceeds the byte limit")
	}
	if request.Dimensions < 1 || request.Dimensions > maxEmbeddingDimensions {
		return fmt.Errorf(
			"memory embedding dimensions must be between 1 and %d",
			maxEmbeddingDimensions,
		)
	}
	return nil
}

func ValidateEmbeddingResult(
	expectedDimensions int,
	result EmbeddingResult,
) error {
	if expectedDimensions < 1 || expectedDimensions > maxEmbeddingDimensions {
		return errors.New("memory embedding expected dimensions are invalid")
	}
	if result.Provider == "" ||
		result.Model == "" ||
		result.Dimensions != expectedDimensions ||
		len(result.Vector) != result.Dimensions ||
		result.InputTokens < 0 ||
		result.TotalTokens < result.InputTokens {
		return errors.New("memory embedding result metadata is invalid")
	}
	nonZero := false
	for _, value := range result.Vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("memory embedding result contains a non-finite value")
		}
		nonZero = nonZero || value != 0
	}
	if !nonZero {
		return errors.New("memory embedding result contains a zero vector")
	}
	return nil
}
