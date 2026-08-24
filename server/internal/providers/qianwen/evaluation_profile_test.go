package qianwen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIELTSCumulativeProfileSchemaUsesCompatibleBandConstraints(t *testing.T) {
	encoded, err := json.Marshal(ieltsCumulativeProfileSchema())
	if err != nil {
		t.Fatal(err)
	}
	schema := string(encoded)
	if strings.Contains(schema, `"multipleOf"`) {
		t.Fatalf("profile schema contains unsupported multipleOf: %s", schema)
	}
	for _, field := range []string{"provisional_band_low", "provisional_band_high"} {
		if !strings.Contains(schema, `"`+field+`":{"maximum":9,"minimum":0,"type":"number"}`) {
			t.Fatalf("profile schema does not constrain %s to a 0-9 number: %s", field, schema)
		}
	}
}
