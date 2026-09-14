package tools

import (
	"slices"
	"strings"
	"testing"
)

func searchNames(ts *ToolSearch, q string) []string {
	matches, _, _ := ts.match(q, defaultMaxResults)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.Spec().Name)
	}
	return names
}

// image_generate is deferred, so the ranker over the real built-in summaries is the only
// way an operator's "make me a picture" reaches it. "edit photo" only shares "edit" with
// the summary, as patch's does, and patch's shorter document ranks first (measured
// 2026-09-14): the query still returns image_generate, it does not own rank 1.
func TestToolSearchFindsImageGenerateForPictureQueries(t *testing.T) {
	reg := NewRegistry()
	for _, tool := range builtinTools() {
		reg.Register(tool)
	}
	ts := &ToolSearch{Registry: reg}
	for _, q := range []string{"image", "picture", "generate an image"} {
		if names := searchNames(ts, q); len(names) == 0 || names[0] != "image_generate" {
			t.Errorf("tool_search(%q) = %s, want image_generate first", q, strings.Join(names, ","))
		}
	}
	if names := searchNames(ts, "edit photo"); !slices.Contains(names, "image_generate") {
		t.Errorf("tool_search(%q) = %s, want image_generate among the results", "edit photo", strings.Join(names, ","))
	}
}
