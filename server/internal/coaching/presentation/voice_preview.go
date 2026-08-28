package presentation

import (
	"context"
	"errors"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const VoicePreviewText = "Hi, I’m your SpeakUp coach. Let’s practice English together."

var ErrVoicePreviewUnavailable = errors.New(
	"coach presentation: voice preview unavailable",
)

type VoicePreviewSynthesisRequest struct {
	Text            string
	Provider        string
	ProviderProfile string
	Model           string
	VoiceID         string
	Locale          string
}

type VoicePreviewSynthesisResult struct {
	Audio platformmedia.ManagedAudioSource
}

type VoicePreviewSynthesizer interface {
	SynthesizeVoicePreview(
		context.Context,
		VoicePreviewSynthesisRequest,
	) (VoicePreviewSynthesisResult, error)
}

type Option func(*Service) error

func WithVoicePreviewSynthesizer(synthesizer VoicePreviewSynthesizer) Option {
	return func(service *Service) error {
		if synthesizer == nil {
			return ErrRepository
		}
		service.voicePreviewSynthesizer = synthesizer
		return nil
	}
}

func (service *Service) CreateVoicePreview(
	ctx context.Context,
	actor requestcontext.Actor,
	voiceOptionID string,
) (platformmedia.ManagedAudioSource, error) {
	if service == nil || service.repository == nil || !actor.Valid() ||
		!validOptionID(voiceOptionID) {
		return nil, ErrInvalidRequest
	}
	if service.voicePreviewSynthesizer == nil {
		return nil, ErrVoicePreviewUnavailable
	}
	catalog, err := service.GetCatalog(ctx, actor)
	if err != nil {
		return nil, err
	}
	var selected *VoiceOption
	for index := range catalog.Voices {
		if catalog.Voices[index].ID == voiceOptionID {
			selected = &catalog.Voices[index]
			break
		}
	}
	if selected == nil {
		return nil, ErrNotFound
	}
	result, err := service.voicePreviewSynthesizer.SynthesizeVoicePreview(
		ctx,
		VoicePreviewSynthesisRequest{
			Text:            VoicePreviewText,
			Provider:        selected.Provider,
			ProviderProfile: selected.ProviderProfile,
			Model:           selected.ProviderModel,
			VoiceID:         selected.ProviderVoiceID,
			Locale:          selected.Locale,
		},
	)
	if err != nil {
		return nil, ErrVoicePreviewUnavailable
	}
	if result.Audio == nil ||
		result.Audio.MediaType() != platformmedia.ContentTypeWAV ||
		result.Audio.Size() <= 44 || result.Audio.Size() > platformmedia.MaxAudioBytes ||
		result.Audio.Duration() <= 0 {
		if result.Audio != nil {
			_ = result.Audio.Close()
		}
		return nil, ErrVoicePreviewUnavailable
	}
	return result.Audio, nil
}
