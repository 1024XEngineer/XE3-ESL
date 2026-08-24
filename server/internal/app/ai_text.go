package app

import (
	"errors"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/ielts"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationagentcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/fieldextractor"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

// AgentModelProviders makes each Agent-owned model boundary explicit at the
// composition root. No business module receives a provider's generic client.
type AgentModelProviders struct {
	Run                agentrun.TextGenerator
	Summary            agentsummary.Generator
	Translation        sharedtranslation.Translator
	PracticeTurnIntent preparationagentcapability.PracticeTurnIntentGenerator
}

// ProviderFactory supplies one required service-level observer to every
// production provider adapter. Direct constructors remain available for
// deterministic tests and non-serving tools.
type ProviderFactory struct {
	observer providerobservability.Recorder
}

func NewProviderFactory(
	observer providerobservability.Recorder,
) (*ProviderFactory, error) {
	if observer == nil {
		return nil, errors.New("bootstrap: provider observer is required")
	}
	return &ProviderFactory{observer: observer}, nil
}

func NewAgentModelProviders(
	configuration config.TextGenerationConfig,
) (AgentModelProviders, error) {
	return newAgentModelProviders(configuration, nil)
}

func (factory *ProviderFactory) AgentModelProviders(
	configuration config.TextGenerationConfig,
) (AgentModelProviders, error) {
	return newAgentModelProviders(configuration, factory.observer)
}

func newAgentModelProviders(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (AgentModelProviders, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return AgentModelProviders{}, err
	}
	runGenerator, err := qianwen.NewAgentRunGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	summaryGenerator, err := qianwen.NewSummaryGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	translator, err := qianwen.NewTranslator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	practiceTurnIntent, err := qianwen.NewPracticeTurnIntentGenerator(
		providerConfig,
		apiKey,
	)
	if err != nil {
		return AgentModelProviders{}, err
	}
	return AgentModelProviders{
		Run:                runGenerator,
		Summary:            summaryGenerator,
		Translation:        translator,
		PracticeTurnIntent: practiceTurnIntent,
	}, nil
}

func NewPreparationJobTargetGenerator(
	configuration config.TextGenerationConfig,
) (preparation.JobTargetGenerator, error) {
	return newPreparationJobTargetGenerator(configuration, nil)
}

func (factory *ProviderFactory) PreparationJobTargetGenerator(
	configuration config.TextGenerationConfig,
) (preparation.JobTargetGenerator, error) {
	return newPreparationJobTargetGenerator(configuration, factory.observer)
}

func newPreparationJobTargetGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (preparation.JobTargetGenerator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	return qianwen.NewPreparationJobTargetGenerator(providerConfig, apiKey)
}

func NewIELTSAnswerGenerator(
	configuration config.TextGenerationConfig,
) (ielts.AnswerGenerator, error) {
	return newIELTSAnswerGenerator(configuration, nil)
}

func (factory *ProviderFactory) IELTSAnswerGenerator(
	configuration config.TextGenerationConfig,
) (ielts.AnswerGenerator, error) {
	return newIELTSAnswerGenerator(configuration, factory.observer)
}

func newIELTSAnswerGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (ielts.AnswerGenerator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	return qianwen.NewIELTSAnswerGenerator(providerConfig, apiKey)
}

func NewEvaluationScoringGenerator(
	configuration config.TextGenerationConfig,
) (textgeneration.Generator, error) {
	return newEvaluationScoringGenerator(configuration, nil)
}

func (factory *ProviderFactory) EvaluationScoringGenerator(
	configuration config.TextGenerationConfig,
) (textgeneration.Generator, error) {
	return newEvaluationScoringGenerator(configuration, factory.observer)
}

func newEvaluationScoringGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (textgeneration.Generator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	providerConfig.Model = configuration.EvaluationModel
	return qianwen.NewEvaluationScoringGenerator(providerConfig, apiKey)
}

func (factory *ProviderFactory) EvaluationProfileGenerator(
	configuration config.TextGenerationConfig,
) (textgeneration.Generator, error) {
	providerConfig, apiKey, err := textProvider(configuration, factory.observer)
	if err != nil {
		return nil, err
	}
	providerConfig.Model = configuration.EvaluationModel
	return qianwen.NewEvaluationProfileGenerator(providerConfig, apiKey)
}

func NewEvaluationSpeechFeedbackGenerator(
	configuration config.TextGenerationConfig,
) (speechfeedback.TextGenerator, error) {
	return newEvaluationSpeechFeedbackGenerator(configuration, nil)
}

func (factory *ProviderFactory) EvaluationSpeechFeedbackGenerator(
	configuration config.TextGenerationConfig,
) (speechfeedback.TextGenerator, error) {
	return newEvaluationSpeechFeedbackGenerator(configuration, factory.observer)
}

func newEvaluationSpeechFeedbackGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (speechfeedback.TextGenerator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	providerConfig.Model = configuration.SpeechFeedbackModel
	return qianwen.NewEvaluationSpeechFeedbackGenerator(providerConfig, apiKey)
}

func NewResumeFieldGenerator(
	configuration config.TextGenerationConfig,
) (fieldextractor.Generator, error) {
	return newResumeFieldGenerator(configuration, nil)
}

func (factory *ProviderFactory) ResumeFieldGenerator(
	configuration config.TextGenerationConfig,
) (fieldextractor.Generator, error) {
	return newResumeFieldGenerator(configuration, factory.observer)
}

func newResumeFieldGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (fieldextractor.Generator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	generator, err := qianwen.NewResumeFieldGenerator(providerConfig, apiKey)
	if err != nil {
		return nil, err
	}
	return generator, nil
}

// NewPracticeQuestionGenerator selects the Practice question generator.
func NewPracticeQuestionGenerator(
	configuration config.TextGenerationConfig,
) (practiceinteraction.QuestionGenerator, error) {
	return newPracticeQuestionGenerator(configuration, nil)
}

func (factory *ProviderFactory) PracticeQuestionGenerator(
	configuration config.TextGenerationConfig,
) (practiceinteraction.QuestionGenerator, error) {
	return newPracticeQuestionGenerator(configuration, factory.observer)
}

func newPracticeQuestionGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (practiceinteraction.QuestionGenerator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	return qianwen.NewPracticeQuestionGenerator(providerConfig, apiKey)
}

// NewPracticeAnswerTipGenerator selects the Practice answer-tip generator.
func NewPracticeAnswerTipGenerator(
	configuration config.TextGenerationConfig,
) (practiceinteraction.AnswerTipGenerator, error) {
	return newPracticeAnswerTipGenerator(configuration, nil)
}

func (factory *ProviderFactory) PracticeAnswerTipGenerator(
	configuration config.TextGenerationConfig,
) (practiceinteraction.AnswerTipGenerator, error) {
	return newPracticeAnswerTipGenerator(configuration, factory.observer)
}

func newPracticeAnswerTipGenerator(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (practiceinteraction.AnswerTipGenerator, error) {
	providerConfig, apiKey, err := textProvider(configuration, observer)
	if err != nil {
		return nil, err
	}
	return qianwen.NewPracticeAnswerTipGenerator(providerConfig, apiKey)
}

func textProvider(
	configuration config.TextGenerationConfig,
	observer providerobservability.Recorder,
) (qianwen.TextConfig, string, error) {
	if configuration.Provider != config.TextProviderQianwen &&
		configuration.Provider != config.TextProviderQiniu {
		return qianwen.TextConfig{}, "", errors.New(
			"bootstrap: text generation provider is not registered",
		)
	}
	return qianwen.TextConfig{
		Provider:        configuration.Provider,
		BaseURL:         configuration.BaseURL,
		Model:           configuration.Model,
		Timeout:         configuration.Timeout,
		MaxOutputTokens: configuration.MaxOutputTokens,
		Observer:        observer,
	}, configuration.APIKey.Reveal(), nil
}
