package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func TestIELTSCreationToolChoice(t *testing.T) {
	warmUp := specificToolChoice(ieltsWarmUpToolName)
	preview := specificToolChoice(practicePreviewToolName)
	auto := ToolChoice{Mode: ToolChoiceAuto}
	none := ToolChoice{Mode: ToolChoiceNone}
	tests := []struct {
		name           string
		messages       []TextMessage
		previousTool   priorIELTSTool
		want           ToolChoice
		wantPriorState bool
	}{
		{
			name:     "Chinese IELTS Part one person",
			messages: routingMessages("雅思 part one 人物"),
			want:     warmUp,
		},
		{
			name:     "English IELTS Part 2 place",
			messages: routingMessages("Create IELTS Part 2 place practice"),
			want:     warmUp,
		},
		{
			name:     "Part 3 experience",
			messages: routingMessages("创建雅思 Part 3 经历类专项练习"),
			want:     warmUp,
		},
		{
			name:     "Part 1 thing",
			messages: routingMessages("帮我安排 IELTS Part 1 事物类练习"),
			want:     warmUp,
		},
		{
			name:     "Part 1 random",
			messages: routingMessages("帮我创建一场 IELTS Part 1，随机安排"),
			want:     warmUp,
		},
		{
			name: "category only follow-up",
			messages: routingConversation(
				"帮我创建一场 IELTS Part 1",
				"随机、人物、地点、事物还是经历？",
				"人物",
			),
			want: warmUp,
		},
		{
			name: "natural category follow-up",
			messages: routingConversation(
				"帮我创建一场雅思的 Part One。",
				"随机、人物、地点、事物还是经历？",
				"呃来一场人物的吧。",
			),
			want: warmUp,
		},
		{
			name: "four user turns retain IELTS creation",
			messages: []TextMessage{
				{Role: TextRoleSystem, Content: "You are SpeakUp."},
				{Role: TextRoleUser, Content: "我想练一场雅思口语"},
				{Role: TextRoleAssistant, Content: "想练哪个部分？"},
				{Role: TextRoleUser, Content: "Part 1"},
				{Role: TextRoleAssistant, Content: "想选什么话题？"},
				{Role: TextRoleUser, Content: "人物"},
				{Role: TextRoleAssistant, Content: "先随便聊一句，不计分。"},
				{Role: TextRoleUser, Content: "My teacher made every lesson interesting."},
			},
			previousTool:   priorIELTSToolWarmUp,
			want:           preview,
			wantPriorState: true,
		},
		{
			name: "Part then category follow-ups",
			messages: []TextMessage{
				{Role: TextRoleSystem, Content: "You are SpeakUp."},
				{Role: TextRoleUser, Content: "我想练一场雅思口语"},
				{Role: TextRoleAssistant, Content: "想练哪个部分？"},
				{Role: TextRoleUser, Content: "Part 1"},
				{Role: TextRoleAssistant, Content: "想选什么话题？"},
				{Role: TextRoleUser, Content: "人物"},
			},
			want: warmUp,
		},
		{
			name:     "missing Part stays model routed",
			messages: routingMessages("我想练一场雅思口语"),
			want:     auto,
		},
		{
			name:     "missing category stays model routed",
			messages: routingMessages("帮我创建一场 IELTS Part 1"),
			want:     auto,
		},
		{
			name:     "direct start",
			messages: routingMessages("创建 IELTS Part 1 人物专项，直接开始"),
			want:     preview,
		},
		{
			name:     "skip warm-up",
			messages: routingMessages("创建 IELTS Part 2 地点专项，跳过热身"),
			want:     preview,
		},
		{
			name:     "no warm-up",
			messages: routingMessages("创建 IELTS Part 3 经历专项，不用热身"),
			want:     preview,
		},
		{
			name:     "full mock",
			messages: routingMessages("帮我创建一场雅思口语完整模考"),
			want:     preview,
		},
		{
			name: "warm-up answer",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"I'd like to talk about my high school teacher.",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           preview,
			wantPriorState: true,
		},
		{
			name: "warm-up-looking text without audit is not trusted",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"I'd like to talk about my high school teacher.",
			),
			want:           warmUp,
			wantPriorState: true,
		},
		{
			name: "cannot answer after warm-up stays conversational",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"我不会",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           none,
			wantPriorState: true,
		},
		{
			name: "sentence question after warm-up stays conversational",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"这个句型是什么意思？",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           none,
			wantPriorState: true,
		},
		{
			name: "pause after warm-up stays conversational",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"先等等",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           none,
			wantPriorState: true,
		},
		{
			name: "short English uncertainty after warm-up stays conversational",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"I don't know.",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           none,
			wantPriorState: true,
		},
		{
			name: "English help request after warm-up stays conversational",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"Could you give me an example.",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           none,
			wantPriorState: true,
		},
		{
			name: "direct start after warm-up",
			messages: routingConversation(
				"给我一场 IELTS Part 3 经历类练习",
				"好，我们先热个身。",
				"直接开始",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           preview,
			wantPriorState: true,
		},
		{
			name: "cancel after warm-up",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"算了，先不练了",
			),
			previousTool: priorIELTSToolWarmUp,
			want:         auto,
		},
		{
			name:     "negated creation stays model routed",
			messages: routingMessages("我不想练 IELTS Part 1 人物"),
			want:     auto,
		},
		{
			name:     "do not skip means warm-up",
			messages: routingMessages("创建 IELTS Part 1 人物练习，不要跳过热身"),
			want:     warmUp,
		},
		{
			name: "change Part and category after warm-up",
			messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				"改成 Part 3 经历",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           warmUp,
			wantPriorState: true,
		},
		{
			name: "change category after warm-up",
			messages: routingConversation(
				"创建雅思 Part 1 人物类专项练习",
				"好，我们先热个身。",
				"换成地点",
			),
			previousTool:   priorIELTSToolWarmUp,
			want:           warmUp,
			wantPriorState: true,
		},
		{
			name: "completed preview does not capture later chat",
			messages: routingConversation(
				"创建雅思 Part 1 随机专项，直接开始",
				"正式练习已准备好。",
				"这个练习需要多久？",
			),
			previousTool:   priorIELTSToolPreview,
			want:           auto,
			wantPriorState: true,
		},
		{
			name: "completed preview is not recreated by repeated direct start",
			messages: routingConversation(
				"创建雅思 Part 1 人物类专项，直接开始",
				"好。",
				"直接开始",
			),
			previousTool:   priorIELTSToolPreview,
			want:           auto,
			wantPriorState: true,
		},
		{
			name:     "IELTS advice is not creation",
			messages: routingMessages("雅思 Part 1 人物题怎么回答？"),
			want:     auto,
		},
		{
			name:     "IELTS example request is not creation",
			messages: routingMessages("给我一个 IELTS Part 1 人物题例子"),
			want:     auto,
		},
		{
			name:     "IELTS practice advice is not creation",
			messages: routingMessages("雅思 Part 1 人物类应该怎么练？"),
			want:     auto,
		},
		{
			name:     "IELTS practice arrangement advice is not creation",
			messages: routingMessages("雅思 Part 1 人物类练习应该怎么安排？"),
			want:     auto,
		},
		{
			name:     "IELTS practice statement is not creation",
			messages: routingMessages("IELTS Part 1 person practice is difficult"),
			want:     auto,
		},
		{
			name:     "full mock statement is not creation",
			messages: routingMessages("IELTS full mock is difficult"),
			want:     auto,
		},
		{
			name:     "ambiguous categories stay model routed",
			messages: routingMessages("创建 IELTS Part 1 人物或地点练习"),
			want:     auto,
		},
		{
			name:     "unsupported Part stays model routed",
			messages: routingMessages("创建 IELTS Part 4 人物练习"),
			want:     auto,
		},
		{
			name:     "non IELTS stays model routed",
			messages: routingMessages("帮我创建一场产品经理面试练习"),
			want:     auto,
		},
		{
			name: "IELTS root beyond adjacent context is ignored",
			messages: []TextMessage{
				{Role: TextRoleSystem, Content: "You are SpeakUp."},
				{Role: TextRoleUser, Content: "创建 IELTS Part 1 人物练习"},
				{Role: TextRoleAssistant, Content: "好。"},
				{Role: TextRoleUser, Content: "聊点别的"},
				{Role: TextRoleAssistant, Content: "可以。"},
				{Role: TextRoleUser, Content: "再聊一点"},
				{Role: TextRoleAssistant, Content: "好。"},
				{Role: TextRoleUser, Content: "继续"},
			},
			previousTool: priorIELTSToolWarmUp,
			want:         auto,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routing := inspectIELTSCreationRouting(TextRequest{
				Messages: test.messages,
			})
			if got := routing.toolChoice(test.previousTool); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ToolChoice = %#v, want %#v; routing = %#v", got, test.want, routing)
			}
			if got := routing.needsPriorToolState(); got != test.wantPriorState {
				t.Fatalf("needsPriorToolState = %t, want %t; routing = %#v", got, test.wantPriorState, routing)
			}
		})
	}
}

func TestPriorIELTSToolResultUsesAdjacentAssistantAudit(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_2","ielts_topic_choice":"person"}`,
	)
	routing := ieltsCreationRouting{
		active: true, mode: ieltsRoutingModePart2, topicChoice: "person",
	}
	tests := []struct {
		name        string
		selected    []agentcontext.MessageSource
		message     conversation.Message
		calls       []ToolCall
		want        priorIELTSTool
		wantLookups int
	}{
		{
			name:     "successful warm-up",
			selected: adjacentIELTSMessageSources(),
			message: conversation.Message{
				Role: conversation.MessageRoleAssistant, ProducedByRunID: "previous-run",
			},
			calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: matchingInput,
				Status: ToolCallSucceeded,
			}},
			want: priorIELTSToolWarmUp, wantLookups: 1,
		},
		{
			name:     "successful preview wins",
			selected: adjacentIELTSMessageSources(),
			message: conversation.Message{
				Role: conversation.MessageRoleAssistant, ProducedByRunID: "previous-run",
			},
			calls: []ToolCall{
				{Name: ieltsWarmUpToolName, Input: matchingInput, Status: ToolCallSucceeded},
				{
					Name: practicePreviewToolName, Input: matchingInput,
					Status:   ToolCallSucceeded,
					Result:   json.RawMessage(`{"content":{"status":"preview_ready"}}`),
					Handoffs: []agenthandoff.Item{loopPracticeHandoff()},
				},
			},
			want: priorIELTSToolPreview, wantLookups: 1,
		},
		{
			name:     "failed warm-up is not state",
			selected: adjacentIELTSMessageSources(),
			message: conversation.Message{
				Role: conversation.MessageRoleAssistant, ProducedByRunID: "previous-run",
			},
			calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: matchingInput,
				Status: ToolCallFailed,
			}},
			want: priorIELTSToolNone, wantLookups: 1,
		},
		{
			name:     "mismatched successful warm-up is not state",
			selected: adjacentIELTSMessageSources(),
			message: conversation.Message{
				Role: conversation.MessageRoleAssistant, ProducedByRunID: "previous-run",
			},
			calls: []ToolCall{{
				Name: ieltsWarmUpToolName,
				Input: json.RawMessage(
					`{"ielts_practice_mode":"PART_2","ielts_topic_choice":"place"}`,
				),
				Status: ToolCallSucceeded,
			}},
			want: priorIELTSToolNone, wantLookups: 1,
		},
		{
			name: "truncated adjacent assistant is not guessed",
			selected: []agentcontext.MessageSource{
				{MessageID: "older-assistant", Sequence: 4, Role: conversation.MessageRoleAssistant},
				{MessageID: "current-user", Sequence: 6, Role: conversation.MessageRoleUser},
			},
			want: priorIELTSToolNone,
		},
		{
			name: "only current message",
			selected: []agentcontext.MessageSource{
				{MessageID: "current-user", Sequence: 6, Role: conversation.MessageRoleUser},
			},
			want: priorIELTSToolNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &ieltsRoutingRepository{calls: test.calls}
			reader := &recordingMessageReader{message: test.message}
			service := &Service{repository: repository, messages: reader}
			got, err := service.priorIELTSToolResult(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{SelectedMessages: test.selected},
				routing,
			)
			if err != nil {
				t.Fatalf("priorIELTSToolResult() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("priorIELTSToolResult() = %v, want %v", got, test.want)
			}
			if repository.listCalls != test.wantLookups {
				t.Fatalf("ListToolCalls calls = %d, want %d", repository.listCalls, test.wantLookups)
			}
			if test.wantLookups == 1 &&
				(reader.messageID != "previous-assistant" ||
					repository.runID != "previous-run") {
				t.Fatalf(
					"audit lookup = message %q, run %q",
					reader.messageID,
					repository.runID,
				)
			}
		})
	}
}

func TestRunLoopAppliesDeterministicIELTSToolChoice(t *testing.T) {
	tests := []struct {
		name      string
		messages  []TextMessage
		toolName  string
		arguments string
		wantNext  ToolChoiceMode
		wantCalls int
		wantText  string
	}{
		{
			name: "specialty warm-up",
			messages: routingMessages(
				"创建雅思 Part 2 人物类专项练习",
			),
			toolName:  ieltsWarmUpToolName,
			arguments: `{"ielts_practice_mode":"PART_2","ielts_topic_choice":"person"}`,
			wantNext:  ToolChoiceNone,
			wantCalls: 1,
			wantText:  "done",
		},
		{
			name: "category follow-up warm-up",
			messages: routingConversation(
				"帮我创建一场 IELTS Part 1",
				"随机、人物、地点、事物还是经历？",
				"人物",
			),
			toolName:  ieltsWarmUpToolName,
			arguments: `{"ielts_practice_mode":"PART_1","ielts_topic_choice":"person"}`,
			wantNext:  ToolChoiceNone,
			wantCalls: 1,
			wantText:  "done",
		},
		{
			name: "direct specialty preview",
			messages: routingMessages(
				"创建 IELTS Part 1 随机专项，直接开始",
			),
			toolName:  practicePreviewToolName,
			arguments: `{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
			wantNext:  ToolChoiceNone,
			wantCalls: 1,
			wantText:  "done",
		},
		{
			name: "full mock preview",
			messages: routingMessages(
				"帮我创建一场雅思口语完整模考",
			),
			toolName:  practicePreviewToolName,
			arguments: `{"ielts_practice_mode":"FULL_MOCK"}`,
			wantNext:  ToolChoiceNone,
			wantCalls: 1,
			wantText:  "done",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := &ieltsLoopTool{name: test.toolName}
			generator := newScriptedGenerator(finalLoopResult("done"))
			service := newLoopTestService(t, generator)
			setLoopTools(t, service, capabilityfixture.NewStore(), selected)

			result, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{},
				TextRequest{Messages: test.messages},
			)
			if err != nil {
				t.Fatalf("generate() error = %v", err)
			}
			if result.Content != test.wantText || selected.calls != 1 {
				t.Fatalf("result = %#v, tool calls = %d", result, selected.calls)
			}
			requests := generator.Requests()
			if got, want := len(requests), test.wantCalls; got != want {
				t.Fatalf("model requests = %d, want %d", got, want)
			}
			if test.wantCalls == 0 {
				return
			}
			if got := requests[0].ToolChoice; got.Mode != test.wantNext {
				t.Fatalf("next ToolChoice = %#v, want mode %q", got, test.wantNext)
			}
		})
	}
}

func TestIELTSRoutingBuildsAuthoritativeToolInput(t *testing.T) {
	tests := []struct {
		name     string
		routing  ieltsCreationRouting
		toolName string
		want     string
	}{
		{
			name: "specialty",
			routing: ieltsCreationRouting{
				mode: ieltsRoutingModePart1, topicChoice: "person",
			},
			toolName: ieltsWarmUpToolName,
			want:     `{"ielts_practice_mode":"PART_1","ielts_topic_choice":"person"}`,
		},
		{
			name:     "full mock",
			routing:  ieltsCreationRouting{mode: ieltsRoutingModeFullMock},
			toolName: practicePreviewToolName,
			want:     `{"ielts_practice_mode":"FULL_MOCK"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call, ok := test.routing.deterministicToolCall("run-1", test.toolName)
			if !ok || call.Name != test.toolName ||
				string(call.Arguments) != test.want {
				t.Fatalf("call = %#v, ok = %t", call, ok)
			}
		})
	}
}

func TestRunLoopReturnsDeterministicIELTSClarification(t *testing.T) {
	tests := []struct {
		name     string
		messages []TextMessage
		want     string
	}{
		{
			name:     "missing scope",
			messages: routingMessages("帮我创建一场 IELTS 口语练习"),
			want:     "没问题，你想先练 Part 1、Part 2、Part 3，还是直接来一场完整模考？",
		},
		{
			name:     "missing category",
			messages: routingMessages("帮我创建一场 IELTS Part 1"),
			want:     "好，那就先练 Part 1：你想聊人物、地点、事物还是经历，还是让我随机选一个？",
		},
		{
			name: "Part follow-up then missing category",
			messages: routingConversation(
				"帮我创建一场 IELTS 口语练习",
				"想练哪个部分？",
				"Part 2",
			),
			want: "好，那就先练 Part 2：你想聊人物、地点、事物还是经历，还是让我随机选一个？",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := newScriptedGenerator(finalLoopResult("model must not run"))
			service := newLoopTestService(t, generator)
			result, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{},
				TextRequest{Messages: test.messages},
			)
			if err != nil {
				t.Fatalf("generate() error = %v", err)
			}
			if result.Content != test.want || result.FinishReason != "stop" ||
				result.Provider != "fake" || result.Model != "configured-model" ||
				!ValidOpaqueID(result.ID) || generator.CallCount() != 0 {
				t.Fatalf("result = %#v, model calls = %d", result, generator.CallCount())
			}
		})
	}
}

func TestRunLoopKeepsWarmUpHelpRepliesToolFree(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_2","ielts_topic_choice":"person"}`,
	)
	for _, reply := range []string{
		"我不会",
		"这个句型是什么意思？",
		"先等等",
		"I don't know.",
	} {
		repository := &ieltsRoutingRepository{calls: []ToolCall{{
			Name: ieltsWarmUpToolName, Input: matchingInput,
			Status: ToolCallSucceeded,
		}}}
		generator := newScriptedGenerator(finalLoopResult("我可以帮你拆解。"))
		service := newLoopTestService(t, generator)
		service.repository = repository
		service.messages = &recordingMessageReader{message: conversation.Message{
			Role:            conversation.MessageRoleAssistant,
			ProducedByRunID: "previous-run",
		}}

		result, err := service.generate(
			context.Background(),
			loopActor(),
			loopRun(),
			agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
			TextRequest{Messages: routingConversation(
				"创建雅思 Part 2 人物类专项练习",
				"好，我们先热个身。",
				reply,
			)},
		)
		if err != nil {
			t.Fatalf("reply %q: generate() error = %v", reply, err)
		}
		requests := generator.Requests()
		if result.Content != "我可以帮你拆解。" || len(requests) != 1 ||
			requests[0].ToolChoice.Mode != ToolChoiceNone {
			t.Fatalf("reply %q: result = %#v, requests = %#v", reply, result, requests)
		}
	}
}

func TestRunLoopRestoresAdjacentWarmUpStateWithoutTextualRoot(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
	)
	repository := &ieltsRoutingRepository{calls: []ToolCall{{
		Name: ieltsWarmUpToolName, Input: matchingInput,
		Status: ToolCallSucceeded,
	}}}
	warmUp := &ieltsLoopTool{name: ieltsWarmUpToolName}
	preview := &ieltsLoopTool{
		name: practicePreviewToolName,
		result: capability.Result{
			Content:  map[string]any{"status": "preview_ready"},
			Handoffs: []agenthandoff.Item{loopPracticeHandoff()},
		},
	}
	generator := newScriptedGenerator(
		finalLoopResult("听起来你暂时没想到具体的 person。"),
	)
	service := newLoopTestService(t, generator)
	setLoopTools(t, service, capabilityfixture.NewStore(), warmUp, preview)
	service.repository = repository
	service.messages = &recordingMessageReader{message: conversation.Message{
		Role:            conversation.MessageRoleAssistant,
		ProducedByRunID: "previous-run",
	}}

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
		TextRequest{Messages: noRootWarmUpConversation("呃。 no person.")},
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "听起来你暂时没想到具体的 person。" ||
		warmUp.calls != 0 || preview.calls != 1 || len(preview.inputs) != 1 ||
		string(preview.inputs[0]) != string(matchingInput) {
		t.Fatalf(
			"result = %#v, warm-up calls = %d, preview calls = %d, inputs = %q",
			result,
			warmUp.calls,
			preview.calls,
			preview.inputs,
		)
	}
	requests := generator.Requests()
	if len(requests) != 1 || requests[0].ToolChoice.Mode != ToolChoiceNone {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRunLoopDoesNotRepeatWarmUpForHelpAfterRestoredState(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
	)
	for _, reply := range []string{
		"I don't know.",
		"Could you give me an example.",
		"先等等",
	} {
		t.Run(reply, func(t *testing.T) {
			repository := &ieltsRoutingRepository{calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: matchingInput,
				Status: ToolCallSucceeded,
			}}}
			warmUp := &ieltsLoopTool{name: ieltsWarmUpToolName}
			preview := &ieltsLoopTool{name: practicePreviewToolName}
			generator := newScriptedGenerator(finalLoopResult("我给你一个简短提示。"))
			service := newLoopTestService(t, generator)
			setLoopTools(t, service, capabilityfixture.NewStore(), warmUp, preview)
			service.repository = repository
			service.messages = &recordingMessageReader{message: conversation.Message{
				Role:            conversation.MessageRoleAssistant,
				ProducedByRunID: "previous-run",
			}}

			result, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
				TextRequest{Messages: noRootWarmUpConversation(reply)},
			)
			if err != nil || result.Content != "我给你一个简短提示。" ||
				warmUp.calls != 0 || preview.calls != 0 {
				t.Fatalf(
					"generate() = (%#v, %v), warm-up calls = %d, preview calls = %d",
					result,
					err,
					warmUp.calls,
					preview.calls,
				)
			}
			requests := generator.Requests()
			if len(requests) != 1 || requests[0].ToolChoice.Mode != ToolChoiceNone {
				t.Fatalf("requests = %#v", requests)
			}
		})
	}
}

func TestIELTSNaturalRandomSelection(t *testing.T) {
	for _, input := range []string{
		"呃你给我随便挑一个。",
		"呃随便帮我挑一个。",
		"你来选吧",
	} {
		topic, found := parseIELTSTopicChoice(input)
		if !found || topic != "random" || !isIELTSSelectionReply(input) {
			t.Fatalf(
				"input %q = topic %q, found %t, selection %t",
				input,
				topic,
				found,
				isIELTSSelectionReply(input),
			)
		}
	}
	for _, input := range []string{
		"不要随便挑一个",
		"为什么随便挑一个？",
	} {
		if topic, found := parseIELTSTopicChoice(input); found || topic != "" ||
			isIELTSSelectionReply(input) {
			t.Fatalf(
				"negative input %q = topic %q, found %t, selection %t",
				input,
				topic,
				found,
				isIELTSSelectionReply(input),
			)
		}
	}
}

func TestIELTSWarmUpAnswerRequiresMeaningfulEnglish(t *testing.T) {
	if isEnglishWarmUpAnswerAttempt("I am.") {
		t.Fatal("isEnglishWarmUpAnswerAttempt accepted stop words only")
	}
	if !isEnglishWarmUpAnswerAttempt("my name is nylon can you hear me?") {
		t.Fatal("isEnglishWarmUpAnswerAttempt rejected the real transcribed answer")
	}
	if got := fallbackIELTSWarmUpAcknowledgement("I am."); got != "听到了。" {
		t.Fatalf("fallbackIELTSWarmUpAcknowledgement() = %q", got)
	}
}

func TestRunLoopRestoredWarmUpStateTransitions(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
	)
	tests := []struct {
		name          string
		reply         string
		modelReply    string
		wantMode      ToolChoiceMode
		wantWarmUp    int
		wantPreview   int
		wantToolInput string
	}{
		{
			name:       "English cancellation",
			reply:      "No thanks.",
			modelReply: "好的，我们先不练。",
			wantMode:   ToolChoiceAuto,
		},
		{
			name:          "direct start",
			reply:         "直接开始",
			modelReply:    "好。",
			wantMode:      ToolChoiceNone,
			wantPreview:   1,
			wantToolInput: string(matchingInput),
		},
		{
			name:          "English topic change",
			reply:         "Switch to place.",
			modelReply:    "好，那就换成地点类。",
			wantMode:      ToolChoiceNone,
			wantWarmUp:    1,
			wantToolInput: `{"ielts_practice_mode":"PART_1","ielts_topic_choice":"place"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &ieltsRoutingRepository{calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: matchingInput,
				Status: ToolCallSucceeded,
			}}}
			warmUp := &ieltsLoopTool{name: ieltsWarmUpToolName}
			preview := &ieltsLoopTool{
				name: practicePreviewToolName,
				result: capability.Result{
					Content:  map[string]any{"status": "preview_ready"},
					Handoffs: []agenthandoff.Item{loopPracticeHandoff()},
				},
			}
			generator := newScriptedGenerator(finalLoopResult(test.modelReply))
			service := newLoopTestService(t, generator)
			setLoopTools(t, service, capabilityfixture.NewStore(), warmUp, preview)
			service.repository = repository
			service.messages = &recordingMessageReader{message: conversation.Message{
				Role:            conversation.MessageRoleAssistant,
				ProducedByRunID: "previous-run",
			}}

			if _, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
				TextRequest{Messages: noRootWarmUpConversation(test.reply)},
			); err != nil {
				t.Fatalf("generate() error = %v", err)
			}
			if warmUp.calls != test.wantWarmUp || preview.calls != test.wantPreview {
				t.Fatalf(
					"warm-up calls = %d, preview calls = %d",
					warmUp.calls,
					preview.calls,
				)
			}
			requests := generator.Requests()
			wantRequests := 1
			if test.name == "direct start" {
				wantRequests = 0
			}
			if len(requests) != wantRequests ||
				wantRequests == 1 && requests[0].ToolChoice.Mode != test.wantMode {
				t.Fatalf("requests = %#v", requests)
			}
			if test.wantToolInput == "" {
				return
			}
			var inputs []json.RawMessage
			if test.wantWarmUp == 1 {
				inputs = warmUp.inputs
			} else {
				inputs = preview.inputs
			}
			if len(inputs) != 1 || string(inputs[0]) != test.wantToolInput {
				t.Fatalf("tool inputs = %q", inputs)
			}
		})
	}
}

func TestRunLoopRestoredStateFailsClosed(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
	)
	validCall := ToolCall{
		Name: ieltsWarmUpToolName, Input: matchingInput,
		Status: ToolCallSucceeded,
	}
	tests := []struct {
		name      string
		calls     []ToolCall
		manifest  []agentcontext.MessageSource
		wantReads int
	}{
		{
			name: "failed audit",
			calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: matchingInput,
				Status: ToolCallFailed,
			}},
			manifest:  adjacentIELTSMessageSources(),
			wantReads: 1,
		},
		{
			name: "malformed audit",
			calls: []ToolCall{{
				Name: ieltsWarmUpToolName, Input: json.RawMessage(`{"bad":true}`),
				Status: ToolCallSucceeded,
			}},
			manifest:  adjacentIELTSMessageSources(),
			wantReads: 1,
		},
		{
			name:      "ambiguous audit",
			calls:     []ToolCall{validCall, validCall},
			manifest:  adjacentIELTSMessageSources(),
			wantReads: 1,
		},
		{
			name:  "non adjacent assistant",
			calls: []ToolCall{validCall},
			manifest: []agentcontext.MessageSource{
				{MessageID: "previous-assistant", Sequence: 4, Role: conversation.MessageRoleAssistant},
				{MessageID: "current-user", Sequence: 6, Role: conversation.MessageRoleUser},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &ieltsRoutingRepository{calls: test.calls}
			generator := newScriptedGenerator(finalLoopResult("我们继续聊。"))
			service := newLoopTestService(t, generator)
			service.repository = repository
			service.messages = &recordingMessageReader{message: conversation.Message{
				Role:            conversation.MessageRoleAssistant,
				ProducedByRunID: "previous-run",
			}}

			if _, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{SelectedMessages: test.manifest},
				TextRequest{Messages: noRootWarmUpConversation("呃。 no person.")},
			); err != nil {
				t.Fatalf("generate() error = %v", err)
			}
			requests := generator.Requests()
			if repository.listCalls != test.wantReads || len(requests) != 1 ||
				requests[0].ToolChoice.Mode != ToolChoiceAuto {
				t.Fatalf("reads = %d, requests = %#v", repository.listCalls, requests)
			}
		})
	}
}

func TestRunLoopDoesNotAuditOrdinaryConversation(t *testing.T) {
	repository := &ieltsRoutingRepository{calls: []ToolCall{{
		Name: ieltsWarmUpToolName,
		Input: json.RawMessage(
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
		),
		Status: ToolCallSucceeded,
	}}}
	generator := newScriptedGenerator(finalLoopResult("Sounds good."))
	service := newLoopTestService(t, generator)
	service.repository = repository
	service.messages = &recordingMessageReader{message: conversation.Message{
		Role:            conversation.MessageRoleAssistant,
		ProducedByRunID: "previous-run",
	}}

	if _, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
		TextRequest{Messages: routingConversation(
			"Let's talk about food.",
			"What food do you like?",
			"I like noodles.",
		)},
	); err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if repository.listCalls != 0 {
		t.Fatalf("ListToolCalls() calls = %d, want 0", repository.listCalls)
	}
}

func TestRunLoopDoesNotRecreateReadyPreviewFromAdjacentAudit(t *testing.T) {
	repository := &ieltsRoutingRepository{calls: []ToolCall{{
		Name: practicePreviewToolName,
		Input: json.RawMessage(
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"random"}`,
		),
		Result:   json.RawMessage(`{"content":{"status":"preview_ready"}}`),
		Handoffs: []agenthandoff.Item{loopPracticeHandoff()},
		Status:   ToolCallSucceeded,
	}}}
	warmUp := &ieltsLoopTool{name: ieltsWarmUpToolName}
	preview := &ieltsLoopTool{name: practicePreviewToolName}
	generator := newScriptedGenerator(finalLoopResult("我们可以聊聊这场练习。"))
	service := newLoopTestService(t, generator)
	setLoopTools(t, service, capabilityfixture.NewStore(), warmUp, preview)
	service.repository = repository
	service.messages = &recordingMessageReader{message: conversation.Message{
		Role:            conversation.MessageRoleAssistant,
		ProducedByRunID: "previous-run",
	}}

	if _, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
		TextRequest{Messages: noRootWarmUpConversation("Tell me more.")},
	); err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	requests := generator.Requests()
	if repository.listCalls != 1 || warmUp.calls != 0 || preview.calls != 0 ||
		len(requests) != 1 || requests[0].ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf(
			"reads = %d, warm-up = %d, preview = %d, requests = %#v",
			repository.listCalls,
			warmUp.calls,
			preview.calls,
			requests,
		)
	}
}

func TestRunLoopRejectsPracticeReadyClaimWithoutPreview(t *testing.T) {
	matchingInput := json.RawMessage(
		`{"ielts_practice_mode":"PART_2","ielts_topic_choice":"person"}`,
	)
	repository := &ieltsRoutingRepository{calls: []ToolCall{{
		Name: ieltsWarmUpToolName, Input: matchingInput,
		Status: ToolCallSucceeded,
	}}}
	generator := newScriptedGenerator(finalLoopResult("正式练习已准备好。"))
	service := newLoopTestService(t, generator)
	service.repository = repository
	service.messages = &recordingMessageReader{message: conversation.Message{
		Role:            conversation.MessageRoleAssistant,
		ProducedByRunID: "previous-run",
	}}

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{SelectedMessages: adjacentIELTSMessageSources()},
		TextRequest{Messages: routingConversation(
			"创建雅思 Part 2 人物类专项练习",
			"先随便聊一句，不计分。",
			"我不会",
		)},
	)
	var generationError *GenerationError
	if !errors.As(err, &generationError) ||
		generationError.Kind != ErrorInvalidResponse {
		t.Fatalf("generate() error = %#v, want invalid response", err)
	}
}

func TestRunLoopDoesNotAutoRouteAfterForcedIELTSToolFailure(t *testing.T) {
	warmUp := &ieltsLoopTool{
		name: ieltsWarmUpToolName,
		err:  capability.ErrExecutionRejected,
	}
	preview := &ieltsLoopTool{name: practicePreviewToolName}
	generator := newScriptedGenerator(finalLoopResult("暂时没能开始，我们可以重试。"))
	service := newLoopTestService(t, generator)
	setLoopTools(t, service, capabilityfixture.NewStore(), warmUp, preview)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("创建雅思 Part 2 人物类专项练习"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if warmUp.calls != 1 || preview.calls != 0 ||
		result.Content != "暂时没能开始，我们可以重试。" {
		t.Fatalf(
			"result = %#v, warm-up calls = %d, preview calls = %d",
			result,
			warmUp.calls,
			preview.calls,
		)
	}
	requests := generator.Requests()
	if len(requests) != 1 || requests[0].ToolChoice.Mode != ToolChoiceNone {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRunLoopLogsIELTSCreationRoutingGuard(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	selected := &ieltsLoopTool{name: ieltsWarmUpToolName}
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-ielts-route",
			ieltsWarmUpToolName,
			`{"ielts_practice_mode":"PART_1","ielts_topic_choice":"person"}`,
		),
		finalLoopResult("done"),
	)
	service := newLoopTestService(t, generator)
	setLoopTools(t, service, capabilityfixture.NewStore(), selected)
	service.logger = logger

	if _, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("雅思 part one 人物"),
	); err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	output := logs.String()
	for _, want := range []string{
		`"routing_version":"model-tool-routing-v2"`,
		`"tool_choice_mode":"specific"`,
		`"tool_choice_name":"ielts.warmup.v1"`,
		`"reason_code":"ielts_creation_routing_guard"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("logs = %s, want %s", output, want)
		}
	}
}

func routingMessages(input string) []TextMessage {
	return []TextMessage{
		{Role: TextRoleSystem, Content: "You are SpeakUp."},
		{Role: TextRoleUser, Content: input},
	}
}

func routingConversation(first string, assistant string, current string) []TextMessage {
	return []TextMessage{
		{Role: TextRoleSystem, Content: "You are SpeakUp."},
		{Role: TextRoleUser, Content: first},
		{Role: TextRoleAssistant, Content: assistant},
		{Role: TextRoleUser, Content: current},
	}
}

func noRootWarmUpConversation(current string) []TextMessage {
	return []TextMessage{
		{Role: TextRoleSystem, Content: "You are SpeakUp."},
		{Role: TextRoleUser, Content: "嗯。我最近在学雅思。"},
		{Role: TextRoleAssistant, Content: "你想练哪个部分？"},
		{Role: TextRoleUser, Content: "Part One"},
		{Role: TextRoleAssistant, Content: "想聊哪类话题？"},
		{Role: TextRoleUser, Content: "呃你给我随便挑一个。"},
		{Role: TextRoleAssistant, Content: "好，先随便聊一句。"},
		{Role: TextRoleUser, Content: current},
	}
}

func adjacentIELTSMessageSources() []agentcontext.MessageSource {
	return []agentcontext.MessageSource{
		{MessageID: "previous-assistant", Sequence: 5, Role: conversation.MessageRoleAssistant},
		{MessageID: "current-user", Sequence: 6, Role: conversation.MessageRoleUser},
	}
}

type ieltsRoutingRepository struct {
	loopRepository
	calls     []ToolCall
	listCalls int
	runID     string
}

type ieltsLoopTool struct {
	name   string
	calls  int
	inputs []json.RawMessage
	result capability.Result
	err    error
}

func (tool *ieltsLoopTool) Definition() capability.Definition {
	return capability.Definition{
		Name:        tool.name,
		Description: "Exercise deterministic IELTS routing in the Agent loop.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"ielts_practice_mode": capability.StringEnumSchema(
				"Selected IELTS practice mode.",
				"FULL_MOCK", "PART_1", "PART_2", "PART_3",
			),
			"ielts_topic_choice": capability.StringEnumSchema(
				"Selected IELTS topic category.",
				"random", "person", "place", "thing", "experience",
			),
		}, nil),
		ReadOnly: true,
		Risk:     capability.RiskReadOnly,
	}
}

func (tool *ieltsLoopTool) Execute(
	_ context.Context,
	_ capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	tool.calls++
	tool.inputs = append(tool.inputs, append(json.RawMessage(nil), input...))
	if tool.err != nil {
		return capability.Result{}, tool.err
	}
	if tool.result.Content != nil {
		return tool.result, nil
	}
	return capability.Result{Content: map[string]any{"ok": true}}, nil
}

func (repository *ieltsRoutingRepository) ListToolCalls(
	_ context.Context,
	_ string,
	runID string,
) ([]ToolCall, error) {
	repository.listCalls++
	repository.runID = runID
	return repository.calls, nil
}
