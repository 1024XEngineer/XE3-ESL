package agentcapability

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
)

func TestIELTSWarmUpDefinitionIsReadOnlyAndExposesNoIdentifiers(t *testing.T) {
	definition := NewIELTSWarmUpTool().Definition()
	if definition.Name != IELTSWarmUpToolName || !definition.ReadOnly ||
		definition.Risk != capability.RiskReadOnly {
		t.Fatalf("definition = %#v", definition)
	}
	for _, instruction := range []string{
		"using only the learner's broad topic choice",
		"never a formal question-bank topic or question",
		"one complete, natural Chinese paragraph",
		"Reproduce the returned prompt verbatim",
		"entire user-facing reply",
		"acknowledgement, transition",
		"taxonomy label, second paragraph",
		"answer template, sentence starter",
		"control instructions",
		"practice-status narration",
		"stop this turn",
		"wait for the learner's answer",
		"do not create a PracticePlan in the same turn",
		"never reads formal test content",
	} {
		if !strings.Contains(definition.Description, instruction) {
			t.Fatalf("definition is missing %q", instruction)
		}
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	if len(properties) != 2 || properties["ielts_practice_mode"] == nil ||
		properties["ielts_topic_choice"] == nil {
		t.Fatalf("input properties = %#v", properties)
	}
	topicChoice := properties["ielts_topic_choice"].(map[string]any)
	for _, mapping := range []string{
		"随机=random",
		"人物=person",
		"地点=place",
		"事物=thing",
		"经历=experience",
	} {
		if !strings.Contains(topicChoice["description"].(string), mapping) {
			t.Fatalf("topic choice description is missing %q: %#v", mapping, topicChoice)
		}
	}
	for _, instruction := range []string{
		"complete selections",
		"do not ask another question",
		"do not",
		"generate a warm-up yourself",
		"call this tool",
	} {
		if !strings.Contains(topicChoice["description"].(string), instruction) {
			t.Fatalf(
				"topic choice description is missing %q: %#v",
				instruction,
				topicChoice,
			)
		}
	}
	for name := range properties {
		if strings.Contains(name, "id") {
			t.Fatalf("definition exposes identifier field %q", name)
		}
	}
	if required := definition.InputSchema["required"].([]string); !reflect.DeepEqual(
		required,
		[]string{"ielts_practice_mode", "ielts_topic_choice"},
	) {
		t.Fatalf("required = %#v", required)
	}
}

func TestIELTSWarmUpInvocationIsReadOnly(t *testing.T) {
	registry, err := capability.NewRegistry(
		NewIELTSWarmUpTool(),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	effect := registry.InvocationEffect(capability.Invocation{
		Name: IELTSWarmUpToolName,
		Input: json.RawMessage(
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
		),
	})
	if effect != capability.InvocationEffectReadOnly {
		t.Fatalf("InvocationEffect = %v", effect)
	}
}

func TestIELTSWarmUpUsesOnlyBroadCategoryWithoutFormalContent(t *testing.T) {
	result, err := NewIELTSWarmUpTool().Execute(
		context.Background(),
		capability.CallContext{},
		json.RawMessage(
			`{"ielts_practice_mode":"PART_2","ielts_topic_choice":"experience"}`,
		),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content["prompt"] !=
		"可以。最近有没有哪次经历让你印象挺深？用一两句英语说说。" ||
		len(result.Content) != 1 ||
		len(result.SourceRefs) != 0 || len(result.Handoffs) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, found := result.Content["mode"]; found {
		t.Fatalf("result exposes internal mode enum: %#v", result.Content)
	}
	if _, found := result.Content["response_steps"]; found {
		t.Fatalf("result includes list-oriented response_steps: %#v", result.Content)
	}
	if _, found := result.Content["sentence_starter"]; found {
		t.Fatalf("result includes an unsolicited answer template: %#v", result.Content)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal result: %v", err)
	}
	for _, secret := range []string{
		"一次难忘的经历",
		"Describe a memorable experience.",
		"when it happened",
		"why it mattered",
		"response_steps",
		"Introduce",
		"one sentence",
		"I'd like to talk about",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("result exposed private question-bank content %q: %s", secret, encoded)
		}
	}
}

func TestIELTSWarmUpRandomUsesSafeBroadPrompt(t *testing.T) {
	result, err := NewIELTSWarmUpTool().Execute(
		context.Background(),
		capability.CallContext{},
		json.RawMessage(
			`{"ielts_practice_mode":"PART_3","ielts_topic_choice":"random"}`,
		),
	)
	if err != nil ||
		result.Content["prompt"] !=
			"那就随意聊聊：最近有什么人、地方、事物或经历让你印象挺深？挑一个，用一两句英语说说。" {
		t.Fatalf("Execute error = %v", err)
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("Marshal result: %v", marshalErr)
	}
	for _, leaked := range []string{"random", "person", "place", "thing", "experience"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("result exposed internal topic value %q: %s", leaked, encoded)
		}
	}
}

func TestIELTSWarmUpSupportsEveryBroadCategory(t *testing.T) {
	for choice, prompt := range map[string]string{
		"person":     "可以。最近有没有谁让你印象挺深？用一两句英语说说。",
		"place":      "可以。最近有没有哪个地方让你印象挺深？用一两句英语说说。",
		"thing":      "可以。最近有没有什么东西让你印象挺深？用一两句英语说说。",
		"experience": "可以。最近有没有哪次经历让你印象挺深？用一两句英语说说。",
	} {
		result, err := NewIELTSWarmUpTool().Execute(
			context.Background(),
			capability.CallContext{},
			json.RawMessage(
				`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"`+
					choice+`"}`,
			),
		)
		if err != nil || result.Content["prompt"] != prompt || len(result.Content) != 1 {
			t.Fatalf("Execute(%q) = (%#v, %v)", choice, result, err)
		}
	}
}

func TestIELTSWarmUpRejectsUnknownTopicChoice(t *testing.T) {
	_, err := NewIELTSWarmUpTool().Execute(
		context.Background(),
		capability.CallContext{},
		json.RawMessage(
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"official-topic"}`,
		),
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("Execute error = %v", err)
	}
}

func TestIELTSWarmUpRejectsFullMock(t *testing.T) {
	_, err := NewIELTSWarmUpTool().Execute(
		context.Background(),
		capability.CallContext{},
		json.RawMessage(
			`{"ielts_practice_mode":"FULL_MOCK","ielts_topic_choice":"random"}`,
		),
	)
	if !errors.Is(err, capability.ErrInvalidInput) {
		t.Fatalf("Execute error = %v", err)
	}
}
