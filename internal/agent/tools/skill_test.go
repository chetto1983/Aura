package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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
	sandboxPath  string
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

func (f *fakeSkillLoader) Snippet(name string) (instructions, hostPath, sandboxPath, interpreter string, ok bool) {
	s, ok := f.snippets[name]
	if !ok {
		return "", "", "", "", false
	}
	return s.instructions, s.hostPath, s.sandboxPath, s.interpreter, true
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

func TestSkillToolSchemaStatesActualAutoActivationPolicy(t *testing.T) {
	t.Parallel()
	spec := (&SkillTool{}).Spec()
	schema := string(spec.Parameters)
	for _, want := range []string{
		"always:false",
		"activate immediately",
		"always:true",
		"delete require approval",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("skill schema does not state %q in the live policy: %s", want, schema)
		}
	}
}

// TestSkillSchemaIsHonestNotDishonest is the AG-011/AG-044 regression: the live
// skill schema must NOT carry the old dishonest "you cannot approve your own
// changes" claim (the dead skillParamsSchema const advertised approval-gating that
// the code never enforces — single-operator in-box auto-activation is the live
// path). Exactly one schema backs the Spec, and it states the real trust boundary.
func TestSkillSchemaIsHonestNotDishonest(t *testing.T) {
	t.Parallel()
	schema := string((&SkillTool{}).Spec().Parameters)
	for _, dishonest := range []string{
		"you cannot approve your own changes",
		"require explicit human approval before they take effect",
	} {
		if strings.Contains(schema, dishonest) {
			t.Fatalf("skill schema still carries the dishonest gated claim %q; the live path is in-box auto-activation: %s", dishonest, schema)
		}
	}
	// The honest schema names the actual single-operator activation boundary.
	if !strings.Contains(schema, "activate immediately in this container") {
		t.Fatalf("skill schema must honestly state in-container auto-activation: %s", schema)
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
	// restore/archive are WIRED now (18-03) — with no writer in this loader-only tool
	// they dispatch to the real handler and return a clear "no writer" error (never the
	// old "not yet available" placeholder, never a panic).
	if _, err := tool.Execute(ctx, json.RawMessage(`{"action":"restore","name":"x"}`)); err == nil ||
		!strings.Contains(err.Error(), "no writer") {
		t.Fatalf("restore (wired, no writer) err = %v, want 'no writer'", err)
	}
}

func TestSkillToolConcurrentExecuteInitializesRouterOnce(t *testing.T) {
	tool := &SkillTool{Loader: newFakeLoader()}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = tool.Execute(context.Background(), json.RawMessage(`{"action":"bogus"}`))
		}()
	}
	wg.Wait()
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
