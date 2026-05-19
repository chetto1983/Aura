package tools

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CoerceParams returns a schema-guided shallow copy of params. It only applies
// casts that are safe and common in model-emitted tool calls: stringified
// numerics, booleans, enum-as-string values, nested objects, and array items.
func CoerceParams(params map[string]any, schema map[string]any) map[string]any {
	out := cloneAnyMap(params)
	if len(out) == 0 || len(schema) == 0 {
		return out
	}
	return coerceObject(out, schema)
}

func coerceValue(value any, schema map[string]any) any {
	if value == nil || len(schema) == 0 {
		return value
	}

	switch preferredSchemaType(schema) {
	case "integer":
		value = coerceInteger(value)
	case "number":
		value = coerceNumber(value)
	case "boolean":
		value = coerceBoolean(value)
	case "object":
		if obj, ok := asStringAnyMap(value); ok {
			value = coerceObject(obj, schema)
		}
	case "array":
		value = coerceArray(value, schema)
	}

	if coerced, ok := coerceEnumAsString(value, schema); ok {
		return coerced
	}
	return value
}

func coerceObject(params map[string]any, schema map[string]any) map[string]any {
	out := cloneAnyMap(params)
	properties := schemaProperties(schema)
	for key, propSchema := range properties {
		value, ok := out[key]
		if !ok {
			continue
		}
		out[key] = coerceValue(value, propSchema)
	}
	return out
}

func coerceArray(value any, schema map[string]any) any {
	itemsSchema, ok := schemaMap(schema["items"])
	if !ok {
		return value
	}
	items, ok := anySlice(value)
	if !ok {
		return value
	}
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = coerceValue(item, itemsSchema)
	}
	return out
}

func coerceInteger(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return value
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return int(n)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.Trunc(f) != f {
		return value
	}
	return int(f)
}

func coerceNumber(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return value
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return value
	}
	return n
}

func coerceBoolean(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return value
	}
}

func coerceEnumAsString(value any, schema map[string]any) (any, bool) {
	if _, ok := value.(string); ok {
		return value, false
	}
	values, ok := enumValues(schema)
	if !ok || len(values) == 0 {
		return nil, false
	}
	candidate := fmt.Sprint(value)
	for _, allowed := range values {
		allowedString, ok := allowed.(string)
		if ok && candidate == allowedString {
			return candidate, true
		}
	}
	return nil, false
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
