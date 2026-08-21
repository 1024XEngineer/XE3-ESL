package capability

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestProfileToolSummariesRedactPersonalData(t *testing.T) {
	input := json.RawMessage(`{
		"patch":{"occupation":"秘密职业","form_of_address":"秘密称呼"},
		"evidence":{"occupation":"我是秘密职业"}
	}`)
	result := Result{Content: map[string]any{
		"coaching_profile": map[string]any{
			"profile": map[string]any{"occupation": "秘密职业"},
		},
	}}
	for name, summary := range map[string]map[string]any{
		"input":  SummarizeJSON(input),
		"result": SummarizeResult(result),
	} {
		encoded := fmt.Sprint(summary)
		if strings.Contains(encoded, "秘密职业") ||
			strings.Contains(encoded, "秘密称呼") {
			t.Fatalf("%s summary leaked profile value: %s", name, encoded)
		}
	}
}
