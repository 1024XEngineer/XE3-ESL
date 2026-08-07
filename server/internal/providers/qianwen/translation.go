package qianwen

import (
	"context"
	"errors"

	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

type Translator struct {
	generator *textClient
}

func NewTranslator(configuration TextConfig, apiKey string) (*Translator, error) {
	generator, err := newTextClient(configuration, apiKey)
	if err != nil {
		return nil, err
	}
	return &Translator{generator: generator}, nil
}

func (translator *Translator) Translate(
	ctx context.Context,
	request sharedtranslation.Request,
) (string, error) {
	if translator == nil || translator.generator == nil {
		return "", sharedtranslation.NewProviderError(
			sharedtranslation.ProviderErrorConfiguration,
			"",
			errors.New("qianwen: translator is required"),
		)
	}
	result, err := translator.generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{
			{
				Role:    protocol.TextRoleSystem,
				Content: "Translate the English text into natural Simplified Chinese. Preserve its meaning, tone, paragraph breaks, and emphasis. Return only the translation, with no quotation marks, markdown fences, answer, coaching, or explanation.",
			},
			{Role: protocol.TextRoleUser, Content: request.Text},
		},
	})
	if err != nil {
		return "", mapTranslationError(err)
	}
	return result.Content, nil
}

func mapTranslationError(err error) error {
	var generationError *protocol.GenerationError
	if !errors.As(err, &generationError) {
		return sharedtranslation.NewProviderError(
			sharedtranslation.ProviderErrorUnavailable, "", err,
		)
	}
	return sharedtranslation.NewProviderError(
		mapTranslationErrorKind(generationError.Kind),
		generationError.RequestID,
		err,
	)
}

func mapTranslationErrorKind(kind protocol.ErrorKind) sharedtranslation.ProviderErrorKind {
	switch kind {
	case protocol.ErrorInvalidRequest:
		return sharedtranslation.ProviderErrorInvalidRequest
	case protocol.ErrorConfiguration:
		return sharedtranslation.ProviderErrorConfiguration
	case protocol.ErrorAuthentication:
		return sharedtranslation.ProviderErrorAuthentication
	case protocol.ErrorAuthorization:
		return sharedtranslation.ProviderErrorAuthorization
	case protocol.ErrorQuotaExhausted:
		return sharedtranslation.ProviderErrorQuotaExhausted
	case protocol.ErrorRateLimited:
		return sharedtranslation.ProviderErrorRateLimited
	case protocol.ErrorTimeout:
		return sharedtranslation.ProviderErrorTimeout
	case protocol.ErrorInvalidResponse:
		return sharedtranslation.ProviderErrorInvalidResponse
	case protocol.ErrorCancelled:
		return sharedtranslation.ProviderErrorCancelled
	default:
		return sharedtranslation.ProviderErrorUnavailable
	}
}

var _ sharedtranslation.Translator = (*Translator)(nil)
