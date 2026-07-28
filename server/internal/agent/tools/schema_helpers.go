package tools

// objectSchema 构造工具定义里使用的简单 JSON Schema 对象。
func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

// stringSchema 构造带说明的字符串字段 Schema。
func stringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}
