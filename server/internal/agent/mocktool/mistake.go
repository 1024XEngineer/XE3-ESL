package mocktool

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

type MistakeSearchInput struct {
	Query      string `json:"query"`
	ScenarioID string `json:"scenario_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type MistakeSearchTool struct {
	store *Store
}

func NewMistakeSearchTool(store *Store) MistakeSearchTool {
	return MistakeSearchTool{store: store}
}

func (t MistakeSearchTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        MistakeSearchToolName,
		Description: "Search recurring speaking mistakes and coaching suggestions. Use when the user asks for 历史错题, 以前的错误, recurring mistakes, pronunciation issues, grammar patterns, or repeated expression problems. Do not use for correcting only the current sentence.",
		InputSchema: tool.ObjectSchema(map[string]any{
			"query":       tool.StringSchema("Mistake category or phrasing to find."),
			"scenario_id": tool.StringSchema("Optional scenario id to restrict the search."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of mistakes to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     tool.RiskReadOnly,
	}
}

func (t MistakeSearchTool) Execute(
	ctx context.Context,
	call tool.CallContext,
	input json.RawMessage,
) (tool.Result, error) {
	if t.store == nil {
		return tool.Result{}, tool.ErrToolRejected
	}
	var parsed MistakeSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Query == "" {
		return tool.Result{}, tool.ErrInvalidInput
	}
	mistakes, err := t.store.SearchMistakes(MistakeSearchToolName, parsed)
	if err != nil {
		return tool.Result{}, err
	}
	items := make([]map[string]any, 0, len(mistakes))
	refs := make([]tool.SourceRef, 0, len(mistakes))
	for _, mistake := range mistakes {
		ref := tool.SourceRef{Type: "mock_mistake", ID: mistake.ID}
		items = append(items, map[string]any{
			"id":          mistake.ID,
			"scenario_id": mistake.ScenarioID,
			"category":    mistake.Category,
			"summary":     mistake.Summary,
			"suggestion":  mistake.Suggestion,
			"source_ref":  ref,
		})
		refs = append(refs, ref)
	}
	return tool.Result{
		Content:    map[string]any{"mistakes": items},
		SourceRefs: refs,
	}, nil
}
