package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/capability"
	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	reviewcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/review/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/test/agent/capabilityfixture"
)

const DatasetVersion = "agent-routing-eval-v7"

type EvaluationResult struct {
	DatasetVersion      string
	Total               int
	Passed              int
	CoreRoutingAccuracy float64
	DirectMisrouteRate  float64
	WriteMisrouteRate   float64
	CaseResults         []CaseResult
}

type CaseResult struct {
	Name          string
	Passed        bool
	Decision      string
	ToolNames     []string
	ToolInputs    map[string]map[string]any
	PreviewInputs []PreviewInputRecord
	AllowedTools  []string
	ErrorCategory string
	Failures      []string
}

type Evaluator struct {
	registry *capability.Registry
	executor *capability.Executor
	router   DeterministicRouter
	preview  *routingPorts
}

func NewEvaluator() (*Evaluator, error) {
	registry, preview, err := newEvaluationRegistry()
	if err != nil {
		return nil, err
	}
	return &Evaluator{
		registry: registry,
		executor: capability.NewExecutor(registry),
		router:   DeterministicRouter{},
		preview:  preview,
	}, nil
}

func (e *Evaluator) Evaluate(
	ctx context.Context,
	cases []RoutingCase,
) (EvaluationResult, error) {
	if e == nil || e.registry == nil || e.executor == nil {
		return EvaluationResult{}, errors.New("agent eval: evaluator is invalid")
	}
	result := EvaluationResult{
		DatasetVersion: DatasetVersion,
		Total:          len(cases),
		CaseResults:    make([]CaseResult, 0, len(cases)),
	}
	writeTools := registeredWriteToolNames(e.registry)
	var directCases, directMisroutes, writeCases, writeMisroutes int
	for index, item := range cases {
		caseResult, err := e.evaluateCase(ctx, index, item)
		if err != nil {
			return EvaluationResult{}, err
		}
		if caseResult.Passed {
			result.Passed++
		}
		if item.ExpectedDecision == DecisionDirect {
			directCases++
			if len(caseResult.ToolNames) > 0 {
				directMisroutes++
			}
		}
		if intersects(item.ForbiddenTools, writeTools) {
			writeCases++
			if intersects(caseResult.ToolNames, writeTools) {
				writeMisroutes++
			}
		}
		result.CaseResults = append(result.CaseResults, caseResult)
	}
	if result.Total > 0 {
		result.CoreRoutingAccuracy = float64(result.Passed) / float64(result.Total)
	}
	if directCases > 0 {
		result.DirectMisrouteRate = float64(directMisroutes) / float64(directCases)
	}
	if writeCases > 0 {
		result.WriteMisrouteRate = float64(writeMisroutes) / float64(writeCases)
	}
	return result, nil
}

func (e *Evaluator) evaluateCase(
	ctx context.Context,
	index int,
	item RoutingCase,
) (CaseResult, error) {
	actor := requestcontext.Actor{UserID: "eval-user", SessionID: "eval-session"}
	runID := fmt.Sprintf("eval-run-%03d", index+1)
	allowedTools := registeredToolNames(e.registry)
	route := e.router.Route(item, allowedTools)
	caseResult := CaseResult{
		Name:         item.Name,
		Decision:     route.Decision,
		ToolNames:    route.ToolNames(),
		ToolInputs:   route.ToolInputs(),
		AllowedTools: append([]string{}, allowedTools...),
	}
	for callIndex, call := range route.ToolCalls {
		callContext := capability.CallContext{
			Actor:      actor,
			ThreadID:   "eval-thread",
			RunID:      runID,
			ToolCallID: fmt.Sprintf("eval-call-%d", callIndex+1),
			RequestID:  fmt.Sprintf("%s-%d", runID, callIndex+1),
		}
		if call.Name == preparationcapability.PracticePreviewToolName {
			callContext.Authorization = json.RawMessage(`{"intent":"REQUEST_CREATE"}`)
		}
		_, err := e.executor.Execute(
			ctx,
			callContext,
			capability.Invocation{Name: call.Name, Input: call.Input},
		)
		if err != nil {
			caseResult.ErrorCategory = capability.ErrorCategory(err)
			if route.Decision != DecisionRefuse {
				caseResult.Failures = append(
					caseResult.Failures,
					fmt.Sprintf("tool %s failed with %s", call.Name, caseResult.ErrorCategory),
				)
			}
			break
		}
	}
	caseResult.PreviewInputs = e.preview.takeInputsForRun(runID)
	caseResult.Failures = append(
		caseResult.Failures,
		validateCase(item, caseResult)...,
	)
	caseResult.Passed = len(caseResult.Failures) == 0
	return caseResult, nil
}

type Route struct {
	Decision  string
	ToolCalls []ToolCall
}

func (r Route) ToolNames() []string {
	names := make([]string, 0, len(r.ToolCalls))
	for _, call := range r.ToolCalls {
		names = append(names, call.Name)
	}
	return names
}

func (r Route) ToolInputs() map[string]map[string]any {
	inputs := make(map[string]map[string]any, len(r.ToolCalls))
	for _, call := range r.ToolCalls {
		var parsed map[string]any
		if err := json.Unmarshal(call.Input, &parsed); err == nil {
			inputs[call.Name] = parsed
		}
	}
	return inputs
}

type ToolCall struct {
	Name  string
	Input []byte
}

type DeterministicRouter struct{}

// Route 是离线评测使用的确定性假模型，只负责生成可复现的 Tool Call。
// 生产 Runtime 不调用这里的关键词规则，真实工具选择由 Provider 模型完成。
func (DeterministicRouter) Route(
	item RoutingCase,
	allowed []string,
) Route {
	input := normalize(lastUserContent(item.Messages))
	switch {
	case hasAny(input, "删除", "delete all", "所有记录"):
		return Route{Decision: DecisionRefuse}
	case hasAny(input, "user_id", "owner_id", "other-user"):
		return Route{Decision: DecisionRefuse}
	case hasAny(input, "刚才这句话", "current utterance"):
		return Route{Decision: DecisionDirect}
	case hasAny(input, "委婉", "polish", "翻译", "有什么问题", "grammar"):
		if !hasBusinessSignal(input) {
			return Route{Decision: DecisionDirect}
		}
	case hasAny(input, "第一条", "first"):
		if containsString(allowed, reviewcapability.ReviewGetToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: reviewcapability.ReviewGetToolName,
					Input: mustRaw(map[string]any{
						"report_id": "mock-report-001",
					}),
				}},
			}
		}
	case followsIELTSWarmUp(item.Messages):
		return routeIELTSPractice(item.Messages, allowed, true)
	case isIELTSPracticeCreationRequest(input):
		return routeIELTSPractice(item.Messages, allowed, false)
	case hasAny(input, "刚完成", "刚练完", "最新报告", "latest report"):
		if containsString(allowed, reviewcapability.ReviewSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name:  reviewcapability.ReviewSearchToolName,
					Input: mustRaw(map[string]any{}),
				}},
			}
		}
	case hasAny(input, "确认开始练习", "确认开始面试", "confirm practice"):
		return Route{Decision: DecisionDirect}
	case hasAny(input, "开始练习", "开始面试", "start practice"):
		return Route{Decision: DecisionClarify}
	case hasAny(input, "预览", "练习方案", "preview"):
		if containsString(allowed, preparationcapability.PracticePreviewToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: preparationcapability.PracticePreviewToolName,
					Input: mustRaw(map[string]any{
						"resolution_kind":        preparationcapability.SceneResolutionKindCatalog,
						"catalog_scene_ids":      []string{"scn_interview_self_introduction"},
						"custom_scenario":        "",
						"custom_experience_hint": "NONE",
					}),
				}},
			}
		}
	case hasAny(input, "评价", "评家", "复盘", "review", "feedback"):
		if containsString(allowed, reviewcapability.ReviewSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: reviewcapability.ReviewSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "interview",
						"limit": 1,
					}),
				}},
			}
		}
	case hasAny(input, "简历", "jd", "resume"):
		if containsString(allowed, capabilityfixture.MaterialSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: capabilityfixture.MaterialSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "resume",
						"kind":  "resume",
						"limit": 1,
					}),
				}},
			}
		}
	case hasAny(input, "错题", "以前的语法", "mistake"):
		if containsString(allowed, capabilityfixture.MistakeSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: capabilityfixture.MistakeSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "structure",
						"limit": 1,
					}),
				}},
			}
		}
	}
	return Route{Decision: DecisionDirect}
}

func routeIELTSPractice(
	messages []EvalMessage,
	allowed []string,
	afterWarmUp bool,
) Route {
	selection := findIELTSPracticeSelection(messages)
	if selection.mode == "" {
		return Route{Decision: DecisionDirect}
	}
	arguments := map[string]any{"ielts_practice_mode": selection.mode}
	if selection.mode != "FULL_MOCK" {
		if selection.topicChoice == "" {
			return Route{Decision: DecisionDirect}
		}
		arguments["ielts_topic_choice"] = selection.topicChoice
	}

	toolName := preparationcapability.IELTSWarmUpToolName
	if selection.mode == "FULL_MOCK" || afterWarmUp ||
		asksToStartDirectly(normalize(lastUserContent(messages))) {
		toolName = preparationcapability.PracticePreviewToolName
		arguments["resolution_kind"] = preparationcapability.SceneResolutionKindCatalog
		arguments["catalog_scene_ids"] = []string{"scn_ielts_speaking"}
		arguments["custom_scenario"] = ""
		arguments["custom_experience_hint"] = "NONE"
	}
	if !containsString(allowed, toolName) {
		return Route{Decision: DecisionDirect}
	}
	return Route{
		Decision: DecisionToolCall,
		ToolCalls: []ToolCall{{
			Name:  toolName,
			Input: mustRaw(arguments),
		}},
	}
}

type ieltsPracticeSelection struct {
	mode        string
	topicChoice string
}

func findIELTSPracticeSelection(messages []EvalMessage) ieltsPracticeSelection {
	start := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" &&
			hasAny(normalize(messages[index].Content), "雅思", "ielts") {
			start = index
			break
		}
	}
	if start < 0 {
		return ieltsPracticeSelection{}
	}

	selection := ieltsPracticeSelection{}
	for _, message := range messages[start:] {
		if message.Role != "user" {
			continue
		}
		input := normalize(message.Content)
		if mode := parseIELTSPracticeMode(input); mode != "" {
			selection.mode = mode
		}
		if choice := parseIELTSTopicChoice(input); choice != "" {
			selection.topicChoice = choice
		}
	}
	return selection
}

func parseIELTSPracticeMode(input string) string {
	switch {
	case hasAny(input, "完整模考", "full mock"):
		return "FULL_MOCK"
	case hasAny(input, "part 1", "part one"):
		return "PART_1"
	case hasAny(input, "part 2", "part two"):
		return "PART_2"
	case hasAny(input, "part 3", "part three"):
		return "PART_3"
	default:
		return ""
	}
}

func parseIELTSTopicChoice(input string) string {
	switch {
	case hasAny(input, "随机", "random"):
		return "random"
	case hasAny(input, "人物", "person"):
		return "person"
	case hasAny(input, "地点", "place"):
		return "place"
	case hasAny(input, "事物", "thing"):
		return "thing"
	case hasAny(input, "经历", "experience"):
		return "experience"
	default:
		return ""
	}
}

func followsIELTSWarmUp(messages []EvalMessage) bool {
	input := normalize(lastUserContent(messages))
	if hasAny(
		input,
		"报告",
		"评价",
		"复盘",
		"feedback",
		"review",
		"取消",
		"cancel",
	) {
		return false
	}
	latestUserIndex := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			latestUserIndex = index
			break
		}
	}
	for index := latestUserIndex - 1; index >= 0; index-- {
		switch messages[index].Role {
		case "assistant":
			content := normalize(messages[index].Content)
			return hasAny(content, "用一两句英语说说", "直接回答", "问我要提示")
		case "user":
			return false
		}
	}
	return false
}

func asksToStartDirectly(input string) bool {
	return hasAny(
		input,
		"跳过",
		"直接开始",
		"马上开始",
		"skip",
		"start directly",
		"start now",
	)
}

func isIELTSPracticeCreationRequest(input string) bool {
	return hasAny(input, "雅思", "ielts") &&
		!hasAny(input, "报告", "评价", "复盘", "feedback", "review") &&
		hasAny(
			input,
			"创建",
			"安排",
			"想练",
			"练习",
			"模考",
			"给我",
			"practice",
			"mock",
		)
}

func registeredToolNames(registry *capability.Registry) []string {
	if registry == nil {
		return nil
	}
	definitions := registry.Definitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func registeredWriteToolNames(registry *capability.Registry) []string {
	if registry == nil {
		return nil
	}
	names := make([]string, 0)
	for _, definition := range registry.Definitions() {
		if !definition.ReadOnly {
			names = append(names, definition.Name)
		}
	}
	return names
}

func validateCase(item RoutingCase, result CaseResult) []string {
	failures := make([]string, 0)
	if result.Decision != item.ExpectedDecision {
		failures = append(
			failures,
			fmt.Sprintf("decision = %s, want %s", result.Decision, item.ExpectedDecision),
		)
	}
	if !sameStrings(result.ToolNames, item.ExpectedToolNames) {
		failures = append(
			failures,
			fmt.Sprintf("tools = %#v, want %#v", result.ToolNames, item.ExpectedToolNames),
		)
	}
	for _, name := range item.ForbiddenTools {
		if containsString(result.ToolNames, name) {
			failures = append(failures, fmt.Sprintf("forbidden tool called: %s", name))
		}
	}
	for toolName, expected := range item.ExpectedArgs {
		actual, ok := result.ToolInputs[toolName]
		if !ok {
			failures = append(failures, fmt.Sprintf("missing args for %s", toolName))
			continue
		}
		for field, expectedValue := range expected {
			if !sameJSONValue(actual[field], expectedValue) {
				failures = append(
					failures,
					fmt.Sprintf(
						"%s.%s = %#v, want %#v",
						toolName,
						field,
						actual[field],
						expectedValue,
					),
				)
			}
		}
	}
	for toolName, forbidden := range item.ForbiddenArgs {
		actual, ok := result.ToolInputs[toolName]
		if !ok {
			continue
		}
		for _, field := range forbidden {
			if _, found := actual[field]; found {
				failures = append(
					failures,
					fmt.Sprintf("%s includes forbidden arg %s", toolName, field),
				)
			}
		}
	}
	if item.ExpectedPreviewInput != nil {
		if len(result.PreviewInputs) != 1 {
			failures = append(
				failures,
				fmt.Sprintf("preview inputs = %d, want 1", len(result.PreviewInputs)),
			)
		} else if !samePreviewInput(
			result.PreviewInputs[0],
			*item.ExpectedPreviewInput,
		) {
			failures = append(
				failures,
				fmt.Sprintf(
					"preview input = %#v, want %#v",
					result.PreviewInputs[0],
					*item.ExpectedPreviewInput,
				),
			)
		}
	}
	return failures
}

func samePreviewInput(left, right PreviewInputRecord) bool {
	return left.Kind == right.Kind &&
		left.CatalogSceneID == right.CatalogSceneID &&
		sameStrings(left.CandidateSceneIDs, right.CandidateSceneIDs)
}

func lastUserContent(messages []EvalMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messages[index].Content
		}
	}
	return ""
}

func latestIELTSRequest(messages []EvalMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		content := messages[index].Content
		if hasAny(normalize(content), "雅思", "ielts") {
			return content
		}
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, item := range left {
		if containsString(right, item) {
			return true
		}
	}
	return false
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string{}, left...)
	rightCopy := append([]string{}, right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func sameJSONValue(left any, right any) bool {
	switch expected := right.(type) {
	case int:
		actual, ok := left.(float64)
		return ok && actual == float64(expected)
	default:
		return left == right
	}
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func hasAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func hasBusinessSignal(text string) bool {
	return hasAny(
		text,
		"面试",
		"会议",
		"客户",
		"演讲",
		"简历",
		"jd",
		"评价",
		"复盘",
		"错题",
		"interview",
		"review",
		"resume",
	)
}
