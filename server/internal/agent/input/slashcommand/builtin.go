package slashcommand

import "encoding/json"

const (
	ToolGoalCreate           = "goal.create.v1"
	ToolGoalSearch           = "goal.search.v1"
	ToolReviewSearch         = "review.search.v1"
	ToolLatestPracticeReport = "practice.report.latest.v1"
)

// Builtins 返回 Agent 首批支持的用户可见斜杠命令。
func Builtins() []Definition {
	return []Definition{
		{
			Name:        "创建面试",
			Aliases:     []string{"面试", "准备面试"},
			Description: "创建或补全面试准备场景",
			ToolName:    ToolGoalCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{
					"title": args,
				})
			},
		},
		{
			Name:        "创建口语场景",
			Aliases:     []string{"口语场景", "新建场景"},
			Description: "创建职业英语口语场景",
			ToolName:    ToolGoalCreate,
			BuildInput: func(args string) (json.RawMessage, error) {
				return JSONObjectInput(map[string]any{
					"title": args,
				})
			},
		},
		{
			Name:        "继续场景",
			Aliases:     []string{"继续"},
			Description: "搜索并恢复相关的历史场景",
			ToolName:    ToolGoalSearch,
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
	}
}
