package mcptools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

func TestWithBrowserProfileGivesEachSessionItsOwnProfile(t *testing.T) {
	profile := func(session string, extra ...any) map[string]any {
		return map[string]any{"session": session, "extraArgs": append(extra, "--profile", "~/profiles/"+session)}
	}
	cases := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{"no args drive the default session", nil, profile("default")},
		{"a null session is no session", map[string]any{"session": nil}, profile("default")},
		{"a named session keeps its other args", map[string]any{"session": "bank", "url": "https://bank.example/"},
			map[string]any{"session": "bank", "url": "https://bank.example/", "extraArgs": []any{"--profile", "~/profiles/bank"}}},
		{"the model's own flags come first", map[string]any{"session": "bank", "extraArgs": []any{"--headed"}}, profile("bank", "--headed")},
		{"48 characters is the longest name", map[string]any{"session": strings.Repeat("a", 48)}, profile(strings.Repeat("a", 48))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := withBrowserProfile(c.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestWithBrowserProfileLeavesTheCallersArgsAlone(t *testing.T) {
	args := map[string]any{"session": "bank", "extraArgs": []any{"--headed"}}
	if _, err := withBrowserProfile(args); err != nil {
		t.Fatal(err)
	}
	if want := (map[string]any{"session": "bank", "extraArgs": []any{"--headed"}}); !reflect.DeepEqual(args, want) {
		t.Fatalf("args became %v", args)
	}
}

func TestWithBrowserProfileRefusesWhatWouldMoveTheProfile(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"a path in the name":        {"session": "../etc"},
		"an empty name":             {"session": ""},
		"a 49-character name":       {"session": strings.Repeat("a", 49)},
		"a name that is no string":  {"session": 7},
		"extraArgs that are a line": {"extraArgs": "--headed"},
		"extraArgs with a number":   {"extraArgs": []any{1}},
		"the model's own profile":   {"extraArgs": []any{"--profile", "/tmp/x"}},
		"the model's profile, =":    {"extraArgs": []any{"--profile=/tmp/x"}},
	} {
		if got, err := withBrowserProfile(args); err == nil {
			t.Errorf("%s: accepted as %v", name, got)
		}
	}
}

// mountBox mounts server as "browser" on boxes.
func mountBox(t *testing.T, server mcp.ManagedServer, browser BrowserControl, boxes *identityBoxes) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	handshakeCtx := identityctx.WithIdentityID(t.Context(), "identity-a")
	closer, _, _, err := MountManagedServerWithOptions(t.Context(), handshakeCtx, reg, "browser", server, MountOptions{Box: boxes, Browser: browser})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() {
		_ = closer()
		for _, p := range boxes.procs {
			<-p.done
		}
	})
	return reg
}

// boxCallCtx is a tool call of identity-a, the identity mountBox mounts for.
func boxCallCtx(t *testing.T) context.Context {
	return tools.WithToolCallContext(identityctx.WithIdentityID(t.Context(), "identity-a"), "sess", "tc", t.TempDir(), 2048)
}

// echoCaller mounts server on a box whose agent_browser_eval answers with the arguments it
// received, and returns a caller for it: what the box saw, or the bridge's error.
func echoCaller(t *testing.T, server mcp.ManagedServer, browser BrowserControl) func(raw string) (map[string]any, error) {
	t.Helper()
	boxes := &identityBoxes{echoTools: []string{"agent_browser_eval"}}
	tool, ok := mountBox(t, server, browser, boxes).Get("browser__agent_browser_eval")
	if !ok {
		t.Fatal("the echo tool was not mounted")
	}
	return func(raw string) (map[string]any, error) {
		res, err := tool.Execute(boxCallCtx(t), json.RawMessage(raw))
		if err != nil {
			return nil, err
		}
		var seen map[string]any
		if err := json.Unmarshal([]byte(res.Preview), &seen); err != nil {
			t.Fatalf("the box saw %q: %v", res.Preview, err)
		}
		return seen, nil
	}
}

func TestBrowserRecipeCallsReachTheBoxWithTheirSessionProfile(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	call := echoCaller(t, recipe.Server, nil)
	seen, err := call(`{"script":"document.title"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"script": "document.title", "session": "default", "extraArgs": []any{"--profile", "~/profiles/default"}}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("the box saw %v, want %v", seen, want)
	}
	if _, err := call(`{"session":"../etc"}`); err == nil || !strings.Contains(err.Error(), "browser session") {
		t.Fatalf("a path as session = %v, want the bridge to refuse it", err)
	}
}

func TestOtherBoxServersGetNoBrowserProfile(t *testing.T) {
	call := echoCaller(t, boxServer, nil)
	seen, err := call(`{"session":"../etc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := (map[string]any{"session": "../etc"}); !reflect.DeepEqual(seen, want) {
		t.Fatalf("a server outside the browser recipe saw %v, want its args untouched", seen)
	}
}
