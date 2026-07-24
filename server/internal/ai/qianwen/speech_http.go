package qianwen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

func decodeSpeechStatusError(
	operation ai.SpeechOperation,
	response *http.Response,
) error {
	body, readErr := readBounded(response.Body, maxErrorResponseBytes)
	code := ""
	requestID := sanitizeIdentifier(response.Header.Get("X-Request-Id"))
	if readErr == nil {
		var envelope errorEnvelope
		if json.Unmarshal(body, &envelope) == nil {
			code = rawIdentifier(envelope.Code)
			if envelope.Error != nil {
				if nestedCode := rawIdentifier(envelope.Error.Code); nestedCode != "" {
					code = nestedCode
				} else if nestedType := sanitizeIdentifier(envelope.Error.Type); nestedType != "" {
					code = nestedType
				}
			}
			if bodyRequestID := sanitizeIdentifier(envelope.RequestID); bodyRequestID != "" {
				requestID = bodyRequestID
			}
		}
	}
	return ai.NewSpeechError(
		operation,
		classifyStatus(response.StatusCode, code),
		response.StatusCode,
		code,
		requestID,
		readErr,
	)
}

func speechTransportError(
	operation ai.SpeechOperation,
	ctx context.Context,
	cause error,
) error {
	kind := ai.ErrorProviderUnavailable
	var safeCause error
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		kind = ai.ErrorCancelled
		safeCause = context.Canceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded),
		errors.Is(cause, context.DeadlineExceeded):
		kind = ai.ErrorTimeout
		safeCause = context.DeadlineExceeded
	}
	return ai.NewSpeechError(operation, kind, 0, "", "", safeCause)
}
