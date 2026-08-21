package ielts

import (
	"context"
	"errors"
	"strings"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

var (
	ErrSpeechInvalid     = errors.New("ielts: invalid speech request")
	ErrSpeechNotFound    = errors.New("ielts: speech resource not found")
	ErrSpeechUnavailable = errors.New("ielts: speech synthesis unavailable")
)

type SpeechSynthesizer interface {
	Synthesize(context.Context, string) (platformmedia.ManagedAudioSource, error)
}

type SpeechService struct {
	questions QuestionResolver
	speech    SpeechSynthesizer
}

func NewSpeechService(questions QuestionResolver, speech SpeechSynthesizer) (*SpeechService, error) {
	if questions == nil || speech == nil {
		return nil, ErrSpeechInvalid
	}
	return &SpeechService{questions: questions, speech: speech}, nil
}

func (service *SpeechService) Question(ctx context.Context, actor requestcontext.Actor, reference QuestionReference) (platformmedia.ManagedAudioSource, error) {
	if ctx == nil || !actor.Valid() || !validQuestionReference(reference) {
		return nil, ErrSpeechInvalid
	}
	question, err := service.questions.ResolveQuestion(ctx, reference)
	if err != nil {
		if errors.Is(err, ErrQuestionSetNotFound) {
			return nil, ErrSpeechNotFound
		}
		return nil, err
	}
	return service.synthesize(ctx, question.Prompt)
}

func (service *SpeechService) synthesize(ctx context.Context, text string) (platformmedia.ManagedAudioSource, error) {
	audio, err := service.speech.Synthesize(ctx, strings.TrimSpace(text))
	if err != nil || audio == nil || platformmedia.ValidateAudioSource(audio) != nil {
		if audio != nil {
			_ = audio.Close()
		}
		return nil, ErrSpeechUnavailable
	}
	return audio, nil
}
