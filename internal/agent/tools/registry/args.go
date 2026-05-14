package tools

import "strings"

// stringArg returns the trimmed string at key, or "" when missing / wrong type.
// Trim is applied at the boundary so callers do not need to re-trim
// (F-040 cleanup — most call sites already wrapped the result in TrimSpace,
// the few that didn't were the bug).
func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return cleanStrings(x)
	case []any:
		values := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return cleanStrings(values)
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
