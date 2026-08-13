package run

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agenthandoff "github.com/1024XEngineer/XE3-ESL/server/internal/agent/handoff"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	practicePreviewToolName     = "practice.preview.v1"
	maxIELTSRoutingContextTurns = 4
)

type ieltsRoutingMode uint8

const (
	ieltsRoutingModeUnknown ieltsRoutingMode = iota
	ieltsRoutingModePart1
	ieltsRoutingModePart2
	ieltsRoutingModePart3
	ieltsRoutingModeFullMock
)

type priorIELTSTool uint8

const (
	priorIELTSToolNone priorIELTSTool = iota
	priorIELTSToolWarmUp
	priorIELTSToolPreview
)

type priorIELTSToolState struct {
	tool        priorIELTSTool
	mode        ieltsRoutingMode
	topicChoice string
}

type ieltsCreationRouting struct {
	active                bool
	mode                  ieltsRoutingMode
	topicChoice           string
	bypassWarmUp          bool
	currentBypassesWarmUp bool
	cancelled             bool
	rootIsCurrent         bool
	priorStateCanUseTool  bool
	currentChangesChoices bool
	currentWarmUpAnswer   bool
	currentSelectionReply bool
}

var ieltsPartPatterns = map[ieltsRoutingMode]*regexp.Regexp{
	ieltsRoutingModePart1: regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9])part\s*(?:1|one)(?:$|[^a-z0-9])`,
	),
	ieltsRoutingModePart2: regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9])part\s*(?:2|two)(?:$|[^a-z0-9])`,
	),
	ieltsRoutingModePart3: regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9])part\s*(?:3|three)(?:$|[^a-z0-9])`,
	),
}

var ieltsEnglishTopicPatterns = map[string]*regexp.Regexp{
	"random": regexp.MustCompile(`(?i)(?:^|[^a-z])random(?:$|[^a-z])`),
	"person": regexp.MustCompile(`(?i)(?:^|[^a-z])person(?:$|[^a-z])`),
	"place":  regexp.MustCompile(`(?i)(?:^|[^a-z])place(?:$|[^a-z])`),
	"thing":  regexp.MustCompile(`(?i)(?:^|[^a-z])thing(?:$|[^a-z])`),
	"experience": regexp.MustCompile(
		`(?i)(?:^|[^a-z])experience(?:$|[^a-z])`,
	),
}

var ieltsEnglishWordPattern = regexp.MustCompile(
	`[A-Za-z]+(?:['’][A-Za-z]+)?`,
)

var ieltsWarmUpStopWords = map[string]struct{}{
	"a": {}, "about": {}, "also": {}, "am": {}, "an": {}, "and": {},
	"are": {}, "at": {}, "be": {}, "because": {}, "been": {}, "being": {},
	"but": {}, "can": {}, "could": {}, "did": {}, "do": {}, "does": {},
	"ever": {}, "for": {}, "from": {}, "had": {}, "has": {}, "have": {},
	"he": {}, "her": {}, "here": {}, "him": {}, "his": {}, "how": {},
	"i": {}, "in": {}, "is": {}, "it": {}, "its": {}, "just": {},
	"me": {}, "might": {}, "mine": {}, "more": {}, "most": {}, "much": {},
	"must": {}, "my": {}, "name": {}, "no": {}, "not": {}, "of": {},
	"on": {}, "or": {}, "our": {}, "really": {}, "she": {}, "should": {},
	"plan": {}, "practice": {}, "ready": {}, "start": {}, "card": {},
	"so": {}, "that": {}, "the": {}, "their": {}, "them": {}, "there": {},
	"these": {}, "they": {}, "this": {}, "those": {}, "to": {}, "very": {},
	"was": {}, "we": {}, "were": {}, "what": {}, "when": {}, "where": {},
	"who": {}, "why": {}, "will": {}, "with": {}, "would": {}, "yes": {},
	"you": {}, "your": {},
}

var ieltsWarmUpBusinessWords = []string{
	"正式练习", "练习计划", "练习卡片", "练习会话", "准备好", "已准备",
	"卡片", "创建", "计划", "准备", "开始", "practice", "plan", "card",
	"ready", "start",
}

func ieltsWarmUpPrompt(message TextMessage) (string, bool) {
	if message.Role != TextRoleTool {
		return "", false
	}
	var result providerToolResult
	if json.Unmarshal([]byte(message.Content), &result) != nil ||
		len(result.Content) != 1 {
		return "", false
	}
	prompt, ok := result.Content["prompt"].(string)
	return strings.TrimSpace(prompt), ok && conversation.ValidMessageContent(prompt)
}

func isIELTSPracticePreviewCall(call ModelToolCall) bool {
	if call.Name != practicePreviewToolName {
		return false
	}
	var input struct {
		PracticeMode string `json:"ielts_practice_mode"`
		TopicChoice  string `json:"ielts_topic_choice"`
	}
	if json.Unmarshal(call.Arguments, &input) != nil {
		return false
	}
	switch input.PracticeMode {
	case "FULL_MOCK":
		return input.TopicChoice == ""
	case "PART_1", "PART_2", "PART_3":
		return validIELTSTopicChoice(input.TopicChoice)
	default:
		return false
	}
}

func validIELTSWarmUpAcknowledgement(content string, input string) bool {
	content = strings.TrimSpace(content)
	if content == "" || strings.ContainsAny(content, "\r\n") ||
		utf8.RuneCountInString(content) > 80 {
		return false
	}
	terminators := 0
	for _, char := range content {
		if strings.ContainsRune("。！？.!?", char) {
			terminators++
		}
	}
	if terminators > 1 {
		return false
	}
	lower := strings.ToLower(content)
	for _, forbidden := range ieltsWarmUpBusinessWords {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	responseWords := make(map[string]struct{})
	for _, word := range ieltsEnglishWordPattern.FindAllString(lower, -1) {
		responseWords[word] = struct{}{}
	}
	for _, word := range meaningfulIELTSWarmUpWords(input) {
		if _, ok := responseWords[strings.ToLower(word)]; ok {
			return true
		}
	}
	return false
}

func fallbackIELTSWarmUpAcknowledgement(input string) string {
	words := meaningfulIELTSWarmUpWords(input)
	if len(words) == 0 {
		return "听到了。"
	}
	selected := words[0]
	for _, word := range words[1:] {
		if len(word) > len(selected) {
			selected = word
		}
	}
	return "听到了，你刚才提到了 “" + selected + "”。"
}

func meaningfulIELTSWarmUpWords(input string) []string {
	words := ieltsEnglishWordPattern.FindAllString(input, -1)
	meaningful := make([]string, 0, len(words))
	for _, word := range words {
		normalized := strings.ToLower(strings.ReplaceAll(word, "’", "'"))
		if len(normalized) < 2 {
			continue
		}
		if _, stop := ieltsWarmUpStopWords[normalized]; stop {
			continue
		}
		meaningful = append(meaningful, word)
	}
	return meaningful
}

func inspectIELTSCreationRouting(request TextRequest) ieltsCreationRouting {
	userMessages := make([]string, 0, maxIELTSRoutingContextTurns)
	for _, message := range request.Messages {
		if message.Role == TextRoleUser {
			userMessages = append(userMessages, userMessageText(message))
		}
	}
	if len(userMessages) == 0 {
		return ieltsCreationRouting{}
	}
	start := len(userMessages) - maxIELTSRoutingContextTurns
	if start < 0 {
		start = 0
	}
	root := -1
	for index := len(userMessages) - 1; index >= start; index-- {
		if isIELTSPracticeCreation(userMessages[index]) {
			root = index
			break
		}
	}
	if root < 0 {
		return ieltsCreationRouting{}
	}

	current := len(userMessages) - 1
	currentSelectionReply := isIELTSSelectionReply(userMessages[current])
	routing := ieltsCreationRouting{
		active:        true,
		rootIsCurrent: root == current,
		currentWarmUpAnswer: !currentSelectionReply &&
			isEnglishWarmUpAnswerAttempt(userMessages[current]),
		currentSelectionReply: currentSelectionReply,
	}
	for index := root; index <= current; index++ {
		text := userMessages[index]
		if index > root && index < current &&
			!isIELTSSelectionReply(text) &&
			!containsIELTSWarmUpBypass(text) {
			return ieltsCreationRouting{}
		}
		if index == current && index > root {
			routing.priorStateCanUseTool = routing.canUseTool()
		}
		if index > root && containsIELTSPracticeCancellation(text) {
			routing.cancelled = true
			continue
		}
		if index > root && !isIELTSSelectionReply(text) &&
			!containsIELTSWarmUpBypass(text) {
			continue
		}
		previousMode := routing.mode
		previousTopic := routing.topicChoice
		if mode, specified := parseIELTSRoutingMode(text); specified {
			routing.mode = mode
			if mode == ieltsRoutingModeFullMock {
				routing.topicChoice = ""
			}
		}
		if topic, specified := parseIELTSTopicChoice(text); specified {
			routing.topicChoice = topic
		}
		if containsIELTSWarmUpBypass(text) {
			routing.bypassWarmUp = true
			if index == current {
				routing.currentBypassesWarmUp = true
			}
		}
		if index == current && index > root &&
			(previousMode != routing.mode || previousTopic != routing.topicChoice) {
			routing.currentChangesChoices = true
		}
	}
	return routing
}

func (routing ieltsCreationRouting) toolChoice(
	previousTool priorIELTSTool,
) ToolChoice {
	if !routing.active {
		return ToolChoice{Mode: ToolChoiceAuto}
	}
	if routing.cancelled {
		return ToolChoice{Mode: ToolChoiceAuto}
	}
	if !routing.rootIsCurrent && previousTool == priorIELTSToolPreview {
		return ToolChoice{Mode: ToolChoiceAuto}
	}
	if routing.mode == ieltsRoutingModeFullMock {
		return specificToolChoice(practicePreviewToolName)
	}
	if !routing.specialtyComplete() {
		return ToolChoice{Mode: ToolChoiceAuto}
	}
	if routing.bypassWarmUp {
		return specificToolChoice(practicePreviewToolName)
	}
	if !routing.rootIsCurrent && previousTool == priorIELTSToolWarmUp &&
		!routing.currentChangesChoices {
		if routing.currentWarmUpAnswer {
			return specificToolChoice(practicePreviewToolName)
		}
		return ToolChoice{Mode: ToolChoiceNone}
	}
	return specificToolChoice(ieltsWarmUpToolName)
}

func (routing ieltsCreationRouting) needsPriorToolState() bool {
	return routing.active && !routing.cancelled && !routing.rootIsCurrent &&
		routing.priorStateCanUseTool
}

func (routing ieltsCreationRouting) canUseTool() bool {
	return routing.mode == ieltsRoutingModeFullMock ||
		routing.specialtyComplete()
}

func (routing ieltsCreationRouting) specialtyComplete() bool {
	return (routing.mode == ieltsRoutingModePart1 ||
		routing.mode == ieltsRoutingModePart2 ||
		routing.mode == ieltsRoutingModePart3) && routing.topicChoice != ""
}

func (routing ieltsCreationRouting) clarification() (string, bool) {
	if !routing.active || routing.cancelled ||
		(!routing.rootIsCurrent && !routing.currentSelectionReply) {
		return "", false
	}
	if routing.mode == ieltsRoutingModeUnknown {
		return "没问题，你想先练 Part 1、Part 2、Part 3，还是直接来一场完整模考？", true
	}
	if !routing.specialtyComplete() && routing.mode != ieltsRoutingModeFullMock {
		mode, _ := routing.practiceModeValue()
		part := strings.ReplaceAll(mode, "PART_", "Part ")
		return "好，那就先练 " + part + "：你想聊人物、地点、事物还是经历，还是让我随机选一个？", true
	}
	return "", false
}

func (routing ieltsCreationRouting) deterministicToolCall(
	runID string,
	toolName string,
) (ModelToolCall, bool) {
	if toolName != ieltsWarmUpToolName && toolName != practicePreviewToolName {
		return ModelToolCall{}, false
	}
	mode, ok := routing.practiceModeValue()
	if !ok {
		return ModelToolCall{}, false
	}
	input := map[string]string{"ielts_practice_mode": mode}
	if routing.mode != ieltsRoutingModeFullMock {
		if routing.topicChoice == "" {
			return ModelToolCall{}, false
		}
		input["ielts_topic_choice"] = routing.topicChoice
	}
	arguments, err := json.Marshal(input)
	if err != nil {
		return ModelToolCall{}, false
	}
	return ModelToolCall{
		ID:        "ielts-routing-" + runID,
		Name:      toolName,
		Arguments: arguments,
	}, true
}

func specificToolChoice(name string) ToolChoice {
	return ToolChoice{Mode: ToolChoiceSpecific, Name: name}
}

func userMessageText(message TextMessage) string {
	if message.Content != "" {
		return message.Content
	}
	for _, part := range message.ContentParts {
		if part.Kind == ContentPartText {
			return part.Text
		}
	}
	return ""
}

func isIELTSPracticeCreation(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if !strings.Contains(normalized, "ielts") &&
		!strings.Contains(normalized, "雅思") {
		return false
	}
	if containsIELTSPracticeCancellation(normalized) || containsAny(normalized,
		"怎么回答", "如何回答", "怎么答", "如何答", "评分规则", "评分标准",
		"怎么练", "如何练", "怎么安排", "如何安排", "是什么", "有什么区别", "分析一下", "讲解一下",
		"怎么样", "示例", "范例", "例子", "技巧", "建议", "方法",
	) {
		return false
	}
	if mode, hasMode := parseIELTSRoutingMode(normalized); hasMode &&
		mode != ieltsRoutingModeUnknown && isBareIELTSModeTopicSelection(normalized) {
		if mode == ieltsRoutingModeFullMock {
			return true
		}
		if topic, hasTopic := parseIELTSTopicChoice(normalized); hasTopic &&
			topic != "" {
			return true
		}
	}
	if containsAny(normalized,
		"想练", "想要练", "创建", "建立", "安排", "专项",
		"来一场", "给我一场", "做一场", "准备一场",
		"want to practice", "want to practise", "create", "set up",
	) {
		return true
	}
	return false
}

func isBareIELTSModeTopicSelection(input string) bool {
	remaining := strings.NewReplacer(
		"ielts", "", "雅思", "", "口语", "", "speaking", "",
		"part one", "", "part two", "", "part three", "",
		"part 1", "", "part 2", "", "part 3", "",
		"part1", "", "part2", "", "part3", "",
		"full mock", "", "完整模考", "", "完整模拟", "", "全真模考", "",
		"全真模拟", "", "全套模考", "",
		"随机", "", "人物", "", "地点", "", "事物", "", "经历", "",
		"random", "", "person", "", "place", "", "thing", "",
		"experience", "", "类", "",
	).Replace(strings.ToLower(input))
	remaining = strings.Trim(remaining, " ，,。.!！?？、:：/\t\n\r-—()（）")
	return remaining == ""
}

func isIELTSSelectionReply(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	normalized = strings.Trim(normalized, " ，,。.!！?？、\t\n\r")
	normalized = strings.TrimSuffix(normalized, "吧")
	for _, exact := range []string{
		"随机", "随机安排", "人物", "人物类", "地点", "地点类",
		"事物", "事物类", "经历", "经历类",
		"part 1", "part1", "part one", "part 2", "part2", "part two",
		"part 3", "part3", "part three", "完整模考", "完整模拟",
		"full mock", "random", "person", "place", "thing", "experience",
	} {
		if normalized == exact {
			return true
		}
	}
	mode, hasMode := parseIELTSRoutingMode(normalized)
	topic, hasTopic := parseIELTSTopicChoice(normalized)
	if len([]rune(normalized)) > 24 {
		return false
	}
	if hasMode && mode != ieltsRoutingModeUnknown &&
		hasTopic && topic != "" {
		return true
	}
	if hasTopic && topic != "" && containsAny(normalized,
		"来一场", "来个", "选", "挑", "就练", "想练", "安排",
		"switch to", "change to", "choose", "pick", "let's do", "practice",
	) {
		return true
	}
	return containsAny(normalized, "改成", "换成", "改为", "换为") &&
		(hasMode && mode != ieltsRoutingModeUnknown || hasTopic && topic != "")
}

func parseIELTSRoutingMode(input string) (ieltsRoutingMode, bool) {
	normalized := strings.ToLower(input)
	matches := make([]ieltsRoutingMode, 0, 2)
	if containsAny(normalized,
		"完整模考", "完整模拟", "全真模考", "全真模拟", "全套模考",
		"full mock",
	) {
		matches = append(matches, ieltsRoutingModeFullMock)
	}
	for mode, pattern := range ieltsPartPatterns {
		if pattern.MatchString(normalized) {
			matches = append(matches, mode)
		}
	}
	if len(matches) != 1 {
		return ieltsRoutingModeUnknown, len(matches) > 1
	}
	return matches[0], true
}

func parseIELTSTopicChoice(input string) (string, bool) {
	normalized := strings.ToLower(input)
	matches := make([]string, 0, 2)
	for choice, chinese := range map[string]string{
		"random":     "随机",
		"person":     "人物",
		"place":      "地点",
		"thing":      "事物",
		"experience": "经历",
	} {
		if strings.Contains(normalized, chinese) ||
			ieltsEnglishTopicPatterns[choice].MatchString(normalized) {
			matches = append(matches, choice)
		}
	}
	if containsNaturalRandomSelection(normalized) {
		matches = append(matches, "random")
	}
	if len(matches) != 1 {
		return "", len(matches) > 1
	}
	return matches[0], true
}

func containsNaturalRandomSelection(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if strings.Contains(normalized, "随机") ||
		ieltsEnglishTopicPatterns["random"].MatchString(normalized) ||
		strings.ContainsAny(normalized, "?？") || containsAny(normalized,
		"不要", "别", "不想", "不需要", "为什么", "为何",
	) {
		return false
	}
	return containsAny(normalized,
		"随便帮我挑一个", "随便给我挑一个", "给我随便挑一个",
		"帮我随便挑一个", "随便挑一个", "你来选", "你帮我选",
		"帮我选一个", "帮我挑一个",
	)
}

func containsIELTSWarmUpBypass(input string) bool {
	normalized := strings.ToLower(input)
	if containsAny(normalized,
		"不要直接开始", "先不要开始", "不需要直接开始", "不用直接开始",
		"不跳过热身", "不要跳过热身", "别跳过热身", "don't start directly",
	) {
		return false
	}
	return containsAny(normalized,
		"直接开始", "跳过热身", "不用热身", "不需要热身", "start directly",
		"skip warm-up", "skip warmup",
	)
}

func containsIELTSPracticeCancellation(input string) bool {
	normalized := strings.ToLower(input)
	return containsAny(normalized,
		"算了", "不练了", "先不练", "不想练", "暂时不练", "不要练",
		"取消练习", "取消这场", "不要直接开始", "先不要开始", "先不开始",
		"cancel", "not now", "no thanks", "never mind", "don't want to practice",
	)
}

func hasRecentIELTSSetupSignal(request TextRequest) bool {
	userMessages := make([]string, 0, maxIELTSRoutingContextTurns)
	for _, message := range request.Messages {
		if message.Role == TextRoleUser {
			userMessages = append(userMessages, userMessageText(message))
		}
	}
	if len(userMessages) < 2 {
		return false
	}
	start := len(userMessages) - maxIELTSRoutingContextTurns
	if start < 0 {
		start = 0
	}
	for _, input := range userMessages[start : len(userMessages)-1] {
		mode, hasMode := parseIELTSRoutingMode(input)
		if (hasMode && mode != ieltsRoutingModeUnknown) ||
			isIELTSSelectionReply(input) {
			return true
		}
	}
	return false
}

func mayContinuePriorIELTSWarmUp(input string) bool {
	return containsIELTSPracticeCancellation(input) ||
		containsIELTSWarmUpBypass(input) ||
		isIELTSSelectionReply(input) ||
		isEnglishWarmUpAnswerAttempt(input) ||
		isIELTSWarmUpHelpOrPause(input)
}

func isEnglishWarmUpAnswerAttempt(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return false
	}
	if isIELTSWarmUpHelpOrPause(normalized) {
		return false
	}
	return len(ieltsEnglishWordPattern.FindAllString(normalized, -1)) >= 2 &&
		len(meaningfulIELTSWarmUpWords(normalized)) > 0
}

func isIELTSWarmUpHelpOrPause(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if containsAny(normalized,
		"i don't know", "i do not know", "i can't", "i cannot",
		"i'm not sure", "i am not sure", "i need more time",
		"no idea", "please explain", "can you explain", "could you explain",
		"what does", "what is", "how do", "how can", "wait a moment",
		"give me a hint", "give me an example", "help me", "say it again",
		"我不会", "不知道", "没听清", "听不懂", "什么意思",
		"怎么说", "如何说", "给个提示", "给我提示", "提示一下",
		"给个例子", "给我个例子", "解释一下", "帮帮我",
		"先等等", "等一下", "稍等", "没想好", "不确定",
	) {
		return true
	}
	for _, prefix := range []string{
		"can you ", "could you ", "would you ", "please ", "help me ",
		"what ", "how ", "why ", "when ", "where ", "who ",
	} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func containsAny(input string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(input, candidate) {
			return true
		}
	}
	return false
}

func routingFromPriorIELTSToolState(
	state priorIELTSToolState,
	input string,
) ieltsCreationRouting {
	selectionReply := isIELTSSelectionReply(input)
	routing := ieltsCreationRouting{
		active:                true,
		mode:                  state.mode,
		topicChoice:           state.topicChoice,
		cancelled:             containsIELTSPracticeCancellation(input),
		bypassWarmUp:          containsIELTSWarmUpBypass(input),
		currentBypassesWarmUp: containsIELTSWarmUpBypass(input),
		priorStateCanUseTool:  true,
		currentWarmUpAnswer: !selectionReply &&
			isEnglishWarmUpAnswerAttempt(input),
		currentSelectionReply: selectionReply,
	}
	if !selectionReply {
		return routing
	}
	previousMode := routing.mode
	previousTopic := routing.topicChoice
	if mode, specified := parseIELTSRoutingMode(input); specified {
		routing.mode = mode
		if mode == ieltsRoutingModeFullMock {
			routing.topicChoice = ""
		}
	}
	if topic, specified := parseIELTSTopicChoice(input); specified {
		routing.topicChoice = topic
	}
	routing.currentChangesChoices = previousMode != routing.mode ||
		previousTopic != routing.topicChoice
	return routing
}

func priorIELTSToolStateFromCall(call ToolCall) (priorIELTSToolState, bool) {
	if call.Status != ToolCallSucceeded ||
		(call.Name != ieltsWarmUpToolName && call.Name != practicePreviewToolName) {
		return priorIELTSToolState{}, false
	}
	var input struct {
		PracticeMode string `json:"ielts_practice_mode"`
		TopicChoice  string `json:"ielts_topic_choice"`
	}
	if json.Unmarshal(call.Input, &input) != nil {
		return priorIELTSToolState{}, false
	}
	state := priorIELTSToolState{topicChoice: input.TopicChoice}
	switch input.PracticeMode {
	case "PART_1":
		state.mode = ieltsRoutingModePart1
	case "PART_2":
		state.mode = ieltsRoutingModePart2
	case "PART_3":
		state.mode = ieltsRoutingModePart3
	case "FULL_MOCK":
		state.mode = ieltsRoutingModeFullMock
	default:
		return priorIELTSToolState{}, false
	}
	if state.mode == ieltsRoutingModeFullMock {
		if call.Name != practicePreviewToolName || state.topicChoice != "" ||
			!persistedPracticePreviewReady(call) {
			return priorIELTSToolState{}, false
		}
		state.tool = priorIELTSToolPreview
		return state, true
	}
	if !validIELTSTopicChoice(state.topicChoice) {
		return priorIELTSToolState{}, false
	}
	if call.Name == practicePreviewToolName {
		if !persistedPracticePreviewReady(call) {
			return priorIELTSToolState{}, false
		}
		state.tool = priorIELTSToolPreview
	} else {
		state.tool = priorIELTSToolWarmUp
	}
	return state, true
}

func validIELTSTopicChoice(choice string) bool {
	switch choice {
	case "random", "person", "place", "thing", "experience":
		return true
	default:
		return false
	}
}

func (service *Service) priorAdjacentIELTSToolState(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	manifest agentcontext.Manifest,
) (priorIELTSToolState, error) {
	if len(manifest.SelectedMessages) < 2 {
		return priorIELTSToolState{}, nil
	}
	current := manifest.SelectedMessages[len(manifest.SelectedMessages)-1]
	previous := manifest.SelectedMessages[len(manifest.SelectedMessages)-2]
	if current.Role != conversation.MessageRoleUser ||
		previous.Role != conversation.MessageRoleAssistant ||
		previous.Sequence+1 != current.Sequence {
		return priorIELTSToolState{}, nil
	}
	message, err := service.messages.FindMessage(
		ctx,
		actor.UserID,
		run.ThreadID,
		previous.MessageID,
	)
	if err != nil {
		return priorIELTSToolState{}, err
	}
	if message.Role != conversation.MessageRoleAssistant ||
		message.ProducedByRunID == "" {
		return priorIELTSToolState{}, nil
	}
	calls, err := service.repository.ListToolCalls(
		ctx,
		actor.UserID,
		message.ProducedByRunID,
	)
	if err != nil {
		return priorIELTSToolState{}, err
	}
	var found priorIELTSToolState
	for _, call := range calls {
		state, ok := priorIELTSToolStateFromCall(call)
		if !ok {
			continue
		}
		if found.tool != priorIELTSToolNone {
			return priorIELTSToolState{}, nil
		}
		found = state
	}
	return found, nil
}

func (routing ieltsCreationRouting) matchesToolInput(
	toolName string,
	input json.RawMessage,
) bool {
	if toolName != ieltsWarmUpToolName && toolName != practicePreviewToolName {
		return true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil || fields == nil {
		return false
	}
	var practiceMode string
	if err := json.Unmarshal(fields["ielts_practice_mode"], &practiceMode); err != nil {
		return false
	}
	mode, ok := routing.practiceModeValue()
	if !ok || practiceMode != mode {
		return false
	}
	topicInput, hasTopic := fields["ielts_topic_choice"]
	if routing.mode == ieltsRoutingModeFullMock {
		return toolName == practicePreviewToolName && !hasTopic
	}
	var topicChoice string
	if !hasTopic || json.Unmarshal(topicInput, &topicChoice) != nil {
		return false
	}
	return topicChoice == routing.topicChoice
}

func (routing ieltsCreationRouting) practiceModeValue() (string, bool) {
	switch routing.mode {
	case ieltsRoutingModePart1:
		return "PART_1", true
	case ieltsRoutingModePart2:
		return "PART_2", true
	case ieltsRoutingModePart3:
		return "PART_3", true
	case ieltsRoutingModeFullMock:
		return "FULL_MOCK", true
	default:
		return "", false
	}
}

func (service *Service) priorIELTSToolResult(
	ctx context.Context,
	actor requestcontext.Actor,
	run Run,
	manifest agentcontext.Manifest,
	routing ieltsCreationRouting,
) (priorIELTSTool, error) {
	if len(manifest.SelectedMessages) < 2 {
		return priorIELTSToolNone, nil
	}
	current := manifest.SelectedMessages[len(manifest.SelectedMessages)-1]
	previous := manifest.SelectedMessages[len(manifest.SelectedMessages)-2]
	if current.Role != conversation.MessageRoleUser ||
		previous.Role != conversation.MessageRoleAssistant ||
		previous.Sequence+1 != current.Sequence {
		return priorIELTSToolNone, nil
	}
	message, err := service.messages.FindMessage(
		ctx,
		actor.UserID,
		run.ThreadID,
		previous.MessageID,
	)
	if err != nil {
		return priorIELTSToolNone, err
	}
	if message.Role != conversation.MessageRoleAssistant ||
		message.ProducedByRunID == "" {
		return priorIELTSToolNone, nil
	}
	calls, err := service.repository.ListToolCalls(
		ctx,
		actor.UserID,
		message.ProducedByRunID,
	)
	if err != nil {
		return priorIELTSToolNone, err
	}
	result := priorIELTSToolNone
	for _, call := range calls {
		if call.Status != ToolCallSucceeded ||
			!routing.matchesToolInput(call.Name, call.Input) {
			continue
		}
		switch call.Name {
		case practicePreviewToolName:
			if persistedPracticePreviewReady(call) {
				return priorIELTSToolPreview, nil
			}
		case ieltsWarmUpToolName:
			result = priorIELTSToolWarmUp
		}
	}
	return result, nil
}

func persistedPracticePreviewReady(call ToolCall) bool {
	var result struct {
		Content map[string]any `json:"content"`
	}
	if json.Unmarshal(call.Result, &result) != nil ||
		result.Content["status"] != "preview_ready" || len(call.Handoffs) != 1 {
		return false
	}
	handoff := call.Handoffs[0]
	return handoff.Type == agenthandoff.ConfirmPracticePlanType &&
		handoff.ExecutableStatus == agenthandoff.PracticePlanReadyStatus
}
