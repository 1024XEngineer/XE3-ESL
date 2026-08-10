package bootstrap

import (
	"errors"

	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agenttitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/qiniu"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
	sharedtranslation "github.com/1024XEngineer/XE3-ESL/server/internal/translation"
)

// AgentModelProviders makes each Agent-owned model boundary explicit at the
// composition root. No business module receives a provider's generic client.
type AgentModelProviders struct {
	Run         agentrun.TextGenerator
	Memory      memory.Generator
	Summary     agentsummary.Generator
	Title       agenttitle.Generator
	Translation sharedtranslation.Translator
}

func NewAgentModelProviders(
	configuration config.TextGenerationConfig,
) (AgentModelProviders, error) {
	switch configuration.Provider {
	case config.TextProviderQianwen:
		return newQianwenAgentModelProviders(configuration)
	case config.TextProviderQiniu:
		return newQiniuAgentModelProviders(configuration)
	default:
		return AgentModelProviders{}, errors.New(
			"bootstrap: text generation provider is not registered",
		)
	}
}

func newQianwenAgentModelProviders(
	configuration config.TextGenerationConfig,
) (AgentModelProviders, error) {
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return AgentModelProviders{}, err
	}
	runGenerator, err := qianwen.NewAgentRunGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	memoryGenerator, err := qianwen.NewMemoryGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	summaryGenerator, err := qianwen.NewSummaryGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	titleGenerator, err := qianwen.NewTitleGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	translator, err := qianwen.NewTranslator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	return AgentModelProviders{
		Run:         runGenerator,
		Memory:      memoryGenerator,
		Summary:     summaryGenerator,
		Title:       titleGenerator,
		Translation: translator,
	}, nil
}

func newQiniuAgentModelProviders(
	configuration config.TextGenerationConfig,
) (AgentModelProviders, error) {
	providerConfig, apiKey, err := qiniuTextProvider(configuration)
	if err != nil {
		return AgentModelProviders{}, err
	}
	runGenerator, err := qiniu.NewAgentRunGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	memoryGenerator, err := qiniu.NewMemoryGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	summaryGenerator, err := qiniu.NewSummaryGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	titleGenerator, err := qiniu.NewTitleGenerator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	translator, err := qiniu.NewTranslator(providerConfig, apiKey)
	if err != nil {
		return AgentModelProviders{}, err
	}
	return AgentModelProviders{
		Run: runGenerator, Memory: memoryGenerator, Summary: summaryGenerator,
		Title: titleGenerator, Translation: translator,
	}, nil
}

func NewPreparationJobTargetGenerator(
	configuration config.TextGenerationConfig,
) (preparation.JobTargetGenerator, error) {
	if configuration.Provider == config.TextProviderQiniu {
		providerConfig, apiKey, err := qiniuTextProvider(configuration)
		if err != nil {
			return nil, err
		}
		return qiniu.NewPreparationJobTargetGenerator(providerConfig, apiKey)
	}
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return nil, err
	}
	return qianwen.NewPreparationJobTargetGenerator(providerConfig, apiKey)
}

func NewIELTSAnswerPreparationGenerator(
	configuration config.TextGenerationConfig,
) (ielts.AnswerPreparationGenerator, error) {
	if configuration.Provider == config.TextProviderQiniu {
		providerConfig, apiKey, err := qiniuTextProvider(configuration)
		if err != nil {
			return nil, err
		}
		return qiniu.NewIELTSAnswerPreparationGenerator(providerConfig, apiKey)
	}
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return nil, err
	}
	return qianwen.NewIELTSAnswerPreparationGenerator(providerConfig, apiKey)
}

func NewEvaluationScoringGenerator(
	configuration config.TextGenerationConfig,
) (scoring.TextGenerator, error) {
	if configuration.Provider == config.TextProviderQiniu {
		providerConfig, apiKey, err := qiniuTextProvider(configuration)
		if err != nil {
			return nil, err
		}
		providerConfig.Model = configuration.EvaluationModel
		return qiniu.NewEvaluationScoringGenerator(providerConfig, apiKey)
	}
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return nil, err
	}
	providerConfig.Model = configuration.EvaluationModel
	return qianwen.NewEvaluationScoringGenerator(providerConfig, apiKey)
}

func NewEvaluationSpeechFeedbackGenerator(
	configuration config.TextGenerationConfig,
) (speechfeedback.TextGenerator, error) {
	if configuration.Provider == config.TextProviderQiniu {
		providerConfig, apiKey, err := qiniuTextProvider(configuration)
		if err != nil {
			return nil, err
		}
		providerConfig.Model = configuration.SpeechFeedbackModel
		return qiniu.NewEvaluationSpeechFeedbackGenerator(providerConfig, apiKey)
	}
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return nil, err
	}
	providerConfig.Model = configuration.SpeechFeedbackModel
	return qianwen.NewEvaluationSpeechFeedbackGenerator(providerConfig, apiKey)
}

func NewResumeFieldGenerator(
	configuration config.TextGenerationConfig,
) (fieldextractor.Generator, error) {
	if configuration.Provider == config.TextProviderQiniu {
		providerConfig, apiKey, err := qiniuTextProvider(configuration)
		if err != nil {
			return nil, err
		}
		return qiniu.NewResumeFieldGenerator(providerConfig, apiKey)
	}
	providerConfig, apiKey, err := qianwenTextProvider(configuration)
	if err != nil {
		return nil, err
	}
	generator, err := qianwen.NewResumeFieldGenerator(providerConfig, apiKey)
	if err != nil {
		return nil, err
	}
	return generator, nil
}

func qiniuTextProvider(
	configuration config.TextGenerationConfig,
) (qiniu.TextConfig, string, error) {
	if configuration.Provider != config.TextProviderQiniu {
		return qiniu.TextConfig{}, "", errors.New(
			"bootstrap: text generation provider is not registered",
		)
	}
	return qiniu.TextConfig{
		Provider:        qiniu.TextProviderName,
		BaseURL:         configuration.BaseURL,
		Model:           configuration.Model,
		Timeout:         configuration.Timeout,
		MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal(), nil
}

func qianwenTextProvider(
	configuration config.TextGenerationConfig,
) (qianwen.TextConfig, string, error) {
	if configuration.Provider != config.TextProviderQianwen {
		return qianwen.TextConfig{}, "", errors.New(
			"bootstrap: text generation provider is not registered",
		)
	}
	return qianwen.TextConfig{
		BaseURL:         configuration.BaseURL,
		Model:           configuration.Model,
		Timeout:         configuration.Timeout,
		MaxOutputTokens: configuration.MaxOutputTokens,
	}, configuration.APIKey.Reveal(), nil
}
