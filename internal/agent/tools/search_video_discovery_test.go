package tools

import (
	"slices"
	"strings"
	"testing"
)

// video_generate is deferred, so the ranker over the real built-in summaries is the only way
// "make a video of this" or "animate this" reaches it. "animate an image" shares "image" with
// image_generate's summary, which names it twice, so image_generate ranks first there (measured
// 2026-09-15): the query still returns video_generate, it does not own rank 1.
func TestToolSearchFindsVideoGenerateForVideoQueries(t *testing.T) {
	reg := NewRegistry()
	for _, tool := range builtinTools() {
		reg.Register(tool)
	}
	ts := &ToolSearch{Registry: reg}
	for _, q := range []string{"video", "make a video", "animate", "turn a photo into a video", "collect a completed video job"} {
		if names := searchNames(ts, q); len(names) == 0 || names[0] != "video_generate" {
			t.Errorf("tool_search(%q) = %s, want video_generate first", q, strings.Join(names, ","))
		}
	}
	if names := searchNames(ts, "animate an image"); !slices.Contains(names, "video_generate") {
		t.Errorf("tool_search(%q) = %s, want video_generate among the results", "animate an image", strings.Join(names, ","))
	}
	for _, q := range []string{"image", "picture", "generate an image"} {
		if names := searchNames(ts, q); len(names) == 0 || names[0] != "image_generate" {
			t.Errorf("tool_search(%q) = %s, want image_generate to stay first", q, strings.Join(names, ","))
		}
	}
}
