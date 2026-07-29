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
		Description: "Search the current user's saved resume and job-description materials and return relevant factual snippets. Use when a response must be grounded in the user's resume, work history, JD, role requirements, or matching experience. Do not use for generic wording help, historical reviews, mistakes, or facts the user already supplied directly in the current message.",
		InputSchema: tool.ObjectSchema(map[string]any{
			"query": tool.TextSchema(
				"Facts, skills, experience, or requirements to find.",
				500,
			),
			"kind": tool.StringEnumSchema(
				"Optional material category used to narrow the search.",
				"resume",
				"jd",
			),
			"limit": tool.IntegerRangeSchema(
				"Maximum number of material snippets to return.",
				1,
				20,
			),
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
		return tool.Result{}, tool.ErrExecutionRejected
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
