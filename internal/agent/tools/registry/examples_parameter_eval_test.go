package tools

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestExamplesParameterEval validates that every ToolCallExample.Arguments
// parses cleanly through the corresponding tool's Parameters JSONSchema.
//
// The hand-rolled validator checks three things (per US-I05 spec):
//   - required-field presence: every field in the schema's "required" list must
//     appear in the example Arguments.
//   - type match: for each arg key that exists in schema "properties", the Go
//     value's concrete type must be consistent with the schema "type" field.
//   - enum membership: for properties that declare an "enum", the arg value must
//     be one of the listed members.
//
// Swarm tools (run_aurabot_swarm, read_swarm_result, list_swarm_tasks) cannot be
// instantiated here due to the swarmtools→registry import cycle; they are covered
// in internal/agent/tools/swarm/tools_test.go.
func TestExamplesParameterEval(t *testing.T) {
	toolsToCheck := []Tool{
		&AskUserTool{},
		&RequestDashboardTokenTool{},
		&ToolSearchTool{},
		&SearchMemoryTool{},
		&WebTool{},
		&WikiPageTool{},
		&TaskTool{loc: time.Local},
		&SourceTool{},
		&FileTool{},
		&DocTool{},
		&ExecuteCodeTool{},
		&ExecuteShellTool{},
		&DevToolTool{},
		&DailyBriefingTool{loc: time.Local, now: time.Now},
	}

	var failures []string
	for _, tool := range toolsToCheck {
		def := definitionForTool(tool)
		for i, ex := range def.Examples {
			errs := validateExampleArguments(def.Parameters, ex.Arguments)
			for _, e := range errs {
				failures = append(failures, fmt.Sprintf(
					"  tool=%s example[%d]: %s", def.Name, i, e,
				))
			}
		}
	}

	if len(failures) > 0 {
		t.Errorf("parameter eval: %d example argument(s) fail schema validation:\n%s",
			len(failures), strings.Join(failures, "\n"))
	}
}

// validateExampleArguments checks one example's Arguments against the tool's
// Parameters JSONSchema. Returns a list of failure strings (empty = valid).
func validateExampleArguments(params, args map[string]any) []string {
	if params == nil {
		return nil
	}
	properties, _ := params["properties"].(map[string]any)
	required := requiredSchemaFields(params)

	var errs []string

	// Required-field presence.
	for _, req := range required {
		if _, ok := args[req]; !ok {
			errs = append(errs, fmt.Sprintf("required field %q missing", req))
		}
	}

	// Type and enum checks for fields present in properties.
	for argKey, argVal := range args {
		prop, ok := properties[argKey]
		if !ok {
			// Extra field not in properties — skip (not our job).
			continue
		}
		propMap, _ := prop.(map[string]any)
		if propMap == nil {
			continue
		}

		// Type check.
		if schemaType, ok := propMap["type"].(string); ok && schemaType != "" {
			if !exampleArgMatchesSchemaType(argVal, schemaType) {
				errs = append(errs, fmt.Sprintf(
					"field %q: type mismatch (schema=%q, got %T)",
					argKey, schemaType, argVal,
				))
			}
		}

		// Enum membership.
		errs = append(errs, checkEnumMembership(argKey, argVal, propMap)...)
	}

	return errs
}

// exampleArgMatchesSchemaType returns true when the Go value v is consistent
// with the JSON Schema type string. Unknown types pass silently.
func exampleArgMatchesSchemaType(v any, schemaType string) bool {
	if v == nil {
		return true // nil is acceptable; required-presence check handles absence.
	}
	switch schemaType {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer", "number":
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return true
		}
		return false
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "array":
		rv := reflect.ValueOf(v)
		return rv.IsValid() && rv.Kind() == reflect.Slice
	case "object":
		_, ok := v.(map[string]any)
		return ok
	default:
		return true // unknown schema type — skip
	}
}

// checkEnumMembership returns an error string if prop declares an "enum" and
// v is not a member. Returns nil when there is no enum constraint or v is a member.
func checkEnumMembership(field string, v any, propMap map[string]any) []string {
	enumRaw, hasEnum := propMap["enum"]
	if !hasEnum {
		return nil
	}
	vStr := fmt.Sprint(v)
	switch enumVals := enumRaw.(type) {
	case []string:
		for _, e := range enumVals {
			if vStr == e {
				return nil
			}
		}
		return []string{fmt.Sprintf("field %q: value %q not in enum %v", field, vStr, enumVals)}
	case []any:
		for _, e := range enumVals {
			if vStr == fmt.Sprint(e) {
				return nil
			}
		}
		return []string{fmt.Sprintf("field %q: value %q not in enum %v", field, vStr, enumVals)}
	}
	return nil
}
