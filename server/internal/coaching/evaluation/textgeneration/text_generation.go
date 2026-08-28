package textgeneration

import (
	"context"
	"strings"
)

type ReportContract struct {
	DimensionKeys []string
	ScoreMaximum  float64
}

func (contract ReportContract) Valid() bool {
	if len(contract.DimensionKeys) == 0 || len(contract.DimensionKeys) > 8 ||
		(contract.ScoreMaximum != 9 && contract.ScoreMaximum != 100) {
		return false
	}
	seen := make(map[string]struct{}, len(contract.DimensionKeys))
	for _, key := range contract.DimensionKeys {
		if key == "" || key != strings.TrimSpace(key) {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

type Request struct {
	SystemPrompt string
	UserPrompt   string
	Report       ReportContract
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
