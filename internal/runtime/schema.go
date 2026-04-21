package runtime

import (
	"sort"
	"strings"
)

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
		normalized["required"] = normalizeRequiredKeys(normalized["properties"], normalized["required"])
		if _, ok := normalized["additionalProperties"]; !ok {
			normalized["additionalProperties"] = false
		}
	}
	return normalized
}

func normalizeRequiredKeys(propertiesValue any, requiredValue any) []string {
	properties, ok := propertiesValue.(map[string]any)
	if !ok || len(properties) == 0 {
		switch typed := requiredValue.(type) {
		case []string:
			return append([]string(nil), typed...)
		case []any:
			result := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok {
					trimmed := strings.TrimSpace(text)
					if trimmed != "" {
						result = append(result, trimmed)
					}
				}
			}
			return result
		default:
			return []string{}
		}
	}

	seen := make(map[string]struct{}, len(properties))
	result := make([]string, 0, len(properties))
	missing := make([]string, 0, len(properties))

	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, ok := properties[trimmed]; !ok {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	switch typed := requiredValue.(type) {
	case []string:
		for _, item := range typed {
			add(item)
		}
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				add(text)
			}
		}
	}

	for key := range properties {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		missing = append(missing, trimmed)
	}
	sort.Strings(missing)
	for _, key := range missing {
		add(key)
	}
	return result
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
