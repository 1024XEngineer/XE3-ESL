package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/command"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

func TestRunLoopExposesAllToolsAndAllowsDirectResponse(t *testing.T) {
	generator := newScriptedGenerator(finalLoopResult("direct-answer"))
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("帮我把这句话说得委婉一点"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "direct-answer" {
		t.Fatalf("Content = %q, want direct-answer", result.Content)
	}
	requests := generator.Requests()
	gotTools := exposedToolNameList(requests[0].Tools)
	wantTools := []string{
		mocktool.MaterialSearchToolName,
		mocktool.MistakeSearchToolName,
		reviewtool.ReviewGetToolName,
		reviewtool.ReviewSearchToolName,
		mattertool.ScenarioCreateToolName,
		mattertool.ScenarioSearchToolName,
	}
	if !reflect.DeepEqual(gotTools, wantTools) {
		t.Fatalf("Tools = %#v, want %#v", gotTools, wantTools)
	}
	if requests[0].ToolChoice.Mode != ai.ToolChoiceAuto {
		t.Fatalf("ToolChoice = %#v, want auto", requests[0].ToolChoice)
	}
}

func TestRunLoopExecutesToolCallAndFeedsResultBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-review-1", reviewtool.ReviewSearchToolName, `{"query":"metrics","limit":1}`),
		finalLoopResult("I found the review and summarized it."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("请结合我的信息处理一下"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I found the review and summarized it." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	if got := len(requests[0].Tools); got == 0 {
		t.Fatal("first request exposed no tools")
	}
	if requests[0].ToolChoice.Mode != ai.ToolChoiceAuto {
		t.Fatalf("first ToolChoice = %#v, want auto", requests[0].ToolChoice)
	}
	second := requests[1]
	if second.ToolChoice.Mode != ai.ToolChoiceAuto {
		t.Fatalf("second ToolChoice = %#v, want auto", second.ToolChoice)
	}
	if got, want := len(second.Messages), 4; got != want {
		t.Fatalf("second request messages = %d, want %d", got, want)
	}
	toolMessage := second.Messages[len(second.Messages)-1]
	if toolMessage.Role != ai.TextRoleTool ||
		toolMessage.ToolCallID != "call-review-1" ||
		!strings.Contains(toolMessage.Content, `"reviews"`) {
		t.Fatalf("tool message = %#v", toolMessage)
	}
}

func TestRunLoopExecutesMultipleToolCallsAndFeedsAllResultsBack(t *testing.T) {
	generator := newScriptedGenerator(
		ai.TextResult{
			ID:           "fake-completion-tools",
			Provider:     "fake",
			Model:        "configured-model",
			FinishReason: "tool_calls",
			ToolCalls: []ai.ToolCall{
				{
					ID:        "call-review-1",
					Name:      reviewtool.ReviewSearchToolName,
					Arguments: json.RawMessage(`{"query":"first","limit":1}`),
				},
				{
					ID:        "call-review-2",
					Name:      reviewtool.ReviewSearchToolName,
					Arguments: json.RawMessage(`{"query":"second","limit":1}`),
				},
			},
			Usage: ai.TokenUsage{
				InputTokens:  1,
				OutputTokens: 1,
				TotalTokens:  2,
			},
		},
		finalLoopResult("I compared both reviews."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("比较我两次面试评价"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I compared both reviews." {
		t.Fatalf("Content = %q", result.Content)
	}

	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	messages := requests[1].Messages
	if got, want := len(messages), 5; got != want {
		t.Fatalf("second request messages = %d, want %d", got, want)
	}
	assistant := messages[2]
	if assistant.Role != ai.TextRoleAssistant ||
		len(assistant.ToolCalls) != 2 {
		t.Fatalf("assistant tool calls = %#v", assistant)
	}
	for index, callID := range []string{"call-review-1", "call-review-2"} {
		message := messages[index+3]
		if message.Role != ai.TextRoleTool ||
			message.ToolCallID != callID ||
			!strings.Contains(message.Content, `"reviews"`) {
			t.Fatalf("tool message %d = %#v", index, message)
		}
	}
}

func TestRunLoopSupportsConsecutiveToolRoundsBeforeFinalResponse(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-review-1",
			reviewtool.ReviewSearchToolName,
			`{"query":"metrics","limit":1}`,
		),
		toolLoopResult(
			"call-material-1",
			mocktool.MaterialSearchToolName,
			`{"query":"backend","limit":1}`,
		),
		finalLoopResult("I combined the review with your resume."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("结合我的评价和简历准备下一轮面试"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I combined the review with your resume." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 3; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	lastMessages := requests[2].Messages
	if got, want := len(lastMessages), 6; got != want {
		t.Fatalf("final request messages = %d, want %d", got, want)
	}
	if lastMessages[3].ToolCallID != "call-review-1" ||
		lastMessages[5].ToolCallID != "call-material-1" {
		t.Fatalf("tool result chain = %#v", lastMessages)
	}
}

func TestRunLoopAllowsModelToAnswerWithoutToolCall(t *testing.T) {
	generator := newScriptedGenerator(finalLoopResult("made-up review"))
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("帮我找一下上次 PM interview 的 review"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "made-up review" {
		t.Fatalf("Content = %q, want model response", result.Content)
	}
}

func TestRunLoopExecutesExplicitCommandBeforeModelResponse(t *testing.T) {
	generator := newScriptedGenerator(finalLoopResult("I found your review."))
	service := newLoopTestService(t, generator)
	commandRegistry, err := command.NewRegistry(command.Builtins()...)
	if err != nil {
		t.Fatalf("command.NewRegistry() error = %v", err)
	}
	service.commands = command.NewRouter(commandRegistry)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("/查评价 last interview"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I found your review." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 1; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	request := requests[0]
	if request.ToolChoice.Mode != ai.ToolChoiceAuto ||
		len(request.Tools) != 6 {
		t.Fatalf(
			"command response routing = choice %#v, tools %d",
			request.ToolChoice,
			len(request.Tools),
		)
	}
	if got, want := len(request.Messages), 4; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
	assistant := request.Messages[2]
	toolResult := request.Messages[3]
	if len(assistant.ToolCalls) != 1 ||
		assistant.ToolCalls[0].Name != reviewtool.ReviewSearchToolName ||
		toolResult.Role != ai.TextRoleTool ||
		toolResult.ToolCallID != "command-call" {
		t.Fatalf(
			"command messages = assistant %#v, tool %#v",
			assistant,
			toolResult,
		)
	}
}

func TestRunLoopContinuesToolCallingAfterExplicitCommand(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-material-after-command",
			mocktool.MaterialSearchToolName,
			`{"query":"backend","limit":1}`,
		),
		finalLoopResult("I combined the command result with your material."),
	)
	service := newLoopTestService(t, generator)
	commandRegistry, err := command.NewRegistry(command.Builtins()...)
	if err != nil {
		t.Fatalf("command.NewRegistry() error = %v", err)
	}
	service.commands = command.NewRouter(commandRegistry)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("/查评价 last interview"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I combined the command result with your material." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	messages := requests[1].Messages
	if got, want := len(messages), 6; got != want {
		t.Fatalf("final request messages = %d, want %d", got, want)
	}
	if messages[3].ToolCallID != "command-call" ||
		messages[5].ToolCallID != "call-material-after-command" {
		t.Fatalf("command tool chain = %#v", messages)
	}
}

func TestRunLoopFeedsToolErrorBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-material-1", mocktool.MaterialSearchToolName, `{"query":"backend"}`),
		finalLoopResult("I could not read the material, so I will continue without it."),
	)
	store := mocktool.NewStore()
	store.SetUnavailable(mocktool.MaterialSearchToolName, true)
	service := newLoopTestServiceWithStore(t, generator, store)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("结合我的简历准备面试"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I could not read the material, so I will continue without it." {
		t.Fatalf("Content = %q", result.Content)
	}
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	toolResult := generator.Requests()[1].Messages[3]
	if toolResult.Role != ai.TextRoleTool ||
		toolResult.ToolCallID != "call-material-1" ||
		!strings.Contains(toolResult.Content, `"category":"internal"`) ||
		!strings.Contains(toolResult.Content, `"retryable":true`) {
		t.Fatalf("tool error result = %#v", toolResult)
	}
}

func TestRunLoopFeedsInvalidArgumentsBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-review-invalid",
			reviewtool.ReviewSearchToolName,
			`{"limit":99}`,
		),
		finalLoopResult("Please tell me which review you want to find."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("找评价"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "Please tell me which review you want to find." {
		t.Fatalf("Content = %q", result.Content)
	}
	toolResult := generator.Requests()[1].Messages[3]
	if toolResult.ToolCallID != "call-review-invalid" ||
		!strings.Contains(toolResult.Content, `"category":"invalid_input"`) ||
		!strings.Contains(toolResult.Content, `"retryable":false`) {
		t.Fatalf("invalid input result = %#v", toolResult)
	}
}

func TestRunLoopReturnsFallbackWhenToolBudgetExhausted(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-1", reviewtool.ReviewSearchToolName, `{"query":"one"}`),
		toolLoopResult("call-2", reviewtool.ReviewSearchToolName, `{"query":"two"}`),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxToolCalls = 1

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("看看我面试评价"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if !strings.Contains(result.Content, "工具太多") {
		t.Fatalf("fallback content = %q", result.Content)
	}
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopRejectsUnexposedToolCall(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-1", "missing.search.v1", `{"query":"one"}`),
		finalLoopResult("That capability is not available."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("帮我润色这句话"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "That capability is not available." {
		t.Fatalf("Content = %q", result.Content)
	}
	toolResult := generator.Requests()[1].Messages[3]
	if toolResult.ToolCallID != "call-1" ||
		!strings.Contains(toolResult.Content, `"category":"unknown_tool"`) {
		t.Fatalf("unknown tool result = %#v", toolResult)
	}
}

func TestRunLoopStopsAfterToolIterationBudget(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-1", reviewtool.ReviewSearchToolName, `{"query":"one"}`),
		toolLoopResult("call-2", mocktool.MaterialSearchToolName, `{"query":"two"}`),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxIterations = 1

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("先找评价，再找材料"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if !strings.Contains(result.Content, "更多步骤") {
		t.Fatalf("fallback content = %q", result.Content)
	}
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopRejectsRepeatedToolCallIDBeforeSecondExecution(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-create-1",
			mattertool.ScenarioCreateToolName,
			`{"title":"First scenario"}`,
		),
		toolLoopResult(
			"call-create-1",
			mattertool.ScenarioCreateToolName,
			`{"title":"Repeated scenario"}`,
		),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxWriteToolCalls = 2

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("创建两个练习场景"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if !strings.Contains(result.Content, "重复提交") {
		t.Fatalf("fallback content = %q", result.Content)
	}
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopReplaysWriteToolWithStableIdempotencyID(t *testing.T) {
	store := mocktool.NewStore()
	runOnce := func() string {
		generator := newScriptedGenerator(
			toolLoopResult(
				"call-create-stable",
				mattertool.ScenarioCreateToolName,
				`{"title":"Stable scenario"}`,
			),
			finalLoopResult("Created."),
		)
		service := newLoopTestServiceWithStore(t, generator, store)
		if _, err := service.generate(
			context.Background(),
			loopActor(),
			loopRun(),
			ContextManifest{},
			loopRequest("创建面试场景"),
		); err != nil {
			t.Fatalf("generate() error = %v", err)
		}
		return generator.Requests()[1].Messages[3].Content
	}

	first := runOnce()
	replayed := runOnce()
	if first != replayed {
		t.Fatalf("idempotent Tool Result changed: first=%s replayed=%s", first, replayed)
	}
	if got, want := toolCallRequestID("run-1", "call-create-stable"),
		"run-1-call-create-stable"; got != want {
		t.Fatalf("toolCallRequestID() = %q, want %q", got, want)
	}
}

func TestRunLoopStopsAfterWriteBudget(t *testing.T) {
	generator := newScriptedGenerator(ai.TextResult{
		ID:           "fake-completion-tools",
		Provider:     "fake",
		Model:        "configured-model",
		FinishReason: "tool_calls",
		ToolCalls: []ai.ToolCall{
			{
				ID:        "call-create-1",
				Name:      mattertool.ScenarioCreateToolName,
				Arguments: json.RawMessage(`{"title":"PM interview"}`),
			},
			{
				ID:        "call-create-2",
				Name:      mattertool.ScenarioCreateToolName,
				Arguments: json.RawMessage(`{"title":"Client meeting"}`),
			},
		},
		Usage: ai.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	})
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("帮我创建一个英文 PM 面试练习场景"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if !strings.Contains(result.Content, "写操作上限") {
		t.Fatalf("fallback content = %q", result.Content)
	}
}

func TestPracticePreviewOnlyConsumesWriteBudgetWhenItCanCreatePlan(
	t *testing.T,
) {
	tests := []struct {
		name      string
		arguments string
		want      bool
	}{
		{
			name:      "candidate lookup",
			arguments: `{"scenario_query":"AI product manager interview"}`,
			want:      false,
		},
		{
			name: "ready plan input",
			arguments: `{"preparation_profile_id":"profile-1",` +
				`"max_effective_turns":3}`,
			want: true,
		},
		{
			name:      "invalid input fails closed",
			arguments: `{`,
			want:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := ai.ToolCall{
				Name:      "practice.preview.v1",
				Arguments: json.RawMessage(test.arguments),
			}
			if got := toolCallMayWrite(call); got != test.want {
				t.Fatalf("toolCallMayWrite() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRunLoopLogsEndToEndToolSequence(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	generator := newScriptedGenerator(
		toolLoopResult("call-review-1", reviewtool.ReviewSearchToolName, `{"query":"metrics","limit":1}`),
		finalLoopResult("I found the review and summarized it."),
	)
	service := newLoopTestService(t, generator)
	service.logger = logger
	service.logOptions = LogOptions{
		LogUserInput:    true,
		LogToolPayloads: true,
		InputPreviewMax: 64,
	}
	service.executor = tool.NewExecutorWithLogger(
		service.registry,
		logger,
		service.logOptions.LogToolPayloads,
	)

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest("看看我上次面试评价"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	output := logs.String()
	assertLogOrder(t, output, []string{
		"agent.run.received",
		"agent.tools.exposed",
		"agent.loop.iteration",
		"agent.routing.decision",
		"agent.tool.call.started",
		"agent.tool.call.succeeded",
		"agent.loop.iteration",
		"tool_call_then_response",
		"agent.run.completed",
	})
	for _, want := range []string{
		`"run_id":"run-1"`,
		`"thread_id":"thread-1"`,
		`"tool_call_id":"call-review-1"`,
		`"input_preview":"看看我上次面试评价"`,
		`"reason_summary"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("logs = %s, want %s", output, want)
		}
	}
}

func TestRunLoopLogOptionsAndPayloadSummariesDoNotLeakSensitiveContent(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	secretInput := "Authorization: Bearer token-123 Cookie: session=abc 简历正文请不要泄漏"
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-material-1",
			mocktool.MaterialSearchToolName,
			`{"query":"简历正文请不要泄漏 Bearer token-123","limit":1}`,
		),
	)
	service := newLoopTestService(t, generator)
	service.logger = logger
	service.logOptions = LogOptions{LogUserInput: false, LogToolPayloads: true}
	service.executor = tool.NewExecutorWithLogger(
		service.registry,
		logger,
		service.logOptions.LogToolPayloads,
	)

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		ContextManifest{},
		loopRequest(secretInput),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	output := logs.String()
	for _, leaked := range []string{
		"Bearer token-123",
		"session=abc",
		"简历正文请不要泄漏",
		"X-Amz-Signature",
		"oss.example.com",
	} {
		if strings.Contains(output, leaked) {
			t.Fatalf("logs leaked %q: %s", leaked, output)
		}
	}
	if strings.Contains(output, "input_preview") {
		t.Fatalf("input preview logged while disabled: %s", output)
	}
}

func TestToolSourceRefsKeepsEmptySliceForPersistence(t *testing.T) {
	refs := toolSourceRefs(nil)
	if refs == nil {
		t.Fatal("toolSourceRefs(nil) = nil, want empty slice")
	}
	if len(refs) != 0 {
		t.Fatalf("len(refs) = %d, want 0", len(refs))
	}
}

func TestValidLoopTextResultRejectsInvalidToolCalls(t *testing.T) {
	tests := map[string]ai.ToolCall{
		"invalid name": {
			ID:        "call-1",
			Name:      "review search",
			Arguments: json.RawMessage(`{"query":"last interview"}`),
		},
		"non-object arguments": {
			ID:        "call-1",
			Name:      reviewtool.ReviewSearchToolName,
			Arguments: json.RawMessage(`[]`),
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			result := ai.TextResult{
				ID:           "fake-completion-tools",
				Provider:     "fake",
				Model:        "configured-model",
				FinishReason: "tool_calls",
				ToolCalls:    []ai.ToolCall{call},
				Usage: ai.TokenUsage{
					InputTokens:  1,
					OutputTokens: 1,
					TotalTokens:  2,
				},
			}
			if validLoopTextResult(result) {
				t.Fatal("invalid tool call accepted")
			}
		})
	}
}

type scriptedGenerator struct {
	mu       sync.Mutex
	results  []ai.TextResult
	requests []ai.TextRequest
}

func newScriptedGenerator(results ...ai.TextResult) *scriptedGenerator {
	return &scriptedGenerator{results: results}
}

func (generator *scriptedGenerator) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	if err := ai.ValidateTextRequest(request); err != nil {
		return ai.TextResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ai.TextResult{}, err
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.requests = append(generator.requests, request)
	if len(generator.results) == 0 {
		return finalLoopResult("default"), nil
	}
	result := generator.results[0]
	generator.results = generator.results[1:]
	return result, nil
}

func (generator *scriptedGenerator) GenerateStream(
	context.Context,
	ai.TextRequest,
	ai.TextDeltaObserver,
) (ai.TextResult, error) {
	panic("non-streaming run unexpectedly selected GenerateStream")
}

func (generator *scriptedGenerator) Requests() []ai.TextRequest {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	requests := make([]ai.TextRequest, len(generator.requests))
	copy(requests, generator.requests)
	return requests
}

func (generator *scriptedGenerator) CallCount() int {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	return len(generator.requests)
}

func newLoopTestService(
	t *testing.T,
	generator ai.TextGenerator,
) *RunService {
	t.Helper()
	return newLoopTestServiceWithStore(t, generator, mocktool.NewStore())
}

func newLoopTestServiceWithStore(
	t *testing.T,
	generator ai.TextGenerator,
	store *mocktool.Store,
) *RunService {
	t.Helper()
	registry, err := mocktool.NewRegistry(store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return &RunService{
		generator: generator,
		configuration: RunConfiguration{
			Provider:           "fake",
			Model:              "configured-model",
			MaxOutputTokens:    512,
			MaxInputCharacters: 12000,
		},
		registry:   registry,
		executor:   tool.NewExecutor(registry),
		loopLimits: normalizeLoopLimits(LoopLimits{LoopTimeout: time.Second}),
	}
}

func loopActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
}

func loopRun() Run {
	return Run{ID: "run-1", OwnerID: "user-1", ThreadID: "thread-1"}
}

func loopRequest(input string) ai.TextRequest {
	return ai.TextRequest{Messages: []ai.TextMessage{
		{Role: ai.TextRoleSystem, Content: "You are SpeakUp."},
		{Role: ai.TextRoleUser, Content: input},
	}}
}

func finalLoopResult(content string) ai.TextResult {
	return ai.TextResult{
		ID:           "fake-completion-1",
		Provider:     "fake",
		Model:        "configured-model",
		Content:      content,
		FinishReason: "stop",
		Usage:        ai.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}
}

func toolLoopResult(id string, name string, arguments string) ai.TextResult {
	var raw json.RawMessage = []byte(arguments)
	return ai.TextResult{
		ID:           "fake-completion-tools",
		Provider:     "fake",
		Model:        "configured-model",
		FinishReason: "tool_calls",
		ToolCalls: []ai.ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: raw,
		}},
		Usage: ai.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}
}

func assertLogOrder(t *testing.T, output string, events []string) {
	t.Helper()
	previous := -1
	for _, event := range events {
		index := strings.Index(output[previous+1:], event)
		if index < 0 {
			t.Fatalf("logs = %s, missing %s", output, event)
		}
		index += previous + 1
		if index <= previous {
			t.Fatalf("logs = %s, %s appeared out of order", output, event)
		}
		previous = index
	}
}
