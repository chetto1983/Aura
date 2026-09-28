package display

import "testing"

func TestMCPDisplaySourceRequiresExactMarker(t *testing.T) {
	valid := map[string]any{"aura_display_source": map[string]any{
		"recipe": "recipe:memory", "tool": "memory_search",
	}}
	got := TrustedMCPFromMeta(valid, "alias__memory_search", `{}`)
	if got == nil || got.Recipe != "recipe:memory" || got.Tool != "memory_search" || got.Action != "" {
		t.Fatalf("valid marker = %+v", got)
	}
	for _, tc := range []struct {
		name string
		meta map[string]any
		tool string
		args string
	}{
		{"missing", nil, "alias__memory_search", `{}`},
		{"wrong tool", valid, "alias__memory_recall", `{}`},
		{"unknown recipe", map[string]any{"aura_display_source": map[string]any{"recipe": "other", "tool": "memory_search"}}, "alias__memory_search", `{}`},
		{"extra field", map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:memory", "tool": "memory_search", "admin": true}}, "alias__memory_search", `{}`},
		{"calendar action mismatch", map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "get_emails"}}, "alias__calendar", `{"action":"send_email"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrustedMCPFromMeta(tc.meta, tc.tool, tc.args); got != nil {
				t.Fatalf("unexpected marker %+v", got)
			}
		})
	}
}
