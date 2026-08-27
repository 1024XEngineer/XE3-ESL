package qianwen

import (
	"reflect"
	"strings"
	"testing"

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
	if dimensions["minItems"] != len(keys) || dimensions["maxItems"] != len(keys) ||
		!strings.Contains(dimensions["description"].(string), strings.Join(keys, ", ")) {
		t.Fatalf("dimensions schema = %#v", dimensions)
	}
	dimension := schemaMap(t, dimensions, "items")
	dimensionProperties := schemaMap(t, dimension, "properties")
	key := schemaMap(t, dimensionProperties, "key")
	wantEnum := []any{
		"TASK_ACHIEVEMENT", "CLARITY_COHERENCE", "LANGUAGE_CONTROL", "INTERACTION",
	}
	if !reflect.DeepEqual(key["enum"], wantEnum) {
		t.Fatalf("dimension key enum = %#v", key["enum"])
	}
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
	if summary["minLength"] != 1 || summary["maxLength"] != 2048 {
		t.Fatalf("summary schema = %#v", summary)
	}
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
	dimension := schemaMap(t, dimensions, "items")
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
