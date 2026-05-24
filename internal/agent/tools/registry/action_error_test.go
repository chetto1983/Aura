package tools

import (
	"strings"
	"testing"
)

func TestGuessActionFromArgs(t *testing.T) {
	fileHints := []ActionHint{
		{Name: "write", RequiredKeys: []string{"content"}},
		{Name: "search", RequiredKeys: []string{"pattern"}},
		{Name: "patch", RequiredKeys: []string{"old", "new"}},
		{Name: "read", RequiredKeys: []string{"path"}},
	}
	cases := []struct {
		name     string
		supplied map[string]any
		want     string
	}{
		{"content → write", map[string]any{"path": "x", "content": "hi"}, "write"},
		{"path only → read", map[string]any{"path": "x", "max_bytes": 100}, "read"},
		{"pattern → search", map[string]any{"pattern": "foo", "path": "x"}, "search"},
		{"old+new → patch (more specific than read)", map[string]any{"path": "x", "old": "a", "new": "b"}, "patch"},
		{"empty → fallback", map[string]any{}, "list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GuessActionFromArgs(tc.supplied, fileHints, "list")
			if got != tc.want {
				t.Fatalf("GuessActionFromArgs = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestActionRequiredError_FileLike(t *testing.T) {
	hints := []ActionHint{
		{Name: "write", RequiredKeys: []string{"content"}},
		{Name: "read", RequiredKeys: []string{"path"}},
	}
	err := ActionRequiredError("file",
		[]string{"list", "read", "write"},
		map[string]any{"path": "wiki/foo.md", "max_bytes": 4000},
		hints,
		"list",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	// Self-correcting elements MUST be present.
	for _, must := range []string{
		"'action' is required",
		"list | read | write",
		"max_bytes",
		"path",
		`"action":"read"`,
		"Retry with",
	} {
		if !strings.Contains(msg, must) {
			t.Fatalf("error missing %q:\n%s", must, msg)
		}
	}
}

func TestUnknownActionError_SuggestsClosest(t *testing.T) {
	err := UnknownActionError("file", "reed",
		[]string{"list", "read", "write", "search"},
		map[string]any{"path": "x"})
	msg := err.Error()
	if !strings.Contains(msg, "unknown action") {
		t.Fatalf("missing unknown-action prefix: %s", msg)
	}
	if !strings.Contains(msg, "Closest valid match: 'read'") {
		t.Fatalf("expected suggestion 'read' for 'reed': %s", msg)
	}
	if !strings.Contains(msg, `"action":"read"`) {
		t.Fatalf("missing retry JSON: %s", msg)
	}
}

func TestClosestActionMatch_ThresholdRespected(t *testing.T) {
	// "xyz" is too far from any valid action; should return ""
	if got := closestActionMatch("xyz", []string{"list", "read", "write"}); got != "" {
		t.Fatalf("expected empty for unrelated input, got %s", got)
	}
	// "wrte" is 1 edit from "write"
	if got := closestActionMatch("wrte", []string{"list", "read", "write"}); got != "write" {
		t.Fatalf("expected write for wrte, got %s", got)
	}
}

func TestRewriteVerbKeyAsAction(t *testing.T) {
	webHints := []ActionHint{
		{Name: "fetch", RequiredKeys: []string{"url"}},
		{Name: "search", RequiredKeys: []string{"query"}},
	}
	webActions := []string{"search", "fetch"}
	cases := []struct {
		name     string
		supplied map[string]any
		want     map[string]any
		didWrite bool
	}{
		{
			"web verb-key search rewritten to action+query",
			map[string]any{"search": "latest news today"},
			map[string]any{"action": "search", "query": "latest news today"},
			true,
		},
		{
			"web verb-key fetch rewritten to action+url",
			map[string]any{"fetch": "https://example.com"},
			map[string]any{"action": "fetch", "url": "https://example.com"},
			true,
		},
		{
			"already has action — left alone",
			map[string]any{"action": "search", "query": "x"},
			map[string]any{"action": "search", "query": "x"},
			false,
		},
		{
			"no verb key — left alone",
			map[string]any{"query": "x"},
			map[string]any{"query": "x"},
			false,
		},
		{
			"verb key with non-string value — left alone",
			map[string]any{"search": 42},
			map[string]any{"search": 42},
			false,
		},
		{
			"verb key with multi-required action left alone",
			map[string]any{"save": "script body"},
			map[string]any{"save": "script body"},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actions := webActions
			hints := webHints
			if _, ok := tc.supplied["save"]; ok {
				actions = []string{"list", "read", "save"}
				hints = []ActionHint{{Name: "save", RequiredKeys: []string{"name", "description", "code"}}}
			}
			out, ok := RewriteVerbKeyAsAction(tc.supplied, actions, hints)
			if ok != tc.didWrite {
				t.Fatalf("rewrite signal = %v, want %v", ok, tc.didWrite)
			}
			if len(out) != len(tc.want) {
				t.Fatalf("output size %d, want %d: %v", len(out), len(tc.want), out)
			}
			for k, v := range tc.want {
				got, present := out[k]
				if !present {
					t.Fatalf("missing key %q in %v", k, out)
				}
				if got != v {
					t.Fatalf("key %q = %v, want %v", k, got, v)
				}
			}
		})
	}
}

func TestActionRequiredError_RenamesVerbKey(t *testing.T) {
	// Live regression 2026-05-24 turn #158: model called web with the
	// action name used as a param key. Old retry hint preserved the bad
	// key; the new one renames it to the action's required param.
	webHints := []ActionHint{
		{Name: "fetch", RequiredKeys: []string{"url"}},
		{Name: "search", RequiredKeys: []string{"query"}},
	}
	err := ActionRequiredError("web",
		[]string{"search", "fetch"},
		map[string]any{"search": "latest news today"},
		webHints,
		"search",
	)
	msg := err.Error()
	if !strings.Contains(msg, `"action":"search"`) {
		t.Fatalf("retry hint missing action: %s", msg)
	}
	if !strings.Contains(msg, `"query":"latest news today"`) {
		t.Fatalf("retry hint did not rename verb key 'search' → 'query': %s", msg)
	}
	if strings.Contains(msg, `"search":"latest news today"`) {
		t.Fatalf("retry hint preserved broken verb key 'search': %s", msg)
	}
}

func TestActionRequiredError_NoSuppliedArgs(t *testing.T) {
	err := ActionRequiredError("source",
		[]string{"list", "read", "store"},
		map[string]any{},
		[]ActionHint{{Name: "store", RequiredKeys: []string{"content"}}},
		"list",
	)
	if !strings.Contains(err.Error(), "[none]") && !strings.Contains(err.Error(), "supplied [none]") {
		t.Fatalf("expected 'none' for empty args: %s", err)
	}
	if !strings.Contains(err.Error(), `"action":"list"`) {
		t.Fatalf("expected fallback action in retry: %s", err)
	}
}
