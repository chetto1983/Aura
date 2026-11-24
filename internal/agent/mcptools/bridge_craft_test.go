package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// pinnedCraftTools is a Craft server's tool list as its pinned version serves it in the box, captured by
// spikes/craft-mcp/list_tools.py.
func pinnedCraftTools(t *testing.T, server string) []*sdkmcp.Tool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "craft", server+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tools []*sdkmcp.Tool
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("%s: %v", server, err)
	}
	return tools
}

func toolNames(tools []*sdkmcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// The tools that need a Craft app's desktop window fail in the box, so the agent never gets them. Each
// hidden name must still be a tool of the pinned version: one renamed by an upgrade fails here instead of
// reaching the agent.
func TestCraftDesktopOnlyToolsAreLeftOut(t *testing.T) {
	want := map[string]int{"deckcraft": 2, "filmcraft": 8, "gridcraft": 1, "pdfcraft": 0, "photocraft": 6, "wordcraft": 4}
	total := 0
	for _, server := range mcp.CraftServers {
		source := mcp.CraftRecipeSource(server)
		advertised := pinnedCraftTools(t, server)
		hidden := craftHiddenTools[source]
		for _, name := range hidden {
			if !slices.Contains(toolNames(advertised), name) {
				t.Errorf("%s: hidden %q is not a tool of the pinned version", server, name)
			}
		}
		visible := toolNames(visibleTools(source, advertised))
		for _, name := range hidden {
			if slices.Contains(visible, name) {
				t.Errorf("%s: %q is still offered", server, name)
			}
		}
		if len(hidden) != want[server] || len(visible) != len(advertised)-len(hidden) {
			t.Errorf("%s: %d hidden and %d visible of %d, want %d hidden", server, len(hidden), len(visible), len(advertised), want[server])
		}
		total += len(hidden)
	}
	if total != 21 {
		t.Errorf("hidden %d tools, want 21", total)
	}
}

func TestAnotherSourceKeepsEveryTool(t *testing.T) {
	advertised := pinnedCraftTools(t, "wordcraft")
	if got := visibleTools(mcp.BrowserRecipeSource, advertised); len(got) != len(advertised) {
		t.Fatalf("the browser source kept %d of %d tools", len(got), len(advertised))
	}
}

// A mount bridges none of them, so neither the manifest nor tool_search can offer one.
func TestACraftMountBridgesNoDesktopOnlyTool(t *testing.T) {
	resetLoadedSlotBudgetForTest()
	advertised := pinnedCraftTools(t, "filmcraft")
	srv, _ := newInMemoryMounted(t, advertised...)
	policy := bridgePolicy{recipeSource: mcp.CraftRecipeSource("filmcraft"), identityScoped: true}
	bridged, err := bridgeFromAdvertisedWithPolicy("filmcraft", srv, advertised, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range bridged {
		if strings.HasPrefix(tool.Spec().Name, "filmcraft__ui_") {
			t.Errorf("bridged %s", tool.Spec().Name)
		}
	}
	if len(bridged) != 9 {
		t.Fatalf("bridged %d tools, want the 9 that work headless", len(bridged))
	}
}

// Every tool a Craft server offers in the box has a class in Aura's table, and the table names nothing the
// pinned version does not offer.
func TestEveryVisibleCraftToolHasAnAction(t *testing.T) {
	for _, server := range mcp.CraftServers {
		source := mcp.CraftRecipeSource(server)
		actions := craftRecipeActions[source]
		visible := toolNames(visibleTools(source, pinnedCraftTools(t, server)))
		for _, name := range visible {
			if _, ok := actions[name]; !ok {
				t.Errorf("%s: %q has no action", server, name)
			}
		}
		for name := range actions {
			if !slices.Contains(visible, name) {
				t.Errorf("%s: the table names %q, which the box does not offer", server, name)
			}
		}
	}
}

// Their writes only touch files in the identity's box, so none stops the turn for an approval, whatever the
// server declares: FilmCraft, PdfCraft and PhotoCraft mark doc_save, command_run and even jobs_list
// destructive.
func TestCraftWritesAreReversibleWhateverTheServerDeclares(t *testing.T) {
	for _, server := range mcp.CraftServers {
		policy := bridgePolicy{recipeSource: mcp.CraftRecipeSource(server)}
		for _, tool := range visibleTools(policy.recipeSource, pinnedCraftTools(t, server)) {
			mutating, destructive := mcpToolRisk(policy, tool)
			if destructive {
				t.Errorf("%s: %s is destructive", server, tool.Name)
			}
			if read := craftRecipeActions[policy.recipeSource][tool.Name] == mcpActionRead; mutating == read {
				t.Errorf("%s: %s mutating=%v, its action is %v", server, tool.Name, mutating, craftRecipeActions[policy.recipeSource][tool.Name])
			}
		}
	}
}

// A tool an upgrade adds is neither in the table nor trusted by its annotations: it fails closed.
func TestAToolTheCraftTableDoesNotKnowFailsClosed(t *testing.T) {
	readOnly := &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	mutating, destructive := mcpToolRisk(bridgePolicy{recipeSource: mcp.CraftRecipeSource("gridcraft")}, mustTool("brand_new", "", nil, readOnly))
	if !mutating || !destructive {
		t.Fatalf("an unknown Craft tool is mutating=%v destructive=%v, want both", mutating, destructive)
	}
}

// The other recipes still honour a server's destructive hint.
func TestOtherRecipesStillHonourADestructiveHint(t *testing.T) {
	yes := true
	_, destructive := mcpToolRisk(bridgePolicy{recipeSource: calendarRecipeSource}, mustTool("list_accounts", "", nil, &sdkmcp.ToolAnnotations{DestructiveHint: &yes}))
	if !destructive {
		t.Fatal("a calendar tool declared destructive was not")
	}
}

// The policy a recipe mount gets is built from the catalog's own server, not set by hand as the tests
// above set it. The live run found the Craft sources missing from that build: every Craft tool fell back
// to its server's hints, so the gateway asked to approve wordcraft open_document and deckcraft add_slide,
// and the desktop-only tools were bridged.
func TestACraftRecipeMountGetsTheCraftPolicy(t *testing.T) {
	for _, server := range mcp.CraftServers {
		entry, ok := mcpmanager.LookupCatalog(server)
		if !ok {
			t.Fatalf("%s recipe missing", server)
		}
		policy := managedBridgePolicy(entry.Server)
		if policy.recipeSource != mcp.CraftRecipeSource(server) {
			t.Errorf("%s: a recipe mount's policy has source %q, want %q", server, policy.recipeSource, mcp.CraftRecipeSource(server))
		}
		if !policy.identityScoped {
			t.Errorf("%s: a box recipe's policy is not identity scoped", server)
		}
	}
}
