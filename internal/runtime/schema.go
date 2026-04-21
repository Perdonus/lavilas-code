package runtime

import "strings"

// NormalizeStrictJSONSchema makes object schemas compatible with strict
// OpenAI-style function calling by recursively forcing
// additionalProperties=false when the schema describes an object and the field
// is omitted.
func NormalizeStrictJSONSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	normalized, _ := normalizeSchemaValue(schema).(map[string]any)
	return normalized
}

func normalizeSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeSchemaMap(typed)
	case []any:
		items := make([]any, len(typed))
		for idx, item := range typed {
			items[idx] = normalizeSchemaValue(item)
		}
		return items
	default:
		return value
	}
}

func normalizeSchemaMap(schema map[string]any) map[string]any {
	normalized := make(map[string]any, len(schema)+1)
	for key, value := range schema {
		normalized[key] = normalizeSchemaValue(value)
	}
	if schemaAllowsObject(normalized["type"]) {
		if _, ok := normalized["additionalProperties"]; !ok {
			normalized["additionalProperties"] = false
		}
	}
	return normalized
}

func schemaAllowsObject(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "object")
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if ok && strings.EqualFold(strings.TrimSpace(text), "object") {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if strings.EqualFold(strings.TrimSpace(item), "object") {
				return true
			}
		}
	}
	return false
}
