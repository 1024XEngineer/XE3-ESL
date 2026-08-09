package qianwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

type IELTSAnswerPreparationGenerator struct {
	generator *textClient
}

func NewIELTSAnswerPreparationGenerator(
	configuration TextConfig,
	apiKey string,
) (*IELTSAnswerPreparationGenerator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &IELTSAnswerPreparationGenerator{generator: generator}, nil
}

func (generator *IELTSAnswerPreparationGenerator) GenerateAnswerPreparation(
	ctx context.Context,
	request ielts.AnswerGenerationRequest,
) (ielts.AnswerGenerationResult, error) {
	if generator == nil {
		return ielts.AnswerGenerationResult{}, missingBusinessGenerator()
	}
	result, err := generateBusinessText(
		ctx,
		generator.generator,
		ielts.GenerationSystemPrompt(),
		ielts.GenerationUserPrompt(request),
		protocol.TextResponseFormatJSON,
	)
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
		return ielts.AnswerGenerationResult{}, invalidIELTSAnswerResponse(result.ID, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ielts.AnswerGenerationResult{}, invalidIELTSAnswerResponse(result.ID, errors.New("qianwen: IELTS answer response contains trailing data"))
	}
	return ielts.AnswerGenerationResult{
		RequestID:         result.ID,
		Provider:          result.Provider,
		Model:             result.Model,
		Answer:            payload.Answer,
		Outline:           payload.Outline,
		UsefulExpressions: payload.UsefulExpressions,
		SpeechText:        payload.SpeechText,
	}, nil
}

func invalidIELTSAnswerResponse(requestID string, cause error) error {
	return protocol.NewGenerationError(
		protocol.ErrorInvalidResponse,
		0,
		"",
		requestID,
		cause,
	)
}

var _ ielts.AnswerPreparationGenerator = (*IELTSAnswerPreparationGenerator)(nil)
