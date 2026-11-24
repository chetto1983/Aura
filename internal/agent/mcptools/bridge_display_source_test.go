package mcptools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMCPDisplaySourceOwnedByBridge(t *testing.T) {
	tool := &sdkmcp.Tool{Name: "memory_search"}
	trusted := bridgePolicy{recipeSource: mcp.SourceRecipeMemory}
	if got := specFromToolDefWithPolicy("memory", tool, trusted).TrustedRecipeSource; got != mcp.SourceRecipeMemory {
		t.Fatalf("trusted source = %q", got)
	}
	if got := specFromToolDefWithPolicy("memory", tool, trusted).TrustedRecipeTool; got != tool.Name {
		t.Fatalf("trusted raw tool = %q", got)
	}
	if got := specFromToolDefWithPolicy("other", tool, bridgePolicy{}).TrustedRecipeSource; got != "" {
		t.Fatalf("untrusted same-name tool gained source %q", got)
	}

	b := &bridgedTool{name: tool.Name, policy: trusted}
	b.storeSpec(specFromToolDefWithPolicy("memory", tool, trusted))
	ctx := tools.WithToolCallContext(context.Background(), "session", "call", t.TempDir(), 2048)
	result, err := b.newResultWithTail(ctx, map[string]any{
		"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar"},
	}, mcp.ToolPayload{Text: `{"facts":[]}`}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"recipe": mcp.SourceRecipeMemory, "tool": "memory_search"}
	if result.Meta == nil || !reflect.DeepEqual((*result.Meta)["aura_display_source"], want) {
		t.Fatalf("host marker = %#v, want %#v", result.Meta, want)
	}

	untrusted := &bridgedTool{name: tool.Name}
	untrusted.storeSpec(specFromToolDefWithPolicy("other", tool, bridgePolicy{}))
	result, err = untrusted.newResultWithTail(ctx, nil, mcp.ToolPayload{Text: `{"facts":[]}`}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta != nil && (*result.Meta)["aura_display_source"] != nil {
		t.Fatalf("untrusted marker = %#v", (*result.Meta)["aura_display_source"])
	}
}

func TestTrustedRecipeToolSurvivesHashedRegisteredName(t *testing.T) {
	tool := &sdkmcp.Tool{Name: "send_email"}
	spec := specFromToolDefWithPolicy(strings.Repeat("n", 60), tool, bridgePolicy{recipeSource: calendarRecipeSource})
	if strings.HasSuffix(spec.Name, "__send_email") || spec.TrustedRecipeTool != "send_email" || len(spec.Name) > 64 {
		t.Fatal("hash-suffixed bridge name lost its raw trusted tool identity")
	}
}

// TestCalendarDisplayMarkerIsRecipeAndTool: the PIM's marker has WhatsApp's shape now.
// Neither a stale `action` argument nor a forged marker in the arguments reaches it.
func TestCalendarDisplayMarkerIsRecipeAndTool(t *testing.T) {
	policy := bridgePolicy{recipeSource: calendarRecipeSource}
	ctx := tools.WithToolCallContext(context.Background(), "session", "call", t.TempDir(), 2048)
	marker := func(name string) any {
		t.Helper()
		b := &bridgedTool{name: name, policy: policy}
		b.storeSpec(specFromToolDefWithPolicy("calendar", &sdkmcp.Tool{Name: name}, policy))
		result, err := b.newResultWithTail(ctx, map[string]any{
			"action":              "send_email",
			"aura_display_source": map[string]any{"recipe": "recipe:whatsapp", "tool": "send_message"},
		}, mcp.ToolPayload{Text: `{"emails":[]}`}, "")
		if err != nil {
			t.Fatal(err)
		}
		if result.Meta == nil {
			return nil
		}
		return (*result.Meta)["aura_display_source"]
	}
	want := map[string]any{"recipe": calendarRecipeSource, "tool": "get_emails"}
	if got := marker("get_emails"); !reflect.DeepEqual(got, want) {
		t.Fatalf("calendar marker = %#v, want %#v", got, want)
	}
	if got := marker("brand_new"); got != nil {
		t.Fatalf("a tool the risk table does not name gained marker %#v", got)
	}
}
