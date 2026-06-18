package display

import (
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// TestNormalizeDispatch (DISP-01): each in-scope tool name routes to the right
// normalizer and yields the matching Payload kind.
func TestNormalizeDispatch(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		result any
		want   Kind
	}{
		{"web_search", "web_search", []web.Result{{Title: "A", URL: "https://a.test/x"}}, KindWebResult},
		{"web_fetch", "web_fetch", web.Page{Title: "P", URL: "https://a.test/p", ContentMD: "# P"}, KindDocument},
		{"swarm_spawn", "swarm_spawn", []ChildReport{{GoalIndex: 0, ChildID: "w1", Status: StatusOK}}, KindSwarmReport},
		{"shell_exec", "shell_exec", CodeInput{Body: "ok", Lang: "bash"}, KindCode},
		{"sandbox_exec", "sandbox_exec", CodeInput{Body: "ok"}, KindCode},
		{"web_error", "web_fetch", &web.WebError{Code: web.CodeTimeout}, KindSystemEvent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := Normalize(tc.tool, tc.result)
			if !ok {
				t.Fatalf("Normalize(%q) recognized=false, want true", tc.tool)
			}
			if p.Type != tc.want {
				t.Fatalf("Normalize(%q) type = %q, want %q", tc.tool, p.Type, tc.want)
			}
		})
	}
}

// TestNormalizeUnknownToolFallback (D-FALLBACK): an unrecognized tool returns
// (Payload{}, false) so the caller renders the raw escaped card.
func TestNormalizeUnknownToolFallback(t *testing.T) {
	p, ok := Normalize("unknown_tool", "whatever")
	if ok {
		t.Fatalf("unknown tool recognized=true, want false (D-FALLBACK)")
	}
	if p.Type != "" {
		t.Fatalf("unknown tool returned a typed payload: %+v", p)
	}
}

// TestNormalizeWrongResultTypeFallback: a recognized tool handed a result of the
// wrong concrete type falls through to D-FALLBACK rather than panicking.
func TestNormalizeWrongResultTypeFallback(t *testing.T) {
	for _, tool := range []string{"web_search", "web_fetch", "swarm_spawn", "shell_exec"} {
		if _, ok := Normalize(tool, 42); ok {
			t.Fatalf("Normalize(%q, wrong-type) recognized=true, want false", tool)
		}
	}
}

// TestNormalizeWithRegistryAccumulates (D-05): two web_search calls sharing a
// Registry accumulate into one numbered source list with stable cross-call RefIDs.
func TestNormalizeWithRegistryAccumulates(t *testing.T) {
	reg := NewRegistry()
	p1, _ := NormalizeWithRegistry("call-1", "web_search", []web.Result{{Title: "A", URL: "https://a.test/x"}}, reg)
	p2, _ := NormalizeWithRegistry("call-2", "web_search", []web.Result{
		{Title: "A", URL: "https://a.test/x"}, // same URL as call-1 -> same RefID
		{Title: "B", URL: "https://b.test/y"},
	}, reg)

	if p1.WebResults[0].RefID != "src-1" {
		t.Fatalf("call-1 RefID = %q, want src-1", p1.WebResults[0].RefID)
	}
	if p2.WebResults[0].RefID != "src-1" {
		t.Fatalf("repeated URL across calls got RefID %q, want src-1", p2.WebResults[0].RefID)
	}
	if p2.WebResults[1].RefID != "src-2" {
		t.Fatalf("new URL RefID = %q, want src-2", p2.WebResults[1].RefID)
	}
	if got := reg.RenderSourceList(); got == "" || len(reg.Sources()) != 2 {
		t.Fatalf("shared registry did not accumulate to 2 sources: %d, list=%q", len(reg.Sources()), got)
	}
}
