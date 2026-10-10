package mcptools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/browsercontrol"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

func TestBrowserSessionsCloseTheLeastRecentlyUsedPastThree(t *testing.T) {
	t.Parallel()
	var s browserSessions
	for _, session := range []string{"weather", "finance", "news"} {
		if evict, err := s.admit(nil, "identity-a", session); evict != "" || err != nil {
			t.Fatalf("admit %s = %q, %v: three fit", session, evict, err)
		}
	}
	if evict, err := s.admit(nil, "identity-a", "weather"); evict != "" || err != nil {
		t.Fatalf("weather again = %q, %v: an open session takes no more room", evict, err)
	}
	if evict, err := s.admit(nil, "identity-a", "movies"); evict != "finance" || err != nil {
		t.Fatalf("a fourth session closes %q, %v: want finance, the least recently used", evict, err)
	}
	if evict, err := s.admit(nil, "identity-b", "weather"); evict != "" || err != nil {
		t.Fatalf("another identity's first session closes %q, %v: each box counts its own", evict, err)
	}
}

func TestBrowserSessionsNeverCloseWhatTheOperatorHolds(t *testing.T) {
	t.Parallel()
	var r browsercontrol.Registry
	var s browserSessions
	for _, session := range []string{"bank", "portal", "news"} {
		if _, err := s.admit(&r, "identity-a", session); err != nil {
			t.Fatal(err)
		}
	}
	r.Hold("identity-a", "bank", new(int))
	if evict, err := s.admit(&r, "identity-a", "movies"); evict != "portal" || err != nil {
		t.Fatalf("bank held, a fourth closes %q, %v: want portal", evict, err)
	}
	r.Hold("identity-a", "news", new(int))
	r.Hold("identity-a", "movies", new(int))
	if evict, err := s.admit(&r, "identity-a", "shop"); err == nil || !strings.Contains(err.Error(), "the operator holds each") {
		t.Fatalf("all three held, a fourth = %q, %v: want it refused", evict, err)
	}
	if evict, err := s.admit(&r, "identity-a", "bank"); evict != "" || err != nil {
		t.Fatalf("a held session's own call = %q, %v: it is already open", evict, err)
	}
}

func TestBrowserSessionsForgetWhatACloseEnded(t *testing.T) {
	t.Parallel()
	var s browserSessions
	s.forget("identity-a", map[string]any{"session": "never-opened"})
	for _, session := range []string{"a", "b", "c"} {
		if _, err := s.admit(nil, "identity-a", session); err != nil {
			t.Fatal(err)
		}
	}
	s.forget("identity-a", map[string]any{"session": "b"})
	if evict, _ := s.admit(nil, "identity-a", "d"); evict != "" {
		t.Fatalf("b closed, a third open session closed %q", evict)
	}
	s.forget("identity-a", map[string]any{"all": true})
	for _, session := range []string{"e", "f", "g"} {
		if evict, _ := s.admit(nil, "identity-a", session); evict != "" {
			t.Fatalf("after closing all, %s closed %q", session, evict)
		}
	}
}

// sessionsCalled is the session of every call the box received for tool, in order.
func (b *identityBoxes) sessionsCalled(t *testing.T, tool string) []string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var sessions []string
	for _, call := range b.calls {
		name, raw, _ := strings.Cut(call, " ")
		if name != tool {
			continue
		}
		var args struct{ Session string }
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			t.Fatalf("the box received %q: %v", raw, err)
		}
		sessions = append(sessions, args.Session)
	}
	return sessions
}

func TestBrowserOpenCarriesTheRuleToCloseItsSession(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	reg := mountBox(t, recipe.Server, nil, &identityBoxes{echoTools: []string{"agent_browser_open"}})
	open, ok := reg.Get("browser__agent_browser_open")
	if !ok {
		t.Fatal("open was not mounted")
	}
	if got := open.Spec().Description; !strings.Contains(got, "call browser__agent_browser_close with it when the site is done") {
		t.Fatalf("open's description %q does not tell the model to close its session", got)
	}
	if whoami, _ := reg.Get("browser__whoami"); strings.Contains(whoami.Spec().Description, "agent_browser_close") {
		t.Fatalf("whoami's description %q carries open's rule", whoami.Spec().Description)
	}
	res, err := open.Execute(boxCallCtx(t), json.RawMessage(`{"session":"weather","url":"https://example.com/"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n\nSession \"weather\" is open. Close it with browser__agent_browser_close when the site is done."; !strings.HasSuffix(res.Preview, want) {
		t.Fatalf("open answered %q, want it to end with %q", res.Preview, want)
	}
}

func TestOtherBoxServersOpenWithoutTheRule(t *testing.T) {
	boxes := &identityBoxes{echoTools: []string{"agent_browser_open"}}
	open, ok := mountBox(t, boxServer, nil, boxes).Get("browser__agent_browser_open")
	if !ok {
		t.Fatal("open was not mounted")
	}
	if strings.Contains(open.Spec().Description, "agent_browser_close") {
		t.Fatalf("a server outside the browser recipe got open's rule: %q", open.Spec().Description)
	}
	for _, session := range []string{"a", "b", "c", "d"} {
		res, err := open.Execute(boxCallCtx(t), json.RawMessage(`{"session":"`+session+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"session":"` + session + `"}`; res.Preview != want {
			t.Fatalf("a server outside the browser recipe answered %q, want %q", res.Preview, want)
		}
	}
}

func TestBrowserOpenKeepsItsRuleAcrossAReconnect(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	policy := managedBridgePolicy(recipe.Server)
	tool := &sdkmcp.Tool{Name: "agent_browser_open", Description: "Navigate to a URL.", InputSchema: map[string]any{"type": "object"}}
	bt := &bridgedTool{name: tool.Name, policy: policy}
	bt.storeSpec(specFromToolDefWithPolicy("browser", tool, policy))
	bt.refreshSpec(&sdkmcp.Tool{Name: tool.Name, Description: "Open a URL.", InputSchema: map[string]any{"type": "object"}})
	got := bt.Spec().Description
	if !strings.HasPrefix(got, "Open a URL.") || strings.Count(got, "browser__agent_browser_close") != 1 {
		t.Fatalf("after a reconnect open's description is %q, want the server's new text and the rule once", got)
	}
}

func TestABrowserCallPastThreeClosesTheLeastRecentlyUsedFirst(t *testing.T) {
	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	boxes := &identityBoxes{echoTools: []string{"agent_browser_open", "agent_browser_snapshot", "agent_browser_close"}}
	reg := mountBox(t, recipe.Server, nil, boxes)
	call := func(tool, session string) string {
		t.Helper()
		bridged, ok := reg.Get("browser__" + tool)
		if !ok {
			t.Fatalf("%s was not mounted", tool)
		}
		res, err := bridged.Execute(boxCallCtx(t), json.RawMessage(`{"session":"`+session+`"}`))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, session, err)
		}
		return res.Preview
	}
	call("agent_browser_open", "weather")
	call("agent_browser_open", "finance")
	call("agent_browser_open", "news")
	call("agent_browser_snapshot", "weather")
	if got := call("agent_browser_open", "movies"); !strings.Contains(got, `Closed browser session "finance" to make room`) {
		t.Fatalf("the fourth open answered %q, want it to say finance was closed", got)
	}
	call("agent_browser_close", "news")
	if got := call("agent_browser_snapshot", "shop"); strings.Contains(got, "Closed browser session") {
		t.Fatalf("after news was closed, a third open session answered %q", got)
	}
	if got := boxes.sessionsCalled(t, "agent_browser_close"); !slices.Equal(got, []string{"finance", "news"}) {
		t.Fatalf("the box was asked to close %q, want finance by the bridge, then news by the model", got)
	}
}
