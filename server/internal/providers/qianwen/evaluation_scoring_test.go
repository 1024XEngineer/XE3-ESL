package qianwen

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
)

func TestEvaluationReportSchemaMatchesFormalReportBounds(t *testing.T) {
	keys := []string{
		"TASK_ACHIEVEMENT", "CLARITY_COHERENCE", "LANGUAGE_CONTROL", "INTERACTION",
	}
	schema, err := evaluationReportSchema(textgeneration.ReportContract{
		DimensionKeys: keys,
		ScoreMaximum:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	properties := schemaMap(t, schema, "properties")
	dimensions := schemaMap(t, properties, "dimensions")
	wantSlots := []any{"dimension_1", "dimension_2", "dimension_3", "dimension_4"}
	if dimensions["type"] != "object" || dimensions["additionalProperties"] != false ||
		!reflect.DeepEqual(dimensions["required"], wantSlots) {
		t.Fatalf("dimensions schema = %#v", dimensions)
	}
	dimensionSlots := schemaMap(t, dimensions, "properties")
	for index, wantKey := range keys {
		slot := fmt.Sprintf("dimension_%d", index+1)
		dimension := schemaMap(t, dimensionSlots, slot)
		dimensionProperties := schemaMap(t, dimension, "properties")
		key := schemaMap(t, dimensionProperties, "key")
		if !reflect.DeepEqual(key["enum"], []any{wantKey}) {
			t.Fatalf("%s key enum = %#v", slot, key["enum"])
		}
	}
	wantOrder := "dimension_1=TASK_ACHIEVEMENT, dimension_2=CLARITY_COHERENCE, dimension_3=LANGUAGE_CONTROL, dimension_4=INTERACTION"
	if !strings.Contains(dimensions["description"].(string), wantOrder) {
		t.Fatalf("dimensions description = %q", dimensions["description"])
	}
	dimension := schemaMap(t, dimensionSlots, "dimension_1")
	dimensionProperties := schemaMap(t, dimension, "properties")
	score := schemaMap(t, dimensionProperties, "score")
	number := score["anyOf"].([]any)[0].(map[string]any)
	if number["minimum"] != 0 || number["maximum"] != float64(100) {
		t.Fatalf("score schema = %#v", number)
	}
	for _, name := range []string{"coverage", "confidence"} {
		ratio := schemaMap(t, dimensionProperties, name)
		if ratio["minimum"] != 0 || ratio["maximum"] != 1 {
			t.Fatalf("%s schema = %#v", name, ratio)
		}
	}
	summary := schemaMap(t, properties, "summary")
	if summary["minLength"] != 1 || summary["maxLength"] != 2048/utf8.UTFMax {
		t.Fatalf("summary schema = %#v", summary)
	}
}

func TestEvaluationReportSchemaStringLimitsCannotExceedDomainUTF8ByteLimits(t *testing.T) {
	schema, err := evaluationReportSchema(textgeneration.ReportContract{
		DimensionKeys: []string{"INTERACTION"}, ScoreMaximum: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	properties := schemaMap(t, schema, "properties")
	dimensions := schemaMap(t, schemaMap(t, properties, "dimensions"), "properties")
	dimension := schemaMap(t, dimensions, "dimension_1")
	findingItems := schemaMap(t,
		schemaMap(t, schemaMap(t, dimension, "properties"), "strengths"), "items",
	)
	findingProperties := schemaMap(t, findingItems, "properties")
	evidenceItems := schemaMap(t, schemaMap(t, findingProperties, "evidence"), "items")
	evidenceProperties := schemaMap(t, evidenceItems, "properties")

	assertUTF8Bound := func(name string, value map[string]any, maximumBytes int) {
		t.Helper()
		maximumCharacters, ok := value["maxLength"].(int)
		if !ok || maximumCharacters*utf8.UTFMax > maximumBytes {
			t.Fatalf("%s schema = %#v; may exceed %d UTF-8 bytes", name, value, maximumBytes)
		}
	}
	assertUTF8Bound("summary", schemaMap(t, properties, "summary"), 2048)
	assertUTF8Bound("message", schemaMap(t, findingProperties, "message"), 2048)
	assertUTF8Bound("suggestion", schemaMap(t, findingProperties, "suggestion"), 2048)
	assertUTF8Bound("quote", schemaMap(t, evidenceProperties, "quote"), 16*1024)
}

func TestEvaluationReportSchemaUsesIELTSScoreRange(t *testing.T) {
	schema, err := evaluationReportSchema(textgeneration.ReportContract{
		DimensionKeys: []string{"FLUENCY_COHERENCE", "LEXICAL_RESOURCE"},
		ScoreMaximum:  9,
	})
	if err != nil {
		t.Fatal(err)
	}
	dimensions := schemaMap(t, schemaMap(t, schema, "properties"), "dimensions")
	dimension := schemaMap(t, schemaMap(t, dimensions, "properties"), "dimension_1")
	score := schemaMap(t, schemaMap(t, dimension, "properties"), "score")
	number := score["anyOf"].([]any)[0].(map[string]any)
	if number["minimum"] != 0 || number["maximum"] != float64(9) {
		t.Fatalf("IELTS score schema = %#v", number)
	}
}

func TestEvaluationReportSchemaRejectsDuplicateDimensionKeys(t *testing.T) {
	if _, err := evaluationReportSchema(textgeneration.ReportContract{
		DimensionKeys: []string{"INTERACTION", "INTERACTION"},
		ScoreMaximum:  100,
	}); err == nil {
		t.Fatal("duplicate dimension keys were accepted")
	}
}

func schemaMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("schema[%q] = %#v", key, source[key])
	}
	return value
}
