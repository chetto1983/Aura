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
