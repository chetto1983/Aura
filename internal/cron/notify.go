package cron

// notify.go is the composite Notifier (D-19): scheduled-job output reaches the user
// via the ALREADY-MOUNTED WhatsApp/mail MCP self-send, addressed to the configured
// recipient or, for WhatsApp, to the account the task's identity paired. The task tool
// asks the same Notifier where a route would deliver before it persists a task. On a
// delivery failure it falls back to stdout AND reports notification-undelivered so
// the dispatcher can bound-retry on a later tick (D-22), mirroring the Phase-9
// fail-soft MCP boot posture. Notify-on-failure too (D-21): a failed agent_job rides
// the same route. Quiet-hours (D-23) deferral is the dispatcher's concern — it
// consults the scheduler's Now-based DuringQuietHours predicate before delivering a
// non-destructive notification.
//
// The MCP self-send tools (send_message/send_email) are resolved through a
// cron-local SelfSendResolver interface, NOT a concrete *tools.Registry import: that
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

// selfSendTools maps each external route to the bare name of its MCP self-send tool. MCP
// tools are namespaced <server>__<tool> (mcptools/name.go); the resolver adapter matches
// the bare suffix.
var selfSendTools = map[NotifyRoute]string{
	RouteWhatsApp: "send_message",
	RouteEmail:    "send_email",
}

// SelfSendResolver resolves an MCP self-send tool by its bare name (send_message /
// send_email) to an executable handle. It is the cron-local seam the composition
// root adapts the mounted *tools.Registry onto (consumer-declared interface, the
// 10-04 taskStore pattern), so package cron never imports internal/agent/tools.
type SelfSendResolver interface {
	Resolve(bareName string) (SelfSendTool, bool)
}

// SelfSendTool is one resolved MCP self-send tool. Send returns nil on a delivered
// message and an error on any MCP-side failure (the composite then falls back to
// stdout, D-22).
type SelfSendTool interface {
	Send(ctx context.Context, args json.RawMessage) error
}

// WhatsAppAccounts reports the number of the WhatsApp account an identity paired in the
// cockpit. internal/whatsappbridge.Client satisfies this cron-local seam.
type WhatsAppAccounts interface {
	LinkedNumber(ctx context.Context, identityID string) (string, error)
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
	whatsapp WhatsAppAccounts
	out      io.Writer
}

// NewNotifier builds the composite Notifier over the self-send resolver and the paired
// WhatsApp accounts. A nil resolver is valid for none/stdout and makes external routes
// fail explicitly; nil accounts leave WhatsApp to AURA_SCHEDULER_NOTIFY_RECIPIENT.
func NewNotifier(resolver SelfSendResolver, whatsapp WhatsAppAccounts) Notifier {
	return &compositeNotifier{resolver: resolver, whatsapp: whatsapp, out: os.Stdout}
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
	if _, ok := selfSendTools[route]; !ok {
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
	if err := tool.Send(ctx, selfSendArgs(route, recipient, text)); err != nil {
		return fmt.Errorf("%s send: %w", selfSendTools[route], err)
	}
	return nil
}

// selfSend resolves the mounted MCP tool and the recipient of an external route. A
// missing tool (nil resolver or no matching MCP server mounted) or recipient is an error,
// so the task tool can refuse the route and the dispatcher can record it undelivered.
func (n *compositeNotifier) selfSend(ctx context.Context, route NotifyRoute) (SelfSendTool, string, error) {
	bareName, ok := selfSendTools[route]
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
	tool, ok := n.resolver.Resolve(bareName)
	if !ok {
		return nil, "", fmt.Errorf("no mounted MCP tool for route %s (want *%s)", route, bareName)
	}
	recipient, err := n.recipient(ctx, route)
	if err != nil {
		return nil, "", err
	}
	return tool, recipient, nil
}

// recipient is AURA_SCHEDULER_NOTIFY_RECIPIENT when the deployment set one, and
// otherwise, for WhatsApp, the number of the account the identity on ctx linked in the
// cockpit. Measured 2026-10-05: with neither, every WhatsApp reminder was sent to an
// empty recipient and refused. Email has no linked account to fall back on.
func (n *compositeNotifier) recipient(ctx context.Context, route NotifyRoute) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AURA_SCHEDULER_NOTIFY_RECIPIENT")); configured != "" {
		return configured, nil
	}
	identityID := identityctx.IdentityID(ctx)
	if route != RouteWhatsApp || n.whatsapp == nil || identityID == "" {
		return "", fmt.Errorf("no recipient for %s: set AURA_SCHEDULER_NOTIFY_RECIPIENT", route)
	}
	number, err := n.whatsapp.LinkedNumber(ctx, identityID)
	if err != nil {
		return "", fmt.Errorf("no recipient for whatsapp: %w; link WhatsApp in the cockpit or set AURA_SCHEDULER_NOTIFY_RECIPIENT", err)
	}
	return number, nil
}

// selfSendArgs is the argument JSON of the route's MCP tool, in the shapes of the
// canonical WhatsApp/mail servers (recipient+message / to+subject+body); the upstream
// schema validates them.
func selfSendArgs(route NotifyRoute, recipient, text string) json.RawMessage {
	args := map[string]string{"recipient": recipient, "message": text}
	if route == RouteEmail {
		args = map[string]string{"to": recipient, "subject": "Aura scheduled task", "body": text}
	}
	raw, _ := json.Marshal(args)
	return raw
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
