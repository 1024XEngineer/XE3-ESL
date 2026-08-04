package capability

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

const maxSummaryStringRunes = 80

var sensitiveSummaryFields = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"token":         {},
	"session":       {},
	"session_id":    {},
	"user_id":       {},
	"owner_id":      {},
	"access_key":    {},
	"secret":        {},
	"signature":     {},
	"signed_url":    {},
	"url":           {},
	"resume":        {},
	"jd":            {},
	"review":        {},
	"audio":         {},
	"object_key":    {},
}

var textSummaryFields = map[string]struct{}{
	"query":              {},
	"content":            {},
	"text":               {},
	"excerpt":            {},
	"summary":            {},
	"background_summary": {},
	"answer":             {},
}

func SummarizeJSON(raw json.RawMessage) map[string]any {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return map[string]any{"valid_json": false, "bytes": len(raw)}
	}
	return summarizeObject(value)
}

func SummarizeResult(result Result) map[string]any {
	summary := summarizeObject(result.Content)
	if len(result.SourceRefs) > 0 {
		summary["source_ref_count"] = len(result.SourceRefs)
	}
	return summary
}

func summarizeObject(value map[string]any) map[string]any {
	summary := make(map[string]any, len(value))
	for key, raw := range value {
		normalized := strings.ToLower(key)
		if _, sensitive := sensitiveSummaryFields[normalized]; sensitive {
			summary[key] = "[redacted]"
			continue
		}
		if _, textField := textSummaryFields[normalized]; textField {
			if text, ok := raw.(string); ok {
				summary[key] = map[string]any{"length": utf8.RuneCountInString(text)}
				continue
			}
		}
		summary[key] = summarizeValue(raw)
	}
	return summary
}

func summarizeValue(value any) any {
	switch typed := value.(type) {
	case string:
		if looksLikeSignedURL(typed) {
			return "[redacted]"
		}
		return truncateString(typed, maxSummaryStringRunes)
	case []any:
		return map[string]any{"count": len(typed)}
	case map[string]any:
		return summarizeObject(typed)
	case nil:
		return nil
	case bool, float64:
		return typed
	default:
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() &&
			(reflected.Kind() == reflect.Slice ||
				reflected.Kind() == reflect.Array) {
			return map[string]any{"count": reflected.Len()}
		}
		return fmt.Sprintf("%T", value)
	}
}

func looksLikeSignedURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.Contains(lower, "x-oss-signature") ||
		strings.Contains(lower, "x-amz-signature")
}

func truncateString(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
