package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeSkillLoader is a deterministic in-memory skillLoader for the tool tests.
type fakeSkillLoader struct {
	skills   []SkillMeta
	bodies   map[string]string
	snippets map[string]fakeSnippet // name -> snippet by-path resolution (D-04)
}

type fakeSnippet struct {
	instructions string
	hostPath     string
	interpreter  string
}

func (f *fakeSkillLoader) List() []SkillMeta { return f.skills }

func (f *fakeSkillLoader) Body(name string) (string, bool) {
	b, ok := f.bodies[name]
	return b, ok
}

func (f *fakeSkillLoader) ManifestDescription() string {
	var b strings.Builder
	for _, s := range f.skills {
		b.WriteString("- " + s.Name + ": " + s.Description + "\n")
	}
	return b.String()
}

func (f *fakeSkillLoader) Snippet(name string) (instructions, hostPath, interpreter string, ok bool) {
	s, ok := f.snippets[name]
	if !ok {
		return "", "", "", false
	}
	return s.instructions, s.hostPath, s.interpreter, true
}

func newFakeLoader() *fakeSkillLoader {
	return &fakeSkillLoader{
		skills: []SkillMeta{
			{Name: "alpha", Description: "Alpha excel spreadsheet skill."},
			{Name: "bravo", Description: "Bravo pdf document skill."},
		},
		bodies: map[string]string{
			"alpha": "Do the alpha thing.",
			"bravo": "Do the bravo thing.",
		},
	}
}

func TestSkillSpecSchemaDiscipline(t *testing.T) {
	t.Parallel()
	tool := &SkillTool{Loader: newFakeLoader()}
	spec := tool.Spec()

	if spec.Name != "skill" {
		t.Fatalf("name = %q, want skill", spec.Name)
	}
	if spec.Deferred {
		t.Fatalf("skill tool must be non-deferred (D-05)")
	}

	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("schema unmarshal: %v", err)
	}
	// Root required must be exactly ["action"].
	req, _ := schema["required"].([]any)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("root required = %v, want [action]", req)
	}
	// No root oneOf/anyOf/enum (OpenAI-wire-safe, D-10).
	for _, k := range []string{"oneOf", "anyOf", "enum"} {
		if _, ok := schema[k]; ok {
			t.Fatalf("root schema must NOT carry %q (OpenAI-wire-safe)", k)
		}
	}
	// The action property carries a string enum including the read + reserved actions.
	// "install"/"catalog" were removed (amendment #51 / D-40): the test asserted the
	// superseded enum — discovery+install is now the find-skills always-on skill.
	props, _ := schema["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]any)
	want := map[string]bool{"list": false, "info": false, "use": false, "create": false, "restore": false}
	for _, v := range enum {
		if s, ok := v.(string); ok {
			if _, tracked := want[s]; tracked {
				want[s] = true
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("action enum missing %q", name)
		}
	}
}

func TestSkillRegistryValidates(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Register(&SkillTool{Loader: newFakeLoader()})
	if err := reg.Validate(); err != nil {
		t.Fatalf("reg.Validate() with skill tool = %v, want nil (Pitfall 6)", err)
	}
}

func TestSkillDispatchErrors(t *testing.T) {
	t.Parallel()
	tool := &SkillTool{Loader: newFakeLoader()}
	ctx := context.Background()

	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":""}`)); err == nil ||
		!strings.Contains(err.Error(), "action is required") {
		t.Fatalf("empty action err = %v, want 'action is required'", err)
	}
	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":"bogus"}`)); err == nil ||
		!strings.Contains(err.Error(), "valid actions are") {
		t.Fatalf("unknown action err = %v, want 'valid actions are ...'", err)
	}
	// A reserved-but-unwired action returns the "not yet available" error. (catalog +
	// install were removed in 11-09 / amendment #51; restore/archive remain reserved.)
	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":"restore"}`)); err == nil ||
		!strings.Contains(err.Error(), "not yet available") {
		t.Fatalf("reserved action err = %v, want 'not yet available'", err)
	}
}

func TestSkillReadActions(t *testing.T) {
	t.Parallel()
	tool := &SkillTool{Loader: newFakeLoader()}
	ctx := withTestToolCallCtx(context.Background())

	// list (no query) returns the manifest.
	res, err := tool.Execute(ctx, json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(res.Preview, "alpha") || !strings.Contains(res.Preview, "bravo") {
		t.Fatalf("list manifest missing skills: %q", res.Preview)
	}

	// list with a query ranks by BM25 — "excel" should surface alpha.
	res, err = tool.Execute(ctx, json.RawMessage(`{"action":"list","query":"excel spreadsheet"}`))
	if err != nil {
		t.Fatalf("list query: %v", err)
	}
	if !strings.Contains(res.Preview, "alpha") {
		t.Fatalf("ranked list should surface alpha for 'excel': %q", res.Preview)
	}

	// info returns the plain body — no authority frame.
	res, err = tool.Execute(ctx, json.RawMessage(`{"action":"info","name":"alpha"}`))
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if res.Preview != "Do the alpha thing." {
		t.Fatalf("info body = %q, want plain body", res.Preview)
	}

	// use wraps the body in the authority frame.
	res, err = tool.Execute(ctx, json.RawMessage(`{"action":"use","name":"bravo"}`))
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !strings.HasPrefix(res.Preview, useAuthorityFrame) || !strings.Contains(res.Preview, "Do the bravo thing.") {
		t.Fatalf("use should wrap body in the authority frame: %q", res.Preview)
	}

	// use/info on an unknown skill errors.
	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":"use","name":"ghost"}`)); err == nil ||
		!strings.Contains(err.Error(), "unknown skill") {
		t.Fatalf("unknown skill err = %v", err)
	}
	// info with no name errors.
	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":"info"}`)); err == nil ||
		!strings.Contains(err.Error(), "name is required") {
		t.Fatalf("missing name err = %v", err)
	}
}

func TestSkillDescriptionByteStable(t *testing.T) {
	t.Parallel()
	tool := &SkillTool{Loader: newFakeLoader()}
	first := tool.Spec().Description
	second := tool.Spec().Description
	if first != second {
		t.Fatalf("skill Description not byte-stable:\n%q\nvs\n%q", first, second)
	}
	if !strings.Contains(first, "alpha") {
		t.Fatalf("Description should embed the manifest: %q", first)
	}
}

func TestSkillNilLoaderManifest(t *testing.T) {
	t.Parallel()
	tool := &SkillTool{} // no loader wired (pool-free manifest path)
	desc := tool.Spec().Description
	if !strings.Contains(desc, "none loaded") {
		t.Fatalf("nil-loader Description should note none loaded: %q", desc)
	}
	if tool.Spec().Deferred {
		t.Fatalf("nil-loader skill tool must still be non-deferred")
	}
}

// withTestToolCallCtx injects a tool-call context so NewResult can build previews
// without spilling (large cap, in-tmp run dir).
func withTestToolCallCtx(ctx context.Context) context.Context {
	return WithToolCallContext(ctx, "test-session", "test-call", "", 1<<20)
}
