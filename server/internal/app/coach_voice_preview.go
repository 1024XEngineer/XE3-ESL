package app

import (
	"context"

	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	coachpresentation "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/presentation"
)

type practiceVoicePreviewSynthesizer struct {
	synthesizer practiceinteraction.SpeechSynthesizer
}

func (adapter practiceVoicePreviewSynthesizer) SynthesizeVoicePreview(
	ctx context.Context,
	request coachpresentation.VoicePreviewSynthesisRequest,
) (coachpresentation.VoicePreviewSynthesisResult, error) {
	result, err := adapter.synthesizer.Synthesize(
		ctx,
		practiceinteraction.SynthesisRequest{
			Text: request.Text,
			Profile: practiceinteraction.SynthesisProfile{
				Provider:        request.Provider,
				ProviderProfile: request.ProviderProfile,
				Model:           request.Model,
				VoiceID:         request.VoiceID,
				Locale:          request.Locale,
			},
		},
	)
	if err != nil {
		return coachpresentation.VoicePreviewSynthesisResult{}, err
	}
	return coachpresentation.VoicePreviewSynthesisResult{Audio: result.Audio}, nil
}

var _ coachpresentation.VoicePreviewSynthesizer = practiceVoicePreviewSynthesizer{}
