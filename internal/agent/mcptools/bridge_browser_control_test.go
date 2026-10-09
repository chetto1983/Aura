package mcptools

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/browsercontrol"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

const browserTestIdentity = "identity-a"

// Every tool of the browser recipe, held or not, stale or not, with and without an element
// reference: a write is refused only while held, a reference only while stale, and a read
// is never refused for being held.
func TestBrowserRefusalTable(t *testing.T) {
	t.Parallel()
	for tool, class := range browserRecipeActions {
		for _, held := range []bool{false, true} {
			for _, stale := range []bool{false, true} {
				for _, selector := range []string{"", "@e3", "#submit"} {
					var r browsercontrol.Registry
					if stale {
						h := new(int)
						r.Hold(browserTestIdentity, "portal", h)
						r.Release(browserTestIdentity, "portal", h)
					}
					if held {
						r.Hold(browserTestIdentity, "portal", new(int))
					}
					args := map[string]any{"session": "portal"}
					if selector != "" {
						args["selector"] = selector
					}
					err := browserGuard(&r, browserTestIdentity, tool, args)
					wantHeld := held && class != MCPActionRead
					wantStale := !wantHeld && stale && selector == "@e3"
					switch {
					case wantHeld:
						if err == nil || !strings.Contains(err.Error(), "held by the operator") {
							t.Errorf("%s held=%v stale=%v sel=%q: err=%v, want held", tool, held, stale, selector, err)
						}
					case wantStale:
						if err == nil || !strings.Contains(err.Error(), "take a snapshot first") {
							t.Errorf("%s held=%v stale=%v sel=%q: err=%v, want stale", tool, held, stale, selector, err)
						}
					default:
						if err != nil {
							t.Errorf("%s held=%v stale=%v sel=%q: refused %v", tool, held, stale, selector, err)
						}
					}
				}
			}
		}
	}
}

func TestBrowserGuardWithoutARegistryRefusesNothing(t *testing.T) {
	t.Parallel()
	if err := browserGuard(nil, browserTestIdentity, "agent_browser_click", map[string]any{"selector": "@e1"}); err != nil {
		t.Fatalf("no registry refused: %v", err)
	}
	refreshBrowserReferences(nil, browserTestIdentity, nil, "[ref=e1]")
}

func TestBrowserGuardIsScopedToTheSessionAndIdentity(t *testing.T) {
	t.Parallel()
	var r browsercontrol.Registry
	r.Hold(browserTestIdentity, "portal", new(int))
	if err := browserGuard(&r, browserTestIdentity, "agent_browser_click", map[string]any{"session": "bank"}); err != nil {
		t.Fatalf("another session refused: %v", err)
	}
	if err := browserGuard(&r, "identity-b", "agent_browser_click", map[string]any{"session": "portal"}); err != nil {
		t.Fatalf("another identity refused: %v", err)
	}
}

// End to end through a real mount: a held session refuses the write before the box sees it;
// after release an element action is refused until a result carrying references comes back.
func TestBrowserExecuteRefreshesOnReferences(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	var r browsercontrol.Registry
	call := echoCaller(t, recipe.Server, &r)

	viewer := new(int)
	r.Hold(browserTestIdentity, "portal", viewer)
	if _, err := call(`{"session":"portal","script":"1"}`); err == nil || !strings.Contains(err.Error(), `browser session "portal" is held`) {
		t.Fatalf("a write while held = %v, want refused", err)
	}
	r.Release(browserTestIdentity, "portal", viewer)

	if _, err := call(`{"session":"portal","selector":"@e3"}`); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a reference after release = %v, want refused as stale", err)
	}
	if _, err := call(`{"session":"portal","script":"textbox \"Email\" [ref=e2]"}`); err != nil {
		t.Fatalf("a call without a reference = %v, want it through", err)
	}
	if r.Stale(browserTestIdentity, "portal") {
		t.Fatal("a result carrying [ref=eN] must refresh the session")
	}
	if _, err := call(`{"session":"portal","selector":"@e3"}`); err != nil {
		t.Fatalf("a reference after the refresh = %v, want it through", err)
	}
}
