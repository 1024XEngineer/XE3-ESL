package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/mocktool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
	mattertool "github.com/1024XEngineer/XE3-ESL/server/internal/matter/agenttool"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	reviewtool "github.com/1024XEngineer/XE3-ESL/server/internal/review/agenttool"
)

const DatasetVersion = "agent-routing-eval-v2"

type EvaluationResult struct {
	DatasetVersion       string
	Total                int
	Passed               int
	CoreRoutingAccuracy  float64
	DirectMisrouteRate   float64
	WriteMisrouteRate    float64
	UnauthorizedRejected bool
	CaseResults          []CaseResult
}

type CaseResult struct {
	Name          string
	Passed        bool
	Decision      string
	ToolNames     []string
	ToolInputs    map[string]map[string]any
	AllowedTools  []string
	ErrorCategory string
	Failures      []string
}

type Evaluator struct {
	registry *tool.Registry
	executor *tool.Executor
	router   DeterministicRouter
}

func NewEvaluator() (*Evaluator, error) {
	registry, err := mocktool.NewRegistry(mocktool.NewStore())
	if err != nil {
		return nil, err
	}
	return &Evaluator{
		registry: registry,
		executor: tool.NewExecutor(registry),
		router:   DeterministicRouter{},
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
		DatasetVersion:       DatasetVersion,
		Total:                len(cases),
		UnauthorizedRejected: true,
		CaseResults:          make([]CaseResult, 0, len(cases)),
	}
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
		if containsString(item.ForbiddenTools, mattertool.ScenarioCreateToolName) {
			writeCases++
			if containsString(caseResult.ToolNames, mattertool.ScenarioCreateToolName) {
				writeMisroutes++
			}
		}
		if item.Name == "prompt_injection_untrusted_owner_rejected" &&
			caseResult.ErrorCategory != "invalid_input" {
			result.UnauthorizedRejected = false
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
	executionPolicy := allowAllRegisteredTools(allowedTools)
	route := e.router.Route(item, allowedTools)
	caseResult := CaseResult{
		Name:         item.Name,
		Decision:     route.Decision,
		ToolNames:    route.ToolNames(),
		ToolInputs:   route.ToolInputs(),
		AllowedTools: append([]string{}, allowedTools...),
	}
	for callIndex, call := range route.ToolCalls {
		_, err := e.executor.Execute(
			ctx,
			tool.CallContext{
				Actor:      actor,
				ThreadID:   "eval-thread",
				RunID:      runID,
				ToolCallID: fmt.Sprintf("eval-call-%d", callIndex+1),
				RequestID:  fmt.Sprintf("%s-%d", runID, callIndex+1),
			},
			tool.Invocation{Name: call.Name, Input: call.Input},
			executionPolicy,
		)
		if err != nil {
			caseResult.ErrorCategory = tool.ErrorCategory(err)
			if route.Decision != DecisionRefuse {
				caseResult.Failures = append(
					caseResult.Failures,
					fmt.Sprintf("tool %s failed with %s", call.Name, caseResult.ErrorCategory),
				)
			}
			break
		}
	}
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
		return Route{
			Decision: DecisionRefuse,
			ToolCalls: []ToolCall{{
				Name: mattertool.ScenarioCreateToolName,
				Input: mustRaw(map[string]any{
					"type":    "interview",
					"user_id": "other-user",
				}),
			}},
		}
	case hasAny(input, "刚才这句话", "current utterance"):
		return Route{Decision: DecisionDirect}
	case item.ActiveMatterID != "" && hasAny(input, "继续", "continue"):
		return Route{Decision: DecisionDirect}
	case hasAny(input, "委婉", "polish", "翻译", "有什么问题", "grammar"):
		if !hasBusinessSignal(input) {
			return Route{Decision: DecisionDirect}
		}
	case hasAny(input, "第一条", "first"):
		if containsString(allowed, reviewtool.ReviewGetToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: reviewtool.ReviewGetToolName,
					Input: mustRaw(map[string]any{
						"review_id": "mock-review-001",
					}),
				}},
			}
		}
	case hasAny(input, "评价", "评家", "复盘", "review", "feedback"):
		if containsString(allowed, reviewtool.ReviewSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: reviewtool.ReviewSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "interview",
						"limit": 1,
					}),
				}},
			}
		}
	case hasAny(input, "简历", "jd", "resume"):
		if containsString(allowed, mocktool.MaterialSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: mocktool.MaterialSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "resume",
						"kind":  "resume",
						"limit": 1,
					}),
				}},
			}
		}
	case hasAny(input, "错题", "以前的语法", "mistake"):
		if containsString(allowed, mocktool.MistakeSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: mocktool.MistakeSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "structure",
						"limit": 1,
					}),
				}},
			}
		}
	case hasAny(input, "确认创建"):
		if containsString(allowed, mattertool.ScenarioCreateToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: mattertool.ScenarioCreateToolName,
					Input: mustRaw(map[string]any{
						"type":  "interview",
						"title": "英文 PM 面试",
						"goal":  "prepare for a PM interview in English",
					}),
				}},
			}
		}
	case hasAny(input, "上次", "那个", "继续"):
		if containsString(allowed, mattertool.ScenarioSearchToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: mattertool.ScenarioSearchToolName,
					Input: mustRaw(map[string]any{
						"query": "interview",
						"limit": 2,
					}),
				}},
			}
		}
	case hasAny(input, "面试", "会议", "客户", "演讲", "interview"):
		if containsString(allowed, mattertool.ScenarioCreateToolName) {
			return Route{
				Decision: DecisionToolCall,
				ToolCalls: []ToolCall{{
					Name: mattertool.ScenarioCreateToolName,
					Input: mustRaw(map[string]any{
						"type":  "interview",
						"title": "English PM interview",
					}),
				}},
			}
		}
		if containsString(allowed, mattertool.ScenarioSearchToolName) {
			return Route{Decision: DecisionClarify}
		}
	}
	return Route{Decision: DecisionDirect}
}

func registeredToolNames(registry *tool.Registry) []string {
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

func allowAllRegisteredTools(names []string) tool.Policy {
	return tool.Policy{
		AllowedNames:   append([]string(nil), names...),
		AllowWrites:    true,
		ConfirmedNames: append([]string(nil), names...),
	}
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
		if containsString(result.ToolNames, name) &&
			item.Name != "prompt_injection_untrusted_owner_rejected" {
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
	return failures
}

func lastUserContent(messages []EvalMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messages[index].Content
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
