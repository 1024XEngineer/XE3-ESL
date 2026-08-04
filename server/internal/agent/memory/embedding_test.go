package memory

import (
	"math"
	"strings"
	"testing"
)

func TestValidateEmbeddingResultRejectsUnsafeVectors(t *testing.T) {
	t.Parallel()

	request := EmbeddingRequest{Input: "memory", Dimensions: 2}
	valid := EmbeddingResult{
		Provider:    "qianwen",
		Model:       "text-embedding-v4",
		Dimensions:  2,
		Vector:      []float32{1, 0},
		InputTokens: 1,
		TotalTokens: 1,
	}
	if err := ValidateEmbeddingResult(request.Dimensions, valid); err != nil {
		t.Fatalf("valid result: %v", err)
	}

	tests := map[string]EmbeddingResult{
		"wrong shape": func() EmbeddingResult {
			result := valid
			result.Vector = []float32{1}
			return result
		}(),
		"zero vector": func() EmbeddingResult {
			result := valid
			result.Vector = []float32{0, 0}
			return result
		}(),
		"non-finite": func() EmbeddingResult {
			result := valid
			result.Vector = []float32{float32(math.Inf(1)), 0}
			return result
		}(),
	}
	for name, result := range tests {
		result := result
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateEmbeddingResult(request.Dimensions, result); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestValidateEmbeddingRequestEnforcesMemoryInputBoundary(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]EmbeddingRequest{
		"blank":      {Input: "", Dimensions: MemoryEmbeddingDimensions},
		"untrimmed":  {Input: " memory ", Dimensions: MemoryEmbeddingDimensions},
		"oversized":  {Input: strings.Repeat("x", maxEmbeddingInputBytes+1), Dimensions: MemoryEmbeddingDimensions},
		"dimensions": {Input: "memory", Dimensions: maxEmbeddingDimensions + 1},
	} {
		request := request
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateEmbeddingRequest(request); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
