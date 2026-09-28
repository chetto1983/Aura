package mcptools

import (
	"context"
	"reflect"
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
	result, err := b.newResult(ctx, map[string]any{
		"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar"},
	}, mcp.ToolPayload{Text: `{"facts":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"recipe": mcp.SourceRecipeMemory, "tool": "memory_search"}
	if result.Meta == nil || !reflect.DeepEqual((*result.Meta)["aura_display_source"], want) {
		t.Fatalf("host marker = %#v, want %#v", result.Meta, want)
	}

	untrusted := &bridgedTool{name: tool.Name}
	untrusted.storeSpec(specFromToolDefWithPolicy("other", tool, bridgePolicy{}))
	result, err = untrusted.newResult(ctx, nil, mcp.ToolPayload{Text: `{"facts":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta != nil && (*result.Meta)["aura_display_source"] != nil {
		t.Fatalf("untrusted marker = %#v", (*result.Meta)["aura_display_source"])
	}
}

func TestMCPDisplaySourceCalendarAction(t *testing.T) {
	policy := bridgePolicy{recipeSource: calendarRecipeSource}
	b := &bridgedTool{name: "calendar", policy: policy}
	b.storeSpec(specFromToolDefWithPolicy("calendar", &sdkmcp.Tool{Name: "calendar"}, policy))
	ctx := tools.WithToolCallContext(context.Background(), "session", "call", t.TempDir(), 2048)
	result, err := b.newResult(ctx, map[string]any{
		"action":              "get_emails",
		"aura_display_source": map[string]any{"recipe": "recipe:whatsapp", "action": "send_message"},
	}, mcp.ToolPayload{Text: `{"emails":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"recipe": calendarRecipeSource, "tool": "calendar", "action": "get_emails"}
	if result.Meta == nil || !reflect.DeepEqual((*result.Meta)["aura_display_source"], want) {
		t.Fatalf("calendar marker = %#v, want %#v", result.Meta, want)
	}
	result, err = b.newResult(ctx, map[string]any{"action": "unknown"}, mcp.ToolPayload{Text: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta != nil && (*result.Meta)["aura_display_source"] != nil {
		t.Fatalf("unknown action gained marker %#v", (*result.Meta)["aura_display_source"])
	}
}
