package capability

// objectSchema 构造工具定义里使用的简单 JSON Schema 对象。
func objectSchema(properties map[string]any, required []string) map[string]any {
	return ObjectSchema(properties, required)
}

// ObjectSchema 构造工具定义里使用的简单 JSON Schema 对象。
func ObjectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

// stringSchema 构造带说明的字符串字段 Schema。
func stringSchema(description string) map[string]any {
	return StringSchema(description)
}

// StringSchema 构造带说明的字符串字段 Schema。
func StringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

// TextSchema 构造必须包含非空文本且有最大长度的字符串 Schema。
func TextSchema(description string, maximumLength int) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"format":      "non-empty-text",
		"minLength":   1,
		"maxLength":   maximumLength,
	}
}

// OptionalTextSchema allows an omitted field or an empty string while still
// bounding non-empty content. Domain parsers remain responsible for trimming
// and any field-specific semantic validation.
func OptionalTextSchema(description string, maximumLength int) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"maxLength":   maximumLength,
	}
}

// IdentifierSchema 构造 Agent 领域 ID Schema，允许字母、数字和常用分隔符。
func IdentifierSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"format":      "agent-id",
		"minLength":   1,
		"maxLength":   128,
	}
}

// StringEnumSchema 构造字符串枚举 Schema，帮助模型只生成业务允许的值。
func StringEnumSchema(description string, values ...string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"enum":        append([]string(nil), values...),
	}
}

// IntegerRangeSchema 构造包含上下界的整数 Schema。
func IntegerRangeSchema(
	description string,
	minimum int,
	maximum int,
) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
		"maximum":     maximum,
	}
}
