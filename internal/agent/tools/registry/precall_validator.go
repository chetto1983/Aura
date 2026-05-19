package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/aura/aura/internal/llm"
)

type TypeMismatch struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type EnumMismatch struct {
	Field   string `json:"field"`
	Allowed []any  `json:"allowed"`
	Actual  any    `json:"actual"`
}

type ValidationResult struct {
	Valid       bool           `json:"valid"`
	Missing     []string       `json:"missing"`
	InvalidType []TypeMismatch `json:"invalid_type"`
	InvalidEnum []EnumMismatch `json:"invalid_enum"`
	Hint        string         `json:"hint"`
	Recoverable bool           `json:"recoverable"`
}

type ValidationError struct {
	Tool   string           `json:"tool"`
	Result ValidationResult `json:"validation_error"`
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	payload, err := json.Marshal(struct {
		Tool            string           `json:"tool"`
		ValidationError ValidationResult `json:"validation_error"`
	}{
		Tool:            e.Tool,
		ValidationError: e.Result.normalized(),
	})
	if err != nil {
		return fmt.Sprintf("tool %q schema validation failed", e.Tool)
	}
	return string(payload)
}

func (e *ValidationError) Unwrap() error {
	return llm.ErrSchemaValidation
}

// ValidateBeforeCall checks the JSON-schema subset Aura tools actually expose:
// required object keys, primitive types, arrays/objects, and enum membership.
func ValidateBeforeCall(toolName string, params map[string]any, schema map[string]any) ValidationResult {
	result := ValidationResult{
		Valid:       true,
		Missing:     []string{},
		InvalidType: []TypeMismatch{},
		InvalidEnum: []EnumMismatch{},
	}
	if len(schema) == 0 || !schemaIsObjectLike(schema) {
		return result
	}
	if params == nil {
		params = map[string]any{}
	}

	validateObjectParams(&result, "", params, schema)
	result.Valid = len(result.Missing) == 0 && len(result.InvalidType) == 0 && len(result.InvalidEnum) == 0
	if !result.Valid {
		result.Recoverable = true
		result.Hint = validationHint(toolName, result)
	}
	return result.normalized()
}

func validateObjectParams(result *ValidationResult, path string, params map[string]any, schema map[string]any) {
	for _, key := range requiredKeys(schema) {
		if _, ok := params[key]; !ok || params[key] == nil {
			result.Missing = append(result.Missing, joinFieldPath(path, key))
		}
	}

	properties := schemaProperties(schema)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, ok := params[key]
		if !ok || value == nil {
			continue
		}
		validateValue(result, joinFieldPath(path, key), value, properties[key])
	}
}

func validateValue(result *ValidationResult, path string, value any, schema map[string]any) {
	if len(schema) == 0 {
		return
	}
	types := schemaTypes(schema)
	if value == nil {
		if len(types) == 0 || containsSchemaType(types, "null") {
			return
		}
		result.InvalidType = append(result.InvalidType, TypeMismatch{Field: path, Expected: strings.Join(types, "|"), Actual: "null"})
		return
	}

	if len(types) > 0 && !matchesAnySchemaType(value, types) {
		result.InvalidType = append(result.InvalidType, TypeMismatch{
			Field:    path,
			Expected: strings.Join(nonNullTypes(types), "|"),
			Actual:   valueTypeName(value),
		})
		return
	}
	if values, ok := enumValues(schema); ok && !enumMatches(value, values) {
		result.InvalidEnum = append(result.InvalidEnum, EnumMismatch{Field: path, Allowed: values, Actual: value})
	}

	if schemaIsObjectLike(schema) {
		if objectValue, ok := asStringAnyMap(value); ok {
			validateObjectParams(result, path, objectValue, schema)
		}
	}
	if preferredSchemaType(schema) == "array" {
		itemsSchema, ok := schemaMap(schema["items"])
		if !ok {
			return
		}
		items, ok := anySlice(value)
		if !ok {
			return
		}
		for i, item := range items {
			validateValue(result, fmt.Sprintf("%s[%d]", path, i), item, itemsSchema)
		}
	}
}

func (r ValidationResult) normalized() ValidationResult {
	if r.Missing == nil {
		r.Missing = []string{}
	}
	if r.InvalidType == nil {
		r.InvalidType = []TypeMismatch{}
	}
	if r.InvalidEnum == nil {
		r.InvalidEnum = []EnumMismatch{}
	}
	return r
}

func validationHint(toolName string, result ValidationResult) string {
	parts := make([]string, 0, 3)
	if len(result.Missing) > 0 {
		parts = append(parts, "provide required field(s): "+strings.Join(result.Missing, ", "))
	}
	if len(result.InvalidType) > 0 {
		fields := make([]string, 0, len(result.InvalidType))
		for _, item := range result.InvalidType {
			fields = append(fields, fmt.Sprintf("%s expects %s, got %s", item.Field, item.Expected, item.Actual))
		}
		parts = append(parts, "fix type(s): "+strings.Join(fields, "; "))
	}
	if len(result.InvalidEnum) > 0 {
		fields := make([]string, 0, len(result.InvalidEnum))
		for _, item := range result.InvalidEnum {
			fields = append(fields, item.Field)
		}
		parts = append(parts, "choose allowed enum value for: "+strings.Join(fields, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Invalid arguments for %s: %s.", toolName, strings.Join(parts, " "))
}

func schemaIsObjectLike(schema map[string]any) bool {
	types := schemaTypes(schema)
	if len(types) == 0 {
		return len(schemaProperties(schema)) > 0 || len(requiredKeys(schema)) > 0 || schema["type"] == nil
	}
	return containsSchemaType(types, "object")
}

func schemaProperties(schema map[string]any) map[string]map[string]any {
	raw, ok := schema["properties"]
	if !ok || raw == nil {
		return nil
	}
	rawProps, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	props := make(map[string]map[string]any, len(rawProps))
	for key, value := range rawProps {
		if prop, ok := schemaMap(value); ok {
			props[key] = prop
		}
	}
	return props
}

func requiredKeys(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok || raw == nil {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		out := append([]string(nil), values...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func schemaMap(value any) (map[string]any, bool) {
	out, ok := value.(map[string]any)
	return out, ok
}

func schemaTypes(schema map[string]any) []string {
	raw, ok := schema["type"]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []string{strings.TrimSpace(value)}
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func preferredSchemaType(schema map[string]any) string {
	for _, item := range nonNullTypes(schemaTypes(schema)) {
		return item
	}
	if len(schemaProperties(schema)) > 0 {
		return "object"
	}
	return ""
}

func nonNullTypes(types []string) []string {
	out := make([]string, 0, len(types))
	for _, item := range types {
		if item != "null" {
			out = append(out, item)
		}
	}
	return out
}

func containsSchemaType(types []string, want string) bool {
	for _, item := range types {
		if item == want {
			return true
		}
	}
	return false
}

func matchesAnySchemaType(value any, types []string) bool {
	for _, typ := range types {
		if typ == "null" {
			continue
		}
		if matchesSchemaType(value, typ) {
			return true
		}
	}
	return false
}

func matchesSchemaType(value any, typ string) bool {
	switch typ {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		n, ok := numericValue(value)
		return ok && math.Trunc(n) == n
	case "number":
		_, ok := numericValue(value)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := asStringAnyMap(value)
		return ok
	case "array":
		_, ok := anySlice(value)
		return ok
	default:
		return true
	}
}

func valueTypeName(value any) string {
	if value == nil {
		return "null"
	}
	if _, ok := value.(string); ok {
		return "string"
	}
	if _, ok := value.(bool); ok {
		return "boolean"
	}
	if _, ok := numericValue(value); ok {
		return "number"
	}
	if _, ok := asStringAnyMap(value); ok {
		return "object"
	}
	if _, ok := anySlice(value); ok {
		return "array"
	}
	return reflect.TypeOf(value).String()
}

func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		if n, err := v.Float64(); err == nil {
			return n, true
		}
	}
	return 0, false
}

func enumValues(schema map[string]any) ([]any, bool) {
	raw, ok := schema["enum"]
	if !ok || raw == nil {
		return nil, false
	}
	switch values := raw.(type) {
	case []any:
		return append([]any(nil), values...), true
	case []string:
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out, true
	default:
		rv := reflect.ValueOf(raw)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return nil, false
		}
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, rv.Index(i).Interface())
		}
		return out, true
	}
}

func enumMatches(value any, values []any) bool {
	for _, allowed := range values {
		if reflect.DeepEqual(value, allowed) {
			return true
		}
		if valueNumber, ok := numericValue(value); ok {
			if allowedNumber, ok := numericValue(allowed); ok && valueNumber == allowedNumber {
				return true
			}
		}
	}
	return false
}

func asStringAnyMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if out, ok := value.(map[string]any); ok {
		return out, true
	}
	return nil, false
}

func anySlice(value any) ([]any, bool) {
	switch items := value.(type) {
	case []any:
		return items, true
	default:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
			return nil, false
		}
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, rv.Index(i).Interface())
		}
		return out, true
	}
}

func joinFieldPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}
