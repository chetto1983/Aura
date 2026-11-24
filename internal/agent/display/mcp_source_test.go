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
	calendar := map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "get_emails"}}
	got = TrustedMCPFromMeta(calendar, "pim__get_emails", `{"accountId":"a1"}`)
	if got == nil || got.Recipe != "recipe:calendar" || got.Tool != "get_emails" || got.Action != "" {
		t.Fatalf("calendar marker = %+v", got)
	}
	legacy := map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "get_emails"}}
	got = TrustedMCPFromMeta(legacy, "pim__calendar", `{"action":"get_emails"}`)
	if got == nil || got.Tool != "calendar" || got.Action != "get_emails" {
		t.Fatalf("legacy calendar marker = %+v", got)
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
		{"calendar action mismatch", legacy, "alias__calendar", `{"action":"send_email"}`},
		{"calendar wrong tool", calendar, "pim__send_email", `{}`},
		{"legacy marker naming a split tool", map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "get_emails", "action": "get_emails"}}, "pim__get_emails", `{"action":"get_emails"}`},
		{"split marker naming the old tool", map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar"}}, "pim__calendar", `{"action":"get_emails"}`},
		{"calendar extra field", map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "get_emails", "admin": true}}, "pim__calendar", `{"action":"get_emails"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrustedMCPFromMeta(tc.meta, tc.tool, tc.args); got != nil {
				t.Fatalf("unexpected marker %+v", got)
			}
		})
	}
}
