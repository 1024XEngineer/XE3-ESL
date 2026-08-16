package qianwen

func strictObjectSchema(required []any, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func stringArraySchema(maxItems int) map[string]any {
	result := map[string]any{
		"type":  "array",
		"items": stringSchema(),
	}
	if maxItems > 0 {
		result["maxItems"] = maxItems
	}
	return result
}

func objectArraySchema(item map[string]any, maxItems int) map[string]any {
	result := map[string]any{
		"type":  "array",
		"items": item,
	}
	if maxItems > 0 {
		result["maxItems"] = maxItems
	}
	return result
}
