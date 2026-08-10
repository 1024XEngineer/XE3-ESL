// Package qiniu adapts Qiniu AI services to the application's provider ports.
package qiniu

import "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen"

const TextProviderName = "qiniu"

// Qiniu's Chat Completions endpoint is OpenAI-compatible. The established
// Qianwen text adapters own the provider-neutral business mappings, while the
// TextConfig provider selector activates Qiniu-specific endpoint, model, wire,
// lineage, and error behavior in the shared audited compatibility client.
type TextConfig = qianwen.TextConfig

type AgentRunGenerator = qianwen.AgentRunGenerator
type MemoryGenerator = qianwen.MemoryGenerator
type SummaryGenerator = qianwen.SummaryGenerator
type TitleGenerator = qianwen.TitleGenerator
type Translator = qianwen.Translator
type PreparationJobTargetGenerator = qianwen.PreparationJobTargetGenerator
type IELTSAnswerPreparationGenerator = qianwen.IELTSAnswerPreparationGenerator
type EvaluationScoringGenerator = qianwen.EvaluationScoringGenerator
type EvaluationSpeechFeedbackGenerator = qianwen.EvaluationSpeechFeedbackGenerator
type ResumeFieldGenerator = qianwen.ResumeFieldGenerator
type PracticeVoiceQuestionGenerator = qianwen.PracticeVoiceQuestionGenerator
type PracticeVoiceAnswerTipGenerator = qianwen.PracticeVoiceAnswerTipGenerator

func NewAgentRunGenerator(
	configuration TextConfig,
	apiKey string,
) (*AgentRunGenerator, error) {
	return qianwen.NewAgentRunGenerator(qiniuTextConfig(configuration), apiKey)
}

func NewMemoryGenerator(
	configuration TextConfig,
	apiKey string,
) (*MemoryGenerator, error) {
	return qianwen.NewMemoryGenerator(qiniuTextConfig(configuration), apiKey)
}

func NewSummaryGenerator(
	configuration TextConfig,
	apiKey string,
) (*SummaryGenerator, error) {
	return qianwen.NewSummaryGenerator(qiniuTextConfig(configuration), apiKey)
}

func NewTitleGenerator(
	configuration TextConfig,
	apiKey string,
) (*TitleGenerator, error) {
	return qianwen.NewTitleGenerator(qiniuTextConfig(configuration), apiKey)
}

func NewTranslator(
	configuration TextConfig,
	apiKey string,
) (*Translator, error) {
	return qianwen.NewTranslator(qiniuTextConfig(configuration), apiKey)
}

func NewPreparationJobTargetGenerator(
	configuration TextConfig,
	apiKey string,
) (*PreparationJobTargetGenerator, error) {
	return qianwen.NewPreparationJobTargetGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func NewIELTSAnswerPreparationGenerator(
	configuration TextConfig,
	apiKey string,
) (*IELTSAnswerPreparationGenerator, error) {
	return qianwen.NewIELTSAnswerPreparationGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func NewEvaluationScoringGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationScoringGenerator, error) {
	return qianwen.NewEvaluationScoringGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func NewEvaluationSpeechFeedbackGenerator(
	configuration TextConfig,
	apiKey string,
) (*EvaluationSpeechFeedbackGenerator, error) {
	return qianwen.NewEvaluationSpeechFeedbackGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func NewResumeFieldGenerator(
	configuration TextConfig,
	apiKey string,
) (*ResumeFieldGenerator, error) {
	return qianwen.NewResumeFieldGenerator(qiniuTextConfig(configuration), apiKey)
}

func NewPracticeVoiceQuestionGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeVoiceQuestionGenerator, error) {
	return qianwen.NewPracticeVoiceQuestionGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func NewPracticeVoiceAnswerTipGenerator(
	configuration TextConfig,
	apiKey string,
) (*PracticeVoiceAnswerTipGenerator, error) {
	return qianwen.NewPracticeVoiceAnswerTipGenerator(
		qiniuTextConfig(configuration),
		apiKey,
	)
}

func qiniuTextConfig(configuration TextConfig) TextConfig {
	configuration.Provider = TextProviderName
	return configuration
}
