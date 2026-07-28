package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

func TestRunLoopDirectResponseDoesNotExposeTools(t *testing.T) {
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
	if got := len(requests[0].Tools); got != 0 {
		t.Fatalf("len(Tools) = %d, want 0", got)
	}
	if requests[0].ToolChoice.Mode != ai.ToolChoiceNone {
		t.Fatalf("ToolChoice = %#v, want none", requests[0].ToolChoice)
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
		loopRequest("看看我面试评价"),
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
	if requests[0].ToolChoice.Mode != ai.ToolChoiceSpecific ||
		requests[0].ToolChoice.Name != reviewtool.ReviewSearchToolName {
		t.Fatalf("first ToolChoice = %#v, want review.search specific", requests[0].ToolChoice)
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

func TestRunLoopReturnsFallbackWhenSpecificToolChoiceIsIgnored(t *testing.T) {
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
	if !strings.Contains(result.Content, "需要先查询") {
		t.Fatalf("fallback content = %q", result.Content)
	}
}

func TestRunLoopReturnsFallbackWhenToolRejected(t *testing.T) {
	generator := newScriptedGenerator(
		toolLoopResult("call-material-1", mocktool.MaterialSearchToolName, `{"query":"backend"}`),
	)
	store := mocktool.NewStore()
	store.SetForbidden(mocktool.MaterialSearchToolName, true)
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
	if !strings.Contains(result.Content, "工具执行失败") {
		t.Fatalf("fallback content = %q", result.Content)
	}
	if got, want := generator.CallCount(), 1; got != want {
		t.Fatalf("Generate calls = %d, want %d", got, want)
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
		toolLoopResult("call-1", reviewtool.ReviewSearchToolName, `{"query":"one"}`),
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
	if !strings.Contains(result.Content, "没有开放") {
		t.Fatalf("fallback content = %q", result.Content)
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
				Arguments: json.RawMessage(`{"type":"interview","title":"PM interview"}`),
			},
			{
				ID:        "call-create-2",
				Name:      mattertool.ScenarioCreateToolName,
				Arguments: json.RawMessage(`{"type":"meeting","title":"Client meeting"}`),
			},
		},
		Usage: ai.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	})
	service := newLoopTestService(t, generator)
	service.confirmed = func(
		context.Context,
		requestcontext.Actor,
		Run,
	) []string {
		return []string{mattertool.ScenarioCreateToolName}
	}

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
		"agent.intent.guarded",
		"agent.routing.candidates",
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
