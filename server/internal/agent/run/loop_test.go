package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	evaluationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/agentcapability"
	goalcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/goal/agentcapability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

func TestRunLoopExposesAllToolsAndAllowsDirectResponse(t *testing.T) {
	generator := newScriptedGenerator(finalLoopResult("direct-answer"))
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
		goalcapability.GoalCreateCapabilityName,
		goalcapability.GoalSearchCapabilityName,
		capabilityfixture.MaterialSearchToolName,
		capabilityfixture.MistakeSearchToolName,
		reviewcapability.ReviewGetToolName,
		reviewcapability.ReviewSearchToolName,
	}
	if !reflect.DeepEqual(gotTools, wantTools) {
		t.Fatalf("Tools = %#v, want %#v", gotTools, wantTools)
	}
	if requests[0].ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf("ToolChoice = %#v, want auto", requests[0].ToolChoice)
	}
}

func TestRunLoopExecutesToolCallAndFeedsResultBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-review-1", reviewcapability.ReviewSearchToolName, `{"query":"metrics","limit":1}`),
		finalLoopResult("I found the review and summarized it."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
	if requests[0].ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf("first ToolChoice = %#v, want auto", requests[0].ToolChoice)
	}
	second := requests[1]
	if second.ToolChoice.Mode != ToolChoiceAuto {
		t.Fatalf("second ToolChoice = %#v, want auto", second.ToolChoice)
	}
	if got, want := len(second.Messages), 4; got != want {
		t.Fatalf("second request messages = %d, want %d", got, want)
	}
	toolMessage := second.Messages[len(second.Messages)-1]
	if toolMessage.Role != TextRoleTool ||
		toolMessage.ToolCallID != "call-review-1" ||
		!strings.Contains(toolMessage.Content, `"reports"`) {
		t.Fatalf("tool message = %#v", toolMessage)
	}
}

func TestRunLoopKeepsSourceRefsOutOfProviderMessagesAndInAudit(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-sensitive-source-1",
			loopSensitiveSourceToolName,
			`{}`,
		),
		finalLoopResult("The audited capability completed."),
	)
	service := newLoopTestService(t, generator)
	setLoopTools(
		t,
		service,
		capabilityfixture.NewStore(),
		loopSensitiveSourceTool{},
	)
	audit := &loopSourceRefRepository{}
	service.repository = audit

	if _, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("run the audited capability"),
	); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	toolMessage := requests[1].Messages[len(requests[1].Messages)-1]
	if toolMessage.Role != TextRoleTool ||
		toolMessage.ToolCallID != "call-sensitive-source-1" ||
		!strings.Contains(toolMessage.Content, `"status":"ready"`) {
		t.Fatalf("tool message = %#v", toolMessage)
	}
	for _, forbidden := range []string{
		"source_refs",
		"handoffs",
		"confirm_practice_plan",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"Confirm this exact practice plan.",
		"preparation_snapshot",
		"snapshot-internal-1",
		"preparation_profile",
		"profile-internal-1",
		"voice_config",
		"config-internal-1",
	} {
		if strings.Contains(toolMessage.Content, forbidden) {
			t.Fatalf("provider tool message leaked %q: %s", forbidden, toolMessage.Content)
		}
	}
	wantRefs := []ToolSourceRef{
		{Type: "preparation_snapshot", ID: "snapshot-internal-1"},
		{Type: "preparation_profile", ID: "profile-internal-1"},
		{Type: "voice_config", ID: "config-internal-1"},
	}
	if !reflect.DeepEqual(audit.sourceRefs, wantRefs) {
		t.Fatalf("persisted SourceRefs = %#v, want %#v", audit.sourceRefs, wantRefs)
	}
	wantHandoffs := []agenthandoff.Item{loopPracticeHandoff()}
	if !reflect.DeepEqual(audit.handoffs, wantHandoffs) {
		t.Fatalf("persisted Handoffs = %#v, want %#v", audit.handoffs, wantHandoffs)
	}
}

func TestRunLoopLetsModelSelectLatestReportCapability(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-latest-report-1",
			evaluationcapability.LatestPracticeReportToolName,
			`{}`,
		),
		finalLoopResult("Here is your latest practice feedback."),
	)
	service := newLoopTestService(t, generator)
	store := capabilityfixture.NewStore()
	registry, err := capability.NewRegistry(append(
		capabilityfixture.Tools(store),
		evaluationcapability.NewLatestPracticeReportTool(loopLatestReportPort{}),
	)...)
	if err != nil {
		t.Fatalf("capability.NewRegistry() error = %v", err)
	}
	service.registry = registry
	service.executor = capability.NewExecutor(registry)

	input := "我刚完成了面试练习。请直接读取这次练习的真实评分与报告。"
	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest(input),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "Here is your latest practice feedback." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	initial := requests[0]
	if initial.ToolChoice.Mode != ToolChoiceAuto ||
		!toolExposed(
			exposedToolNames(initial.Tools),
			evaluationcapability.LatestPracticeReportToolName,
		) {
		t.Fatalf(
			"initial routing = choice %#v, tools %#v",
			initial.ToolChoice,
			exposedToolNameList(initial.Tools),
		)
	}
	if got, want := len(initial.Messages), 2; got != want ||
		initial.Messages[1].Role != TextRoleUser ||
		initial.Messages[1].Content != input {
		t.Fatalf("initial messages = %#v, want original input", initial.Messages)
	}
	messages := requests[1].Messages
	if got, want := len(messages), 4; got != want {
		t.Fatalf("second request messages = %d, want %d", got, want)
	}
	assistant := messages[2]
	toolResult := messages[3]
	if len(assistant.ToolCalls) != 1 ||
		assistant.ToolCalls[0].ID != "call-latest-report-1" ||
		assistant.ToolCalls[0].Name != evaluationcapability.LatestPracticeReportToolName ||
		toolResult.Role != TextRoleTool ||
		toolResult.ToolCallID != "call-latest-report-1" ||
		!strings.Contains(toolResult.Content, `"practice_report"`) {
		t.Fatalf(
			"latest report messages = assistant %#v, tool %#v",
			assistant,
			toolResult,
		)
	}
}

type loopLatestReportPort struct{}

func (loopLatestReportPort) LatestPracticeReport(
	context.Context,
	capability.CallContext,
) (evaluationcapability.LatestPracticeReport, error) {
	return evaluationcapability.LatestPracticeReport{
		Scene:          "面试英语",
		AssessmentMode: "评分与反馈",
	}, nil
}

func TestRunLoopExecutesMultipleToolCallsAndFeedsAllResultsBack(t *testing.T) {
	generator := newScriptedGenerator(
		TextResult{
			ID:           "fake-completion-tools",
			Provider:     "fake",
			Model:        "configured-model",
			FinishReason: "tool_calls",
			ToolCalls: []ModelToolCall{
				{
					ID:        "call-review-1",
					Name:      reviewcapability.ReviewSearchToolName,
					Arguments: json.RawMessage(`{"query":"first","limit":1}`),
				},
				{
					ID:        "call-review-2",
					Name:      reviewcapability.ReviewSearchToolName,
					Arguments: json.RawMessage(`{"query":"second","limit":1}`),
				},
			},
			Usage: TokenUsage{
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
		agentcontext.Manifest{},
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
	if assistant.Role != TextRoleAssistant ||
		len(assistant.ToolCalls) != 2 {
		t.Fatalf("assistant tool calls = %#v", assistant)
	}
	for index, callID := range []string{"call-review-1", "call-review-2"} {
		message := messages[index+3]
		if message.Role != TextRoleTool ||
			message.ToolCallID != callID ||
			!strings.Contains(message.Content, `"reports"`) {
			t.Fatalf("tool message %d = %#v", index, message)
		}
	}
}

func TestRunLoopSupportsConsecutiveToolRoundsBeforeFinalResponse(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-review-1",
			reviewcapability.ReviewSearchToolName,
			`{"query":"metrics","limit":1}`,
		),
		toolLoopResult(
			"call-material-1",
			capabilityfixture.MaterialSearchToolName,
			`{"query":"backend","limit":1}`,
		),
		finalLoopResult("I combined the review with your resume."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
		agentcontext.Manifest{},
		loopRequest("帮我找一下上次 PM interview 的 review"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "made-up review" {
		t.Fatalf("Content = %q, want model response", result.Content)
	}
}

func TestRunLoopTreatsSlashPrefixedTextAsNaturalLanguage(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-review-from-model",
			reviewcapability.ReviewSearchToolName,
			`{"query":"last interview","limit":1}`,
		),
		finalLoopResult("I found your review."),
	)
	service := newLoopTestService(t, generator)

	input := "/查评价 last interview"
	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest(input),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if result.Content != "I found your review." {
		t.Fatalf("Content = %q", result.Content)
	}
	requests := generator.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
	initial := requests[0]
	if initial.ToolChoice.Mode != ToolChoiceAuto || len(initial.Tools) != 6 {
		t.Fatalf(
			"initial routing = choice %#v, tools %d",
			initial.ToolChoice,
			len(initial.Tools),
		)
	}
	if got, want := len(initial.Messages), 2; got != want ||
		initial.Messages[1].Role != TextRoleUser ||
		initial.Messages[1].Content != input {
		t.Fatalf("initial messages = %#v, want original input", initial.Messages)
	}
	messages := requests[1].Messages
	if got, want := len(messages), 4; got != want ||
		messages[3].ToolCallID != "call-review-from-model" {
		t.Fatalf("model-selected tool chain = %#v", messages)
	}
}

func TestRunLoopFeedsExplicitCapabilityFailureBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-material-1", capabilityfixture.MaterialSearchToolName, `{"query":"backend"}`),
		finalLoopResult("I could not read the material, so I will continue without it."),
	)
	store := capabilityfixture.NewStore()
	store.SetUnavailable(capabilityfixture.MaterialSearchToolName, true)
	service := newLoopTestServiceWithStore(t, generator, store)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
	if toolResult.Role != TextRoleTool ||
		toolResult.ToolCallID != "call-material-1" ||
		!strings.Contains(toolResult.Content, `"category":"internal"`) ||
		!strings.Contains(toolResult.Content, `"retryable":true`) {
		t.Fatalf("tool error result = %#v", toolResult)
	}
}

func TestRunLoopReturnsExplicitModelFailure(t *testing.T) {
	want := NewGenerationError(
		ErrorProviderUnavailable,
		0,
		"",
		"",
		errors.New("provider unavailable"),
	)
	service := newLoopTestService(
		t,
		&failingTextGenerator{err: want},
	)

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("请帮我继续准备面试"),
	)
	var generationError *GenerationError
	if !errors.As(err, &generationError) ||
		generationError.Kind != ErrorProviderUnavailable {
		t.Fatalf("generate() error = %#v, want provider unavailable", err)
	}
}

func TestRunLoopFeedsInvalidArgumentsBackToModel(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-review-invalid",
			reviewcapability.ReviewSearchToolName,
			`{"limit":99}`,
		),
		finalLoopResult("Please tell me which review you want to find."),
	)
	service := newLoopTestService(t, generator)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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

func TestRunLoopFailsWhenToolBudgetExhausted(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-1", reviewcapability.ReviewSearchToolName, `{"query":"one"}`),
		toolLoopResult("call-2", reviewcapability.ReviewSearchToolName, `{"query":"two"}`),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxToolCalls = 1

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("看看我面试评价"),
	)
	assertLoopFailure(t, result, err, FailureToolCallBudgetExhausted)
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
		agentcontext.Manifest{},
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

func TestRunLoopFailsAfterToolIterationBudget(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-1", reviewcapability.ReviewSearchToolName, `{"query":"one"}`),
		toolLoopResult("call-2", capabilityfixture.MaterialSearchToolName, `{"query":"two"}`),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxIterations = 1

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("先找评价，再找材料"),
	)
	assertLoopFailure(t, result, err, FailureToolIterationBudgetExhausted)
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopFailsRepeatedToolCallIDBeforeSecondExecution(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-create-1",
			goalcapability.GoalCreateCapabilityName,
			`{"title":"First goal"}`,
		),
		toolLoopResult(
			"call-create-1",
			goalcapability.GoalCreateCapabilityName,
			`{"title":"Repeated goal"}`,
		),
	)
	service := newLoopTestService(t, generator)
	service.loopLimits.MaxWriteToolCalls = 2

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("创建两个练习场景"),
	)
	assertLoopFailure(t, result, err, FailureDuplicateToolCall)
	if got, want := generator.CallCount(), 2; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopReplaysWriteToolWithStableIdempotencyID(t *testing.T) {
	store := capabilityfixture.NewStore()
	runOnce := func() string {
		generator := newScriptedGenerator(
			toolLoopResult(
				"call-create-stable",
				goalcapability.GoalCreateCapabilityName,
				`{"title":"Stable goal"}`,
			),
			finalLoopResult("Created."),
		)
		service := newLoopTestServiceWithStore(t, generator, store)
		if _, err := service.generate(
			context.Background(),
			loopActor(),
			loopRun(),
			agentcontext.Manifest{},
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

func TestRunLoopFailsBeforeExecutingBatchOverWriteBudget(t *testing.T) {
	store := capabilityfixture.NewStore()
	firstTitle := "write-budget-first-unique"
	secondTitle := "write-budget-second-unique"
	generator := newScriptedGenerator(TextResult{
		ID:           "fake-completion-tools",
		Provider:     "fake",
		Model:        "configured-model",
		FinishReason: "tool_calls",
		ToolCalls: []ModelToolCall{
			{
				ID:        "call-create-1",
				Name:      goalcapability.GoalCreateCapabilityName,
				Arguments: json.RawMessage(`{"title":"` + firstTitle + `"}`),
			},
			{
				ID:        "call-create-2",
				Name:      goalcapability.GoalCreateCapabilityName,
				Arguments: json.RawMessage(`{"title":"` + secondTitle + `"}`),
			},
		},
		Usage: TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	})
	service := newLoopTestServiceWithStore(t, generator, store)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("帮我创建一个英文 PM 面试练习场景"),
	)
	assertLoopFailure(t, result, err, FailureWriteToolCallBudgetExhausted)
	assertGoalNotCreated(t, store, firstTitle)
	assertGoalNotCreated(t, store, secondTitle)
}

func TestRunLoopQueriesThenExecutesOneConditionalWrite(t *testing.T) {
	store := capabilityfixture.NewStore()
	conditional := &loopConditionalTool{}
	generator := newScriptedGenerator(
		toolLoopResult(
			"call-conditional-query",
			loopConditionalToolName,
			`{}`,
		),
		toolLoopResult(
			"call-conditional-write",
			loopConditionalToolName,
			`{"write_value":"persisted-value"}`,
		),
		finalLoopResult("The conditional write completed."),
	)
	service := newLoopTestServiceWithStore(t, generator, store)
	setLoopTools(t, service, store, conditional)

	result, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
		loopRequest("先查询，再执行一次条件写入"),
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if got, want := result.Content, "The conditional write completed."; got != want {
		t.Fatalf("Content = %q, want %q", got, want)
	}
	if got, want := len(conditional.inputs), 2; got != want {
		t.Fatalf("conditional calls = %d, want %d", got, want)
	}
	if conditional.inputs[0].WriteValue != "" ||
		conditional.inputs[1].WriteValue == "" {
		t.Fatalf("conditional inputs = %#v", conditional.inputs)
	}
	if got, want := generator.CallCount(), 3; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
	}
}

func TestRunLoopReservesWriteBudgetForRejectedInvocations(t *testing.T) {
	tests := []struct {
		name string
		call TextResult
	}{
		{
			name: "unknown tool",
			call: toolLoopResult(
				"call-unknown-write",
				"missing.write.v1",
				`{}`,
			),
		},
		{
			name: "schema invalid conditional tool",
			call: toolLoopResult(
				"call-invalid-conditional",
				loopConditionalToolName,
				`{"write_value":0}`,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := capabilityfixture.NewStore()
			conditional := &loopConditionalTool{}
			title := "write-after-rejected-" + strings.ReplaceAll(test.name, " ", "-")
			generator := newScriptedGenerator(
				test.call,
				toolLoopResult(
					"call-create-after-rejected",
					goalcapability.GoalCreateCapabilityName,
					`{"title":"`+title+`"}`,
				),
			)
			service := newLoopTestServiceWithStore(t, generator, store)
			setLoopTools(t, service, store, conditional)

			result, err := service.generate(
				context.Background(),
				loopActor(),
				loopRun(),
				agentcontext.Manifest{},
				loopRequest("attempt a guarded write"),
			)
			assertLoopFailure(t, result, err, FailureWriteToolCallBudgetExhausted)
			if got, want := generator.CallCount(), 2; got != want {
				t.Fatalf("Generate calls = %d, want %d", got, want)
			}
			if len(conditional.inputs) != 0 {
				t.Fatalf("rejected invocation reached tool: %#v", conditional.inputs)
			}
			assertGoalNotCreated(t, store, title)
		})
	}
}

func TestRunLoopLogsEndToEndToolSequence(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	generator := newScriptedGenerator(
		toolLoopResult("call-review-1", reviewcapability.ReviewSearchToolName, `{"query":"metrics","limit":1}`),
		finalLoopResult("I found the review and summarized it."),
	)
	service := newLoopTestService(t, generator)
	service.logger = logger
	service.logOptions = LogOptions{
		LogUserInput:    true,
		LogToolPayloads: true,
		InputPreviewMax: 64,
	}
	service.executor = capability.NewExecutorWithLogger(
		service.registry,
		logger,
		service.logOptions.LogToolPayloads,
	)

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
			capabilityfixture.MaterialSearchToolName,
			`{"query":"简历正文请不要泄漏 Bearer token-123","limit":1}`,
		),
	)
	service := newLoopTestService(t, generator)
	service.logger = logger
	service.logOptions = LogOptions{LogUserInput: false, LogToolPayloads: true}
	service.executor = capability.NewExecutorWithLogger(
		service.registry,
		logger,
		service.logOptions.LogToolPayloads,
	)

	_, err := service.generate(
		context.Background(),
		loopActor(),
		loopRun(),
		agentcontext.Manifest{},
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
	tests := map[string]ModelToolCall{
		"invalid name": {
			ID:        "call-1",
			Name:      "review search",
			Arguments: json.RawMessage(`{"query":"last interview"}`),
		},
		"non-object arguments": {
			ID:        "call-1",
			Name:      reviewcapability.ReviewSearchToolName,
			Arguments: json.RawMessage(`[]`),
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			result := TextResult{
				ID:           "fake-completion-tools",
				Provider:     "fake",
				Model:        "configured-model",
				FinishReason: "tool_calls",
				ToolCalls:    []ModelToolCall{call},
				Usage: TokenUsage{
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

const loopConditionalToolName = "conditional.write.v1"

const loopSensitiveSourceToolName = "sensitive.source.read.v1"

type loopSensitiveSourceTool struct{}

func (loopSensitiveSourceTool) Definition() capability.Definition {
	return capability.Definition{
		Name:        loopSensitiveSourceToolName,
		Description: "Return public content with internal audit source references.",
		InputSchema: capability.ObjectSchema(map[string]any{}, nil),
		ReadOnly:    true,
		Risk:        capability.RiskReadOnly,
	}
}

func (loopSensitiveSourceTool) Execute(
	context.Context,
	capability.CallContext,
	json.RawMessage,
) (capability.Result, error) {
	return capability.Result{
		Content: map[string]any{"status": "ready"},
		SourceRefs: []capability.SourceRef{
			{Type: "preparation_snapshot", ID: "snapshot-internal-1"},
			{Type: "preparation_profile", ID: "profile-internal-1"},
			{Type: "voice_config", ID: "config-internal-1"},
		},
		Handoffs: []agenthandoff.Item{loopPracticeHandoff()},
	}, nil
}

func loopPracticeHandoff() agenthandoff.Item {
	return agenthandoff.Item{
		Type:                     agenthandoff.ConfirmPracticePlanType,
		Label:                    "Confirm practice",
		PracticePlanID:           "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		PlanRevision:             2,
		Target:                   "Backend interview",
		SceneName:                "Project deep dive",
		PracticeExperience:       "INTERVIEW",
		SceneCategory:            "INTERVIEW_PROFESSIONAL",
		PracticeMode:             "FULL_SIMULATION",
		Roles:                    []string{"Technical interviewer"},
		PracticeScope:            "Full simulation",
		SuggestedDurationSeconds: 600,
		MinEffectiveTurns:        3,
		MaxEffectiveTurns:        5,
		ExecutableStatus:         agenthandoff.PracticePlanReadyStatus,
		ConfirmationPrompt:       "Confirm this exact practice plan.",
	}
}

type loopConditionalInput struct {
	WriteValue string `json:"write_value,omitempty"`
}

type loopConditionalTool struct {
	inputs []loopConditionalInput
}

func (conditional *loopConditionalTool) Definition() capability.Definition {
	return capability.Definition{
		Name:        loopConditionalToolName,
		Description: "Query without input or perform one conditional write.",
		InputSchema: capability.ObjectSchema(map[string]any{
			"write_value": capability.TextSchema("Value to persist.", 100),
		}, nil),
		ReadOnly: false,
		Risk:     capability.RiskLowRiskWrite,
	}
}

func (conditional *loopConditionalTool) ClassifyInvocationEffect(
	input json.RawMessage,
) (capability.InvocationEffect, error) {
	parsed, err := parseLoopConditionalInput(input)
	if err != nil {
		return 0, err
	}
	if parsed.WriteValue != "" {
		return capability.InvocationEffectMayWrite, nil
	}
	return capability.InvocationEffectReadOnly, nil
}

func (conditional *loopConditionalTool) Execute(
	_ context.Context,
	_ capability.CallContext,
	input json.RawMessage,
) (capability.Result, error) {
	parsed, err := parseLoopConditionalInput(input)
	if err != nil {
		return capability.Result{}, err
	}
	conditional.inputs = append(conditional.inputs, parsed)
	return capability.Result{Content: map[string]any{"ok": true}}, nil
}

func parseLoopConditionalInput(
	input json.RawMessage,
) (loopConditionalInput, error) {
	var parsed loopConditionalInput
	if err := json.Unmarshal(input, &parsed); err != nil {
		return loopConditionalInput{}, capability.ErrInvalidInput
	}
	return parsed, nil
}

func setLoopTools(
	t *testing.T,
	service *Service,
	store *capabilityfixture.Store,
	extra ...capability.Tool,
) {
	t.Helper()
	items := append(capabilityfixture.Tools(store), extra...)
	registry, err := capability.NewRegistry(items...)
	if err != nil {
		t.Fatalf("capability.NewRegistry() error = %v", err)
	}
	service.registry = registry
	service.executor = capability.NewExecutor(registry)
}

func assertGoalNotCreated(
	t *testing.T,
	store *capabilityfixture.Store,
	title string,
) {
	t.Helper()
	items, err := store.SearchGoals(
		context.Background(),
		capability.CallContext{},
		goalcapability.GoalSearchInput{Query: title},
	)
	if err != nil {
		t.Fatalf("SearchGoals() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("goal %q was created before batch rejection: %#v", title, items)
	}
}

func assertLoopFailure(
	t *testing.T,
	result TextResult,
	err error,
	wantKind string,
) {
	t.Helper()
	if err == nil {
		t.Fatal("generate() error = nil, want explicit loop failure")
	}
	kind, retryable := classifyRunFailure(err)
	if kind != wantKind || retryable {
		t.Fatalf(
			"classifyRunFailure() = (%q, %t), want (%q, false)",
			kind,
			retryable,
			wantKind,
		)
	}
	if result.ID != "" || result.Provider != "" || result.Model != "" ||
		result.Content != "" || len(result.ToolCalls) != 0 ||
		result.FinishReason != "" || result.Usage != (TokenUsage{}) {
		t.Fatalf("loop failure returned model result = %#v", result)
	}
}

type scriptedGenerator struct {
	mu       sync.Mutex
	results  []TextResult
	requests []TextRequest
}

type failingTextGenerator struct {
	err error
}

func (generator *failingTextGenerator) Generate(
	context.Context,
	TextRequest,
) (TextResult, error) {
	return TextResult{}, generator.err
}

func newScriptedGenerator(results ...TextResult) *scriptedGenerator {
	return &scriptedGenerator{results: results}
}

func (generator *scriptedGenerator) Generate(
	ctx context.Context,
	request TextRequest,
) (TextResult, error) {
	if err := ValidateTextRequest(request); err != nil {
		return TextResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return TextResult{}, err
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
	TextRequest,
	TextDeltaObserver,
) (TextResult, error) {
	panic("non-streaming run unexpectedly selected GenerateStream")
}

func (generator *scriptedGenerator) Requests() []TextRequest {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	requests := make([]TextRequest, len(generator.requests))
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
	generator TextGenerator,
) *Service {
	t.Helper()
	return newLoopTestServiceWithStore(t, generator, capabilityfixture.NewStore())
}

func newLoopTestServiceWithStore(
	t *testing.T,
	generator TextGenerator,
	store *capabilityfixture.Store,
) *Service {
	t.Helper()
	registry, err := capabilityfixture.NewRegistry(store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return &Service{
		repository: loopRepository{},
		manifests:  loopManifestRepository{},
		generator:  generator,
		configuration: Configuration{
			Provider:           "fake",
			Model:              "configured-model",
			MaxOutputTokens:    512,
			MaxInputCharacters: 12000,
		},
		registry:   registry,
		executor:   capability.NewExecutor(registry),
		loopLimits: normalizeLoopLimits(LoopLimits{LoopTimeout: time.Second}),
	}
}

type loopRepository struct{}

type loopSourceRefRepository struct {
	loopRepository
	sourceRefs []ToolSourceRef
	handoffs   []agenthandoff.Item
}

func (repository *loopSourceRefRepository) CompleteToolCall(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ json.RawMessage,
	sourceRefs []ToolSourceRef,
	handoffs []agenthandoff.Item,
) (ToolCall, error) {
	repository.sourceRefs = append([]ToolSourceRef(nil), sourceRefs...)
	repository.handoffs = agenthandoff.CloneItems(handoffs)
	return ToolCall{
		SourceRefs: repository.sourceRefs,
		Handoffs:   repository.handoffs,
	}, nil
}

func (loopRepository) CreateInitial(
	context.Context,
	string,
	string,
	string,
	string,
	Configuration,
) (Submission, error) {
	panic("unexpected CreateInitial")
}

func (loopRepository) CreateRetry(
	context.Context,
	string,
	string,
	string,
	Configuration,
) (Retry, error) {
	panic("unexpected CreateRetry")
}

func (loopRepository) Claim(context.Context, string, string) (Run, bool, error) {
	panic("unexpected Claim")
}

func (loopRepository) Find(context.Context, string, string) (Run, error) {
	panic("unexpected Find")
}

func (loopRepository) ProposeToolCall(
	_ context.Context,
	call ToolCall,
) (ToolCall, error) {
	return call, nil
}

func (loopRepository) StartToolCall(
	context.Context,
	string,
	string,
	string,
	string,
) (ToolCall, error) {
	return ToolCall{}, nil
}

func (loopRepository) CompleteToolCall(
	context.Context,
	string,
	string,
	string,
	json.RawMessage,
	[]ToolSourceRef,
	[]agenthandoff.Item,
) (ToolCall, error) {
	return ToolCall{}, nil
}

func (loopRepository) FailToolCall(
	context.Context,
	string,
	string,
	string,
	ToolCallStatus,
	string,
) (ToolCall, error) {
	return ToolCall{}, nil
}

func (loopRepository) ListToolCalls(context.Context, string, string) ([]ToolCall, error) {
	panic("unexpected ListToolCalls")
}

func (loopRepository) Complete(
	context.Context,
	string,
	string,
	string,
	Completion,
) (Run, error) {
	panic("unexpected Complete")
}

func (loopRepository) Fail(
	context.Context,
	string,
	string,
	string,
	string,
	bool,
) (Run, error) {
	panic("unexpected Fail")
}

func (loopRepository) RecoverInterrupted(context.Context) (int64, error) {
	panic("unexpected RecoverInterrupted")
}

type loopManifestRepository struct{}

func (loopManifestRepository) SaveManifest(
	context.Context,
	agentcontext.Manifest,
) (agentcontext.Manifest, error) {
	panic("unexpected SaveManifest")
}

func (loopManifestRepository) FindManifest(
	context.Context,
	string,
	string,
) (agentcontext.Manifest, error) {
	panic("unexpected FindManifest")
}

func (loopManifestRepository) SaveToolSnapshot(
	_ context.Context,
	manifest agentcontext.Manifest,
) (agentcontext.Manifest, error) {
	return manifest, nil
}

func loopActor() requestcontext.Actor {
	return requestcontext.Actor{UserID: "user-1", SessionID: "session-1"}
}

func loopRun() Run {
	return Run{ID: "run-1", OwnerID: "user-1", ThreadID: "thread-1"}
}

func loopRequest(input string) TextRequest {
	return TextRequest{Messages: []TextMessage{
		{Role: TextRoleSystem, Content: "You are SpeakUp."},
		{Role: TextRoleUser, Content: input},
	}}
}

func finalLoopResult(content string) TextResult {
	return TextResult{
		ID:           "fake-completion-1",
		Provider:     "fake",
		Model:        "configured-model",
		Content:      content,
		FinishReason: "stop",
		Usage:        TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}
}

func toolLoopResult(id string, name string, arguments string) TextResult {
	var raw json.RawMessage = []byte(arguments)
	return TextResult{
		ID:           "fake-completion-tools",
		Provider:     "fake",
		Model:        "configured-model",
		FinishReason: "tool_calls",
		ToolCalls: []ModelToolCall{{
			ID:        id,
			Name:      name,
			Arguments: raw,
		}},
		Usage: TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
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
