package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/whatsappbridge"
)

// recipeTool is a mounted tool as the MCP bridge registers it: a registered name that
// may be truncated, plus the host-owned recipe provenance. Its Execute builds the result
// the way a bridged tool does, through tools.NewResult.
type recipeTool struct {
	name, recipe, tool string
	gotArgs            json.RawMessage
}

func (r *recipeTool) Spec() tools.Spec {
	return tools.Spec{Name: r.name, TrustedRecipeSource: r.recipe, TrustedRecipeTool: r.tool}
}

func (r *recipeTool) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	r.gotArgs = args
	return tools.NewResult(ctx, `{"success":true}`)
}

// TestTaskToolChecksRoutesOnlyWithAStore: the pool-free manifest path builds the tool to
// read its Spec; only the live path, which persists, asks the scheduler notifier first,
// and that notifier refuses an external route no mounted MCP tool can send.
func TestTaskToolChecksRoutesOnlyWithAStore(t *testing.T) {
	t.Parallel()

	notifier := newSchedulerNotifier(&config.Config{}, tools.NewRegistry(), nil)
	if got := newTaskTool(nil, notifier); got.Destinations != nil {
		t.Fatal("the pool-free task tool must not carry a route check")
	}
	if got := newTaskTool(newCronTaskStore(nil, nil), notifier); got.Destinations == nil {
		t.Fatal("the live task tool must check a route before persisting")
	}
	owner := identityctx.WithIdentityID(context.Background(), "10000000-0000-4000-8000-000000000001")
	if _, err := notifier.Destination(owner, cron.RouteWhatsApp); err == nil || !strings.Contains(err.Error(), "no MCP tool mounted") {
		t.Fatalf("Destination(whatsapp) with nothing mounted = %v, want the missing-tool reason", err)
	}
}

// TestSelfSendResolverMatchesTheManagedRecipe: a tool merely NAMED like the WhatsApp send
// is not the one the recipe mounted; only the host-owned provenance selects it.
func TestSelfSendResolverMatchesTheManagedRecipe(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.Register(&recipeTool{name: "lookalike__send_message"})
	trusted := &recipeTool{name: "whatsapp__send_message", recipe: "recipe:whatsapp", tool: "send_message"}
	reg.Register(trusted)
	r := selfSendResolver{reg: reg, runDir: t.TempDir(), previewCap: 4096}

	got, ok := r.Resolve("recipe:whatsapp", "send_message")
	if !ok || got.(selfSendTool).tool != trusted {
		t.Fatalf("Resolve(recipe:whatsapp send_message) = %v, %v; want the recipe-mounted tool", got, ok)
	}
	if _, ok := r.Resolve("recipe:calendar", "calendar"); ok {
		t.Fatal("an unmounted recipe resolved")
	}
	if _, ok := (selfSendResolver{}).Resolve("recipe:whatsapp", "send_message"); ok {
		t.Fatal("a resolver without a registry resolved")
	}
}

// TestSelfSendRunsWithAToolCallContext pins the second defect measured on 2026-10-05: the
// bridge sent the WhatsApp reminder, then tools.NewResult refused to build its result
// without a tool-call context, so the send was reported failed and every retry sent it
// again. A scheduler self-send must succeed exactly when the tool did.
func TestSelfSendRunsWithAToolCallContext(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	sender := &recipeTool{name: "whatsapp__send_message", recipe: "recipe:whatsapp", tool: "send_message"}
	reg.Register(sender)
	r := selfSendResolver{reg: reg, runDir: t.TempDir(), previewCap: 4096}
	tool, _ := r.Resolve("recipe:whatsapp", "send_message")

	if err := tool.Send(context.Background(), json.RawMessage(`{"recipient":"393331112222","message":"drink water"}`)); err != nil {
		t.Fatalf("Send = %v; a delivered message must not be reported failed", err)
	}
	if string(sender.gotArgs) != `{"recipient":"393331112222","message":"drink water"}` {
		t.Fatalf("tool got args %s", sender.gotArgs)
	}
}

func TestOwnAddresses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	own := ownAddresses{}
	if _, err := own.OwnAddress(ctx, cron.RouteWhatsApp, "id-1"); !errors.Is(err, whatsappbridge.ErrNotConfigured) {
		t.Fatalf("whatsapp without a bridge = %v, want ErrNotConfigured", err)
	}
	if _, err := own.OwnAddress(ctx, cron.RouteEmail, "id-1"); err == nil || !strings.Contains(err.Error(), "no identity store") {
		t.Fatalf("email without an identity store = %v, want the missing-store reason", err)
	}
	if _, err := own.OwnAddress(ctx, cron.RouteStdout, "id-1"); err == nil {
		t.Fatal("stdout has no own address, got none refused")
	}
}

// TestSignInEmail: a user identity's name is the Authula email it signs in with
// (serve_auth.go joins the two); anything else has no address to default to.
func TestSignInEmail(t *testing.T) {
	t.Parallel()

	if got, err := signInEmail(identity.Identity{Kind: "user", Name: "owner@example.com"}); err != nil || got != "owner@example.com" {
		t.Fatalf("signInEmail(user) = %q, %v", got, err)
	}
	for _, owner := range []identity.Identity{
		{Kind: "service", Name: "cli@example.com"},
		{Kind: "user", Name: "local"},
	} {
		if got, err := signInEmail(owner); err == nil {
			t.Fatalf("signInEmail(%+v) = %q, want an error", owner, got)
		}
	}
}
