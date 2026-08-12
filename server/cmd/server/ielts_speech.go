package main

import (
	"context"
	"errors"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

type ieltsSpeechSynthesizer struct {
	practice practicevoice.SpeechSynthesizer
}

func (synthesizer ieltsSpeechSynthesizer) Synthesize(ctx context.Context, text string) (platformmedia.ManagedAudioSource, error) {
	if synthesizer.practice == nil {
		return nil, errors.New("IELTS speech synthesizer is required")
	}
	result, err := synthesizer.practice.Synthesize(ctx, practicevoice.SynthesisRequest{Text: text})
	if err != nil {
		return nil, err
	}
	if result.Audio == nil {
		return nil, errors.New("IELTS speech synthesis returned no audio")
	}
	return result.Audio, nil
}
