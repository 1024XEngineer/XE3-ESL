package commands

import "encoding/json"

const (
	ToolScenarioCreate = "scenario.create.v1"
	ToolScenarioSearch = "scenario.search.v1"
	ToolReviewSearch   = "review.search.v1"
	ToolMistakeSearch  = "mistake.search.v1"
	ToolMaterialSearch = "material.search.v1"
)

// Builtins 返回 Agent 首批支持的用户可见斜杠命令。
func Builtins() []Definition {
	return []Definition{
		{
			Name:        "创建面试",
			Aliases:     []string{"面试", "准备面试"},
			Description: "创建或补全面试准备场景",
			ToolName:    ToolScenarioCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{
					"type":  "interview",
					"title": args,
				})
			},
		},
		{
			Name:        "创建口语场景",
			Aliases:     []string{"口语场景", "新建场景"},
			Description: "创建职业英语口语场景",
			ToolName:    ToolScenarioCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{
					"type":  "speaking",
					"title": args,
				})
			},
		},
		{
			Name:        "继续场景",
			Aliases:     []string{"继续"},
			Description: "搜索并恢复相关的历史场景",
			ToolName:    ToolScenarioSearch,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{"query": args})
			},
		},
		{
			Name:        "查评价",
			Aliases:     []string{"评价", "查上次评价"},
			Description: "查询历史 Review 或面试评价",
			ToolName:    ToolReviewSearch,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{"query": args})
			},
		},
		{
			Name:        "查错题",
			Aliases:     []string{"错题"},
			Description: "查询历史错题或学习问题",
			ToolName:    ToolMistakeSearch,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{"query": args})
			},
		},
		{
			Name:        "解析简历",
			Aliases:     []string{"简历"},
			Description: "检索或解析简历和材料",
			ToolName:    ToolMaterialSearch,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{"query": args})
			},
		},
	}
}
