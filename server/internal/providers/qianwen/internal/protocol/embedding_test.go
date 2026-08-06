package protocol

import (
	"math"
	"testing"
)

func TestEmbeddingValidationRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	validRequest := EmbeddingRequest{
		Inputs:     []string{"Java backend interview"},
		Dimensions: 2,
	}
	validResult := EmbeddingResult{
		Provider:    "qianwen",
		Model:       "text-embedding-v4",
		Dimensions:  2,
		Vectors:     [][]float32{{1, 0.5}},
		InputTokens: 3,
		TotalTokens: 3,
	}
	if err := ValidateEmbeddingResult(validRequest, validResult); err != nil {
		t.Fatalf("valid result: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*EmbeddingResult)
	}{
		{
			name: "wrong dimensions",
			mutate: func(result *EmbeddingResult) {
				result.Vectors[0] = []float32{1}
			},
		},
		{
			name: "non finite",
			mutate: func(result *EmbeddingResult) {
				result.Vectors[0][0] = float32(math.Inf(1))
			},
		},
		{
			name: "zero vector",
			mutate: func(result *EmbeddingResult) {
				result.Vectors[0] = []float32{0, 0}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			result := validResult
			result.Vectors = [][]float32{append(
				[]float32(nil),
				validResult.Vectors[0]...,
			)}
			test.mutate(&result)
			if err := ValidateEmbeddingResult(validRequest, result); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
