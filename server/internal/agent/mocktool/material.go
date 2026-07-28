package mocktool

import (
	"context"
	"encoding/json"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/tool"
)

type MaterialSearchInput struct {
	Query string `json:"query"`
	Kind  string `json:"kind,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type MaterialSearchTool struct {
	store *Store
}

func NewMaterialSearchTool(store *Store) MaterialSearchTool {
	return MaterialSearchTool{store: store}
}

func (t MaterialSearchTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        MaterialSearchToolName,
		Description: "Search resume and job description materials for relevant facts. Use when the user asks to combine 简历, 履历, JD, 岗位要求, resume, or job description context with English practice. Do not use for generic wording help without material context.",
		InputSchema: tool.ObjectSchema(map[string]any{
			"query": tool.StringSchema("Resume or JD facts to find."),
			"kind":  tool.StringSchema("Optional material kind: resume or jd."),
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of material snippets to return.",
			},
		}, []string{"query"}),
		ReadOnly: true,
		Risk:     tool.RiskReadOnly,
	}
}

func (t MaterialSearchTool) Execute(
	ctx context.Context,
	call tool.CallContext,
	input json.RawMessage,
) (tool.Result, error) {
	if t.store == nil {
		return tool.Result{}, tool.ErrToolRejected
	}
	var parsed MaterialSearchInput
	if err := json.Unmarshal(input, &parsed); err != nil ||
		parsed.Query == "" ||
		(parsed.Kind != "" && parsed.Kind != "resume" && parsed.Kind != "jd") {
		return tool.Result{}, tool.ErrInvalidInput
	}
	materials, err := t.store.SearchMaterials(MaterialSearchToolName, parsed)
	if err != nil {
		return tool.Result{}, err
	}
	items := make([]map[string]any, 0, len(materials))
	refs := make([]tool.SourceRef, 0, len(materials))
	for _, material := range materials {
		ref := tool.SourceRef{Type: "mock_material", ID: material.ID}
		items = append(items, map[string]any{
			"id":         material.ID,
			"kind":       material.Kind,
			"title":      material.Title,
			"excerpt":    material.Excerpt,
			"source_ref": ref,
		})
		refs = append(refs, ref)
	}
	return tool.Result{
		Content:    map[string]any{"materials": items},
		SourceRefs: refs,
	}, nil
}
