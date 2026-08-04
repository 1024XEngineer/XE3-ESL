package capabilityfixture

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

type MistakeSearchInput struct {
	Query   string `json:"query"`
	SceneID string `json:"scene_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
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
		Description: "Search the current user's saved historical speaking mistakes and coaching suggestions. Use for recurring grammar, pronunciation, clarity, structure, or expression problems observed across earlier practice. Do not use to correct only the current utterance, inspect a review, or search resume and job-description materials.",
		InputSchema: tool.ObjectSchema(map[string]any{
			"query": tool.TextSchema(
				"Mistake category, language pattern, or coaching topic to find.",
				500,
			),
			"scene_id": tool.IdentifierSchema(
				"Optional existing Scene id used to narrow the mistake search.",
			),
			"limit": tool.IntegerRangeSchema(
				"Maximum number of historical mistakes to return.",
				1,
				20,
			),
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
		return tool.Result{}, tool.ErrExecutionRejected
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
			"id":         mistake.ID,
			"scene_id":   mistake.SceneID,
			"category":   mistake.Category,
			"summary":    mistake.Summary,
			"suggestion": mistake.Suggestion,
			"source_ref": ref,
		})
		refs = append(refs, ref)
	}
	return tool.Result{
		Content:    map[string]any{"mistakes": items},
		SourceRefs: refs,
	}, nil
}
