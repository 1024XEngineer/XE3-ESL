package app

import (
	"errors"
	"time"

	agentconversation "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	practiceinteractionpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction/postgres"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeAudioConfiguration groups three independently owned composition
// inputs without merging their Ports or lifecycle rules.
type RuntimeAudioConfiguration struct {
	AgentVoice          AgentVoiceConfiguration
	PracticeInteraction PracticeInteractionConfiguration
	Media               AudioMediaConfiguration
}

type AgentVoiceConfiguration struct {
	Recognizer             agentvoice.StreamingSpeechRecognizer
	Synthesizer            agentvoice.SpeechSynthesizer
	AssistantSpeech        agentconversation.AssistantSpeechSynthesizer
	MessageFeedback        *evaluation.AgentMessageFeedbackScheduler
	InputEnabled           bool
	ScratchDirectory       string
	ObjectReadAllowedHosts []string
	ReadTimeout            time.Duration
	StagedTTL              time.Duration
	ASRLease               time.Duration
}

type PracticeInteractionConfiguration struct {
	Evaluation          PracticeEvaluationSchedulers
	Recognizer          practiceinteraction.SpeechRecognizer
	RecordedRecognizer  practiceinteraction.SpeechRecognizer
	Synthesizer         practiceinteraction.SpeechSynthesizer
	QuestionGenerator   practiceinteraction.QuestionGenerator
	QuestionTranslator  sharedtranslation.Translator
	AnswerTipGenerator  practiceinteraction.AnswerTipGenerator
	TemporaryAudio      practiceinteraction.TemporaryAudioVault
	AudioStagedTTL      time.Duration
	ASRLease            time.Duration
	RealtimeReadTimeout time.Duration
	RecordedReadTimeout time.Duration
}

type AudioMediaConfiguration struct {
	ObjectStore objectstore.Store
	UploadLease time.Duration
}

type AgentImageConfiguration struct {
	ObjectStore objectstore.Store
	StagedTTL   time.Duration
	UploadLease time.Duration
}

// InterviewResumeConfiguration supplies the temporary PDF boundary owned by
// InterviewPreparation.
type InterviewResumeConfiguration struct {
	ObjectStore sharedmedia.DocumentStore
	Parser      interviewresume.Parser
	UploadLease time.Duration
}

// buildPracticeInteractionApplication constructs infrastructure and delegates
// Practice Interaction business wiring to the owning package.
func buildPracticeInteractionApplication(
	database *pgxpool.Pool,
	configuration PracticeInteractionConfiguration,
	completion practicepostgres.CompletionScheduler,
	turnFeedback practicepostgres.TurnFeedbackScheduler,
	profile practicepostgres.IELTSProfileScheduler,
	feedbackReader practiceinteraction.TurnFeedbackStatusReader,
	ids practice.PracticeResourceIDGenerator,
	mediaService *sharedmedia.Service,
	recordingMediaEnabled bool,
) (
	*practiceinteraction.SessionApplication,
	*practiceinteraction.SameQuestionRetryApplication,
	*practiceinteraction.RecordingService,
	error,
) {
	if database == nil ||
		configuration.Recognizer == nil ||
		configuration.RecordedRecognizer == nil ||
		configuration.Synthesizer == nil ||
		configuration.QuestionGenerator == nil ||
		configuration.TemporaryAudio == nil ||
		configuration.ASRLease <= 0 || completion == nil ||
		turnFeedback == nil || profile == nil || feedbackReader == nil || ids == nil {
		return nil, nil, nil,
			errors.New("bootstrap: Practice Interaction dependencies are required")
	}

	repository, err := practiceinteractionpostgres.New(
		database,
		completion,
		turnFeedback,
		profile,
		ids,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	var recordings *practiceinteraction.RecordingService
	if recordingMediaEnabled {
		if mediaService == nil {
			return nil, nil, nil,
				errors.New("bootstrap: Practice recording media is required")
		}
		recordings, err = practiceinteraction.NewRecordingService(
			mediaService,
			repository,
			configuration.AudioStagedTTL,
		)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	application, retryApplication, err :=
		practiceinteraction.NewRuntimeApplications(
			practiceinteraction.RuntimeConfiguration{
				Repository:         repository,
				IDs:                ids,
				TemporaryAudio:     configuration.TemporaryAudio,
				Recognizer:         configuration.Recognizer,
				RecordedRecognizer: configuration.RecordedRecognizer,
				Synthesizer:        configuration.Synthesizer,
				QuestionGenerator:  configuration.QuestionGenerator,
				QuestionTranslator: configuration.QuestionTranslator,
				AnswerTipGenerator: configuration.AnswerTipGenerator,
				Recordings:         recordings,
				ASRLease:           configuration.ASRLease,
				FeedbackReader:     feedbackReader,
			},
		)
	if err != nil {
		return nil, nil, nil, err
	}
	return application, retryApplication, recordings, nil
}
