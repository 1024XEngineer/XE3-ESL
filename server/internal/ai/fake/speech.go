package fake

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

// SpeechRecognizer is an explicit deterministic provider for offline tests.
// Production assembly must select a real adapter.
type SpeechRecognizer struct {
	result ai.TranscriptionResult
	err    error
}

func NewSpeechRecognizer(result ai.TranscriptionResult) *SpeechRecognizer {
	return &SpeechRecognizer{result: result}
}

func NewFailingSpeechRecognizer(err error) *SpeechRecognizer {
	return &SpeechRecognizer{err: err}
}

func (recognizer *SpeechRecognizer) Transcribe(
	ctx context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	if err := speechContextError(ctx, ai.SpeechOperationTranscription); err != nil {
		return ai.TranscriptionResult{}, err
	}
	if err := ai.ValidateTranscriptionRequest(request); err != nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	if recognizer.err != nil {
		return ai.TranscriptionResult{}, recognizer.err
	}
	return recognizer.result, nil
}

// SpeechSynthesizer is an explicit deterministic provider for offline tests.
// The configured result owns any ManagedAudioSource it carries.
type SpeechSynthesizer struct {
	result ai.SynthesisResult
	err    error
}

func NewSpeechSynthesizer(result ai.SynthesisResult) *SpeechSynthesizer {
	return &SpeechSynthesizer{result: result}
}

func NewFailingSpeechSynthesizer(err error) *SpeechSynthesizer {
	return &SpeechSynthesizer{err: err}
}

func (synthesizer *SpeechSynthesizer) Synthesize(
	ctx context.Context,
	request ai.SynthesisRequest,
) (ai.SynthesisResult, error) {
	if err := speechContextError(ctx, ai.SpeechOperationSynthesis); err != nil {
		return ai.SynthesisResult{}, err
	}
	if err := ai.ValidateSynthesisRequest(request); err != nil {
		return ai.SynthesisResult{}, ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	if synthesizer.err != nil {
		return ai.SynthesisResult{}, synthesizer.err
	}
	return synthesizer.result, nil
}

func speechContextError(ctx context.Context, operation ai.SpeechOperation) error {
	if ctx == nil {
		return ai.NewSpeechError(
			operation,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			context.Canceled,
		)
	}
	if err := ctx.Err(); err != nil {
		kind := ai.ErrorCancelled
		if err == context.DeadlineExceeded {
			kind = ai.ErrorTimeout
		}
		return ai.NewSpeechError(operation, kind, 0, "", "", err)
	}
	return nil
}

var _ ai.SpeechRecognizer = (*SpeechRecognizer)(nil)
var _ ai.SpeechSynthesizer = (*SpeechSynthesizer)(nil)
