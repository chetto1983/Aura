package cron

// notify.go is the composite Notifier (D-19): scheduled-job output reaches the user
// via the ALREADY-MOUNTED WhatsApp / PIM-mail MCP self-send, addressed to the configured
// recipient or to the task identity's own number or address. The task tool
// asks the same Notifier where a route would deliver before it persists a task. On a
// delivery failure it falls back to stdout AND reports notification-undelivered so
// the dispatcher can bound-retry on a later tick (D-22), mirroring the Phase-9
// fail-soft MCP boot posture. Notify-on-failure too (D-21): a failed agent_job rides
// the same route. Quiet-hours (D-23) deferral is the dispatcher's concern — it
// consults the scheduler's Now-based DuringQuietHours predicate before delivering a
// non-destructive notification.
//
// The MCP self-send tools (WhatsApp send_message, the PIM calendar tool) are resolved
// through a cron-local SelfSendResolver interface, NOT a concrete *tools.Registry import: that
// keeps package cron free of an internal/agent/tools import (tools/task.go already
// imports cron — the reverse import would be a cycle). The composition root (cmd/aura,
// which imports both) supplies a thin adapter over the mounted registry.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
)

// NotifyRoute is the per-task delivery instruction (D-20).
type NotifyRoute string

// The delivery routes. WhatsApp and email are MCP self-sends (D-19) this composite
// performs itself; stdout is the always-available fallback sink. Telegram is neither:
// it is a CHANNEL route, delivered one layer up by the Dispatch origin gate through
// ChannelDeliverer, which is the only place holding the task's identity and the live
// channel registry. It is listed here because it is a value the enum must accept —
// reaching this composite WITH it means the origin gate already declined, and
// sendViaMCP says so rather than mis-sending it as a WhatsApp message.
const (
	RouteNone     NotifyRoute = "none"
	RouteWhatsApp NotifyRoute = "whatsapp"
	RouteEmail    NotifyRoute = "email"
	RouteStdout   NotifyRoute = "stdout"
	RouteTelegram NotifyRoute = "telegram"
)

// ValidNotifyRoute is the SINGLE source of truth for the route enum, consumed by every
// validating caller (the task tool, the cockpit scheduler API, the CLI flag). Empty is
// invalid: omission must reach the operator-choice flow rather than an implicit default.
func ValidNotifyRoute(route string) bool {
	switch NotifyRoute(route) {
	case RouteNone, RouteWhatsApp, RouteEmail, RouteStdout, RouteTelegram:
		return true
	default:
		return false
	}
}

// selfSendRoute names the MCP tool that carries an external route by the managed recipe
// the host mounted it from (tools.Spec TrustedRecipeSource/TrustedRecipeTool), never by a
// registered name, and builds that tool's arguments.
type selfSendRoute struct {
	recipe, tool string
	args         func(recipient, text string) map[string]any
}

var selfSendRoutes = map[NotifyRoute]selfSendRoute{
	RouteWhatsApp: {recipe: "recipe:whatsapp", tool: "send_message", args: func(recipient, text string) map[string]any {
		return map[string]any{"recipient": recipient, "message": text}
	}},
	// The PIM multiplexes mail behind its one calendar tool. Without an accountId it picks
	// the sending account itself: by the recipient's domain, else its first account.
	RouteEmail: {recipe: "recipe:calendar", tool: "calendar", args: func(recipient, text string) map[string]any {
		return map[string]any{"action": "send_email", "to": []string{recipient}, "subject": "Aura scheduled task", "body": text, "bodyFormat": "text"}
	}},
}

// SelfSendResolver resolves the MCP tool mounted from a managed recipe to an executable
// handle. It is the cron-local seam the composition root adapts the mounted
// *tools.Registry onto (consumer-declared interface, the 10-04 taskStore pattern), so
// package cron never imports internal/agent/tools.
type SelfSendResolver interface {
	Resolve(recipe, tool string) (SelfSendTool, bool)
}

// SelfSendTool is one resolved MCP self-send tool. Send returns nil on a delivered
// message and an error on any MCP-side failure (the composite then falls back to
// stdout, D-22).
type SelfSendTool interface {
	Send(ctx context.Context, args json.RawMessage) error
}

// OwnAddresses names where an identity itself is reached on an external route: the
// number of the WhatsApp account it paired in the cockpit, the address it signs in with.
type OwnAddresses interface {
	OwnAddress(ctx context.Context, route NotifyRoute, identityID string) (string, error)
}

// Notifier delivers a job's output text over an explicit route. It is the seam the
// dispatcher rides for both success summaries and failure alerts (D-21) and for the
// RISKY/DESTRUCTIVE immediate alert (D-27), and the one the task tool asks where a
// route would deliver before it persists a task.
type Notifier interface {
	// Notify delivers text over route to the recipient Destination resolves for the
	// identity on ctx. It returns nil on delivery (including the stdout route), and a
	// non-nil error when the route is invalid or the MCP self-send failed (after a
	// diagnostic stdout copy) — so the dispatcher marks the run notification-undelivered
	// and bound-retries (D-22).
	Notify(ctx context.Context, route NotifyRoute, text string) error
	// Destination resolves, without sending, the recipient an external self-send route
	// reaches for the identity on ctx, and "" for every route that addresses no one. A
	// non-nil error is the reason the route cannot deliver now.
	Destination(ctx context.Context, route NotifyRoute) (string, error)
}

// compositeNotifier resolves an explicit route to an MCP self-send tool. Failed
// external sends leave a diagnostic stdout copy but remain failed for bounded retry.
type compositeNotifier struct {
	resolver SelfSendResolver
	own      OwnAddresses
	out      io.Writer
}

// NewNotifier builds the composite Notifier over the self-send resolver and the
// identities' own addresses. A nil resolver is valid for none/stdout and makes external
// routes fail explicitly; nil addresses leave every route to AURA_SCHEDULER_NOTIFY_RECIPIENT.
func NewNotifier(resolver SelfSendResolver, own OwnAddresses) Notifier {
	return &compositeNotifier{resolver: resolver, own: own, out: os.Stdout}
}

// Notify accepts only an explicit route. RouteNone is intentionally silent; a failed
// external send writes a diagnostic stdout copy and still returns an undelivered error.
func (n *compositeNotifier) Notify(ctx context.Context, route NotifyRoute, text string) error {
	if route == RouteNone {
		return nil
	}
	if !ValidNotifyRoute(string(route)) {
		return fmt.Errorf("notify: invalid explicit route %q", route)
	}
	if route == RouteStdout {
		return n.stdout(route, text)
	}
	if err := n.sendViaMCP(ctx, route, text); err != nil {
		_ = n.stdout(route, text)
		return fmt.Errorf("notify %s: MCP self-send failed, wrote diagnostic stdout copy: %w", route, err)
	}
	return nil
}

func (n *compositeNotifier) Destination(ctx context.Context, route NotifyRoute) (string, error) {
	if _, ok := selfSendRoutes[route]; !ok {
		return "", nil
	}
	_, recipient, err := n.selfSend(ctx, route)
	return recipient, err
}

func (n *compositeNotifier) sendViaMCP(ctx context.Context, route NotifyRoute, text string) error {
	tool, recipient, err := n.selfSend(ctx, route)
	if err != nil {
		return err
	}
	args, _ := json.Marshal(selfSendRoutes[route].args(recipient, text))
	if err := tool.Send(ctx, args); err != nil {
		return fmt.Errorf("%s send: %w", selfSendRoutes[route].tool, err)
	}
	return nil
}

// selfSend resolves the mounted MCP tool and the recipient of an external route. A
// missing tool (nil resolver or no matching MCP server mounted) or recipient is an error,
// so the task tool can refuse the route and the dispatcher can record it undelivered.
func (n *compositeNotifier) selfSend(ctx context.Context, route NotifyRoute) (SelfSendTool, string, error) {
	carrier, ok := selfSendRoutes[route]
	if !ok {
		// Telegram never had an MCP self-send; the Dispatch origin gate delivers it. Being
		// here means that gate declined because no channel owns this identity or no
		// deliverer is wired, so the honest answer is undelivered. Notify then
		// writes the stdout fallback AND returns non-nil, which is the D-22 contract the
		// dispatcher retries on.
		return nil, "", fmt.Errorf("route %s has no MCP self-send: no live channel owns this task's identity", route)
	}
	if n.resolver == nil {
		return nil, "", fmt.Errorf("no MCP self-send resolver mounted for route %s", route)
	}
	tool, ok := n.resolver.Resolve(carrier.recipe, carrier.tool)
	if !ok {
		return nil, "", fmt.Errorf("no MCP tool mounted for route %s (want %s %s)", route, carrier.recipe, carrier.tool)
	}
	recipient, err := n.recipient(ctx, route)
	if err != nil {
		return nil, "", err
	}
	return tool, recipient, nil
}

// recipient is AURA_SCHEDULER_NOTIFY_RECIPIENT when the deployment set one, and
// otherwise the identity's own address on the route. Measured 2026-10-05: with neither,
// every WhatsApp reminder was sent to an empty recipient and refused.
func (n *compositeNotifier) recipient(ctx context.Context, route NotifyRoute) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AURA_SCHEDULER_NOTIFY_RECIPIENT")); configured != "" {
		return configured, nil
	}
	identityID := identityctx.IdentityID(ctx)
	if n.own == nil || identityID == "" {
		return "", fmt.Errorf("no recipient for %s: set AURA_SCHEDULER_NOTIFY_RECIPIENT", route)
	}
	address, err := n.own.OwnAddress(ctx, route, identityID)
	if err != nil {
		return "", fmt.Errorf("no recipient for %s: %w (or set AURA_SCHEDULER_NOTIFY_RECIPIENT)", route, err)
	}
	return address, nil
}

// stdout writes the notification to the fallback sink (a daemon nobody tails still
// leaves the line in the journal). It returns an error only if the write itself
// fails — the dispatcher treats that as notification-undelivered.
func (n *compositeNotifier) stdout(route NotifyRoute, text string) error {
	if _, err := fmt.Fprintf(n.out, "[scheduler notify route=%s] %s\n", route, text); err != nil {
		return fmt.Errorf("stdout notify: %w", err)
	}
	return nil
}
