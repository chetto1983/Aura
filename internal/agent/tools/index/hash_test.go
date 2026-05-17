package toolindex

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// Stable shape so every test references the same "tool":
func sampleDef() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "web_fetch",
		Description: "Fetch a web page by URL and return its body.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":   map[string]any{"type": "string", "description": "URL to fetch"},
				"timeout_sec": map[string]any{"type": "integer", "default": 30},
			},
			"required": []any{"url"},
		},
	}
}

func TestContentHash_Deterministic(t *testing.T) {
	def := sampleDef()
	h1, err := ContentHash(def, "web fetch: Fetch a page", "embeddinggemma:256d")
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	for i := 0; i < 200; i++ {
		h, err := ContentHash(def, "web fetch: Fetch a page", "embeddinggemma:256d")
		if err != nil {
			t.Fatalf("hash %d: %v", i, err)
		}
		if h != h1 {
			t.Fatalf("non-deterministic: iter %d got %s, want %s", i, h, h1)
		}
	}
}

func TestContentHash_FieldSensitivity(t *testing.T) {
	base := sampleDef()
	baseHash, _ := ContentHash(base, "txt", "m1")
	cases := []struct {
		name   string
		mutate func(*llm.ToolDefinition) (text, model string)
	}{
		{"name change", func(d *llm.ToolDefinition) (string, string) { d.Name = "web_fetch_v2"; return "txt", "m1" }},
		{"description change", func(d *llm.ToolDefinition) (string, string) { d.Description += "."; return "txt", "m1" }},
		{"schema field add", func(d *llm.ToolDefinition) (string, string) {
			props := d.Parameters["properties"].(map[string]any)
			props["new_field"] = map[string]any{"type": "string"}
			return "txt", "m1"
		}},
		{"embed_text change", func(d *llm.ToolDefinition) (string, string) { return "different text", "m1" }},
		{"embed_model change", func(d *llm.ToolDefinition) (string, string) { return "txt", "m2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := sampleDef()
			text, model := tc.mutate(&def)
			h, err := ContentHash(def, text, model)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if h == baseHash {
				t.Fatalf("hash unchanged after %s — change is invisible to reconcile diff", tc.name)
			}
		})
	}
}

func TestContentHash_MapKeyOrderInsensitive(t *testing.T) {
	// Two builds of an equivalent schema in different orders MUST hash
	// identically. This is the entire point of canonical JSON.
	def1 := llm.ToolDefinition{
		Name:        "t",
		Description: "d",
		Parameters: map[string]any{
			"alpha": "1",
			"beta":  "2",
			"gamma": "3",
			"nested": map[string]any{
				"zeta":  "z",
				"eta":   "e",
				"theta": "h",
			},
		},
	}
	// Same logical content, different builder order — Go's map literal
	// iteration is randomized so json.Marshal naive would diverge across
	// runs even for these two values. canonicalJSON must collapse them.
	def2 := llm.ToolDefinition{
		Name:        "t",
		Description: "d",
		Parameters: map[string]any{
			"gamma": "3",
			"beta":  "2",
			"alpha": "1",
			"nested": map[string]any{
				"theta": "h",
				"eta":   "e",
				"zeta":  "z",
			},
		},
	}
	h1, err := ContentHash(def1, "", "")
	if err != nil {
		t.Fatalf("h1: %v", err)
	}
	h2, err := ContentHash(def2, "", "")
	if err != nil {
		t.Fatalf("h2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("key-order-sensitive hash:\n  h1=%s\n  h2=%s", h1, h2)
	}
}

func TestContentHash_FormatIsLowercaseHex(t *testing.T) {
	h, err := ContentHash(sampleDef(), "txt", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 {
		t.Fatalf("expected 64-char hex, got %d", len(h))
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("non-lowercase-hex char %q in %s", c, h)
		}
	}
}

func TestCanonicalJSON_NestedSlice(t *testing.T) {
	// Slices preserve order (semantic difference), and nested map keys
	// inside slices are sorted.
	v := []any{
		map[string]any{"b": 2, "a": 1},
		map[string]any{"d": 4, "c": 3},
	}
	got, err := canonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"a":1,"b":2},{"c":3,"d":4}]`
	if string(got) != want {
		t.Fatalf("canonical:\n  got:  %s\n  want: %s", got, want)
	}
	if strings.Contains(string(got), "\n") {
		t.Fatalf("canonical must be single-line, got:\n%s", got)
	}
}

func TestCanonicalJSON_StringMapPromoted(t *testing.T) {
	// map[string]string should be sorted same as map[string]any.
	got, err := canonicalJSON(map[string]string{"x": "1", "a": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":"2","x":"1"}` {
		t.Fatalf("got %s", got)
	}
}
