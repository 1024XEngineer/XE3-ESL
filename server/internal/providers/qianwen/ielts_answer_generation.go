package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type IELTSAnswerGenerator struct{ generator *textClient }

func NewIELTSAnswerGenerator(configuration TextConfig, apiKey string) (*IELTSAnswerGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &IELTSAnswerGenerator{generator: generator}, nil
}

func (generator *IELTSAnswerGenerator) GenerateIELTSAnswer(ctx context.Context, request ielts.AnswerGenerationInput) (ielts.AnswerGenerationResult, error) {
	result, err := generateBusinessText(ctx, generator.generator,
		ielts.AnswerGenerationSystemPrompt(), ielts.AnswerGenerationUserPrompt(request),
		&protocol.JSONSchemaDefinition{
			Name:   "ielts_answer",
			Strict: true,
			Schema: ieltsAnswerSchema(),
		})
	if err != nil {
		return ielts.AnswerGenerationResult{}, err
	}
	var payload struct {
		Answer            string   `json:"answer"`
		Outline           []string `json:"outline"`
		UsefulExpressions []string `json:"useful_expressions"`
		SpeechText        string   `json:"speech_text"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(result.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return ielts.AnswerGenerationResult{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ielts.AnswerGenerationResult{}, errors.New("qianwen: IELTS answer contains trailing data")
	}
	return ielts.AnswerGenerationResult{
		RequestID: result.ID, Provider: result.Provider, Model: result.Model,
		Answer: payload.Answer, Outline: payload.Outline,
		UsefulExpressions: payload.UsefulExpressions, SpeechText: payload.SpeechText,
	}, nil
}

func ieltsAnswerSchema() map[string]any {
	return strictObjectSchema(
		[]any{"answer", "outline", "useful_expressions", "speech_text"},
		map[string]any{
			"answer":             stringSchema(),
			"outline":            stringArraySchema(12),
			"useful_expressions": stringArraySchema(16),
			"speech_text":        stringSchema(),
		},
	)
}

var _ ielts.AnswerGenerator = (*IELTSAnswerGenerator)(nil)
