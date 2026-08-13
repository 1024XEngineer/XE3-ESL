package agentcapability

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
)

const IELTSWarmUpToolName = "ielts.warmup.v1"

type IELTSWarmUpInput struct {
	PracticeMode string `json:"ielts_practice_mode"`
	TopicChoice  string `json:"ielts_topic_choice"`
}

type IELTSWarmUpTool struct{}

func NewIELTSWarmUpTool() IELTSWarmUpTool {
	return IELTSWarmUpTool{}
}

func (tool IELTSWarmUpTool) Definition() capability.Definition {
	return capability.Definition{
		Name:        IELTSWarmUpToolName,
		Description: "Prepare one unscored IELTS Speaking warm-up using only the learner's broad topic choice, never a formal question-bank topic or question. Use only after the user has chosen Part 1, Part 2, or Part 3 and a random or category topic. The returned prompt is already one complete, natural Chinese paragraph. Reproduce the returned prompt verbatim as the entire user-facing reply. Do not add an acknowledgement, transition, heading, taxonomy label, second paragraph, tutorial list, answer template, sentence starter, score, critique, control instructions, or practice-status narration. After returning the warm-up, stop this turn and wait for the learner's answer; do not create a PracticePlan in the same turn. This never reads formal test content, creates a PracticePlan or PracticeSession, scores the learner, or reveals internal values or ids. Do not use for a full mock.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"ielts_practice_mode": capability.StringEnumSchema(
				"IELTS Speaking specialty Part selected by the user.",
				"PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"Complete Chinese topic choice mapped to its internal value: 随机=random, 人物=person, 地点=place, 事物=thing, 经历=experience. These terms are complete selections: do not ask another question or generate a warm-up yourself; call this tool.",
				"random", "person", "place", "thing", "experience",
			),
		}, []string{"ielts_practice_mode", "ielts_topic_choice"}),
		ReadOnly: true,
		Risk:     capability.RiskReadOnly,
	}
}

func (tool IELTSWarmUpTool) Execute(
	ctx context.Context,
	_ capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	if ctx == nil {
		return capability.Result{}, capability.ErrExecutionRejected
	}
	parsed, _, err := parseIELTSWarmUpInput(input)
	if err != nil {
		return capability.Result{}, err
	}
	prompt := warmUpPrompt(parsed.TopicChoice)
	content := map[string]any{
		"prompt": prompt,
	}
	return capability.Result{Content: content}, nil
}

func parseIELTSWarmUpInput(
	input json.RawMessage,
) (IELTSWarmUpInput, ielts.PracticeMode, error) {
	var parsed IELTSWarmUpInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return IELTSWarmUpInput{}, "", capability.ErrInvalidInput
	}
	mode := ielts.PracticeMode(parsed.PracticeMode)
	if mode != ielts.PracticeModePart1 && mode != ielts.PracticeModePart2 &&
		mode != ielts.PracticeModePart3 {
		return IELTSWarmUpInput{}, "", capability.ErrInvalidInput
	}
	if !isWarmUpTopicChoice(parsed.TopicChoice) {
		return IELTSWarmUpInput{}, "", capability.ErrInvalidInput
	}
	return parsed, mode, nil
}

func isWarmUpTopicChoice(choice string) bool {
	switch choice {
	case "random", "person", "place", "thing", "experience":
		return true
	default:
		return false
	}
}

func warmUpPrompt(choice string) string {
	switch choice {
	case "person":
		return "可以。最近有没有谁让你印象挺深？用一两句英语说说。"
	case "place":
		return "可以。最近有没有哪个地方让你印象挺深？用一两句英语说说。"
	case "thing":
		return "可以。最近有没有什么东西让你印象挺深？用一两句英语说说。"
	case "experience":
		return "可以。最近有没有哪次经历让你印象挺深？用一两句英语说说。"
	default:
		return "那就随意聊聊：最近有什么人、地方、事物或经历让你印象挺深？挑一个，用一两句英语说说。"
	}
}
