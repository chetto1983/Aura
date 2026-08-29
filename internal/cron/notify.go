package cron

// notify.go is the composite Notifier (D-19): scheduled-job output reaches the user
// via the ALREADY-MOUNTED WhatsApp/mail MCP self-send (cmd/aura/main.go:150-158
// allowlists already carry send_message/send_email — no new MCP wiring). On a
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

// The MCP self-send tool bare names each route resolves to. MCP tools are namespaced
// <server>__<tool> (mcptools/name.go); the resolver adapter matches the bare suffix.
const (
	toolSendMessage = "send_message" // whatsapp
	toolSendEmail   = "send_email"   // mail
)

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

// Notifier delivers a job's output text to a route's recipient. It is the seam the
// dispatcher rides for both success summaries and failure alerts (D-21) and for the
// RISKY/DESTRUCTIVE immediate alert (D-27).
type Notifier interface {
	// Notify delivers text to recipient over route. It returns nil on delivery
	// (including the stdout fallback), and a non-nil error ONLY when the MCP
	// self-send failed (even though the stdout fallback was written) — so the
	// dispatcher marks the run notification-undelivered and bound-retries (D-22).
	Notify(ctx context.Context, route NotifyRoute, recipient, text string) error
}

// compositeNotifier resolves an explicit route to an MCP self-send tool. Failed
// external sends leave a diagnostic stdout copy but remain failed for bounded retry.
type compositeNotifier struct {
	resolver SelfSendResolver
	out      io.Writer
}

// NewNotifier builds the composite Notifier over the self-send resolver. A nil
// resolver is valid for none/stdout and makes external routes fail explicitly.
func NewNotifier(resolver SelfSendResolver) Notifier {
	return &compositeNotifier{resolver: resolver, out: os.Stdout}
}

// Notify accepts only an explicit route. RouteNone is intentionally silent; a failed
// external send writes a diagnostic stdout copy and still returns an undelivered error.
func (n *compositeNotifier) Notify(ctx context.Context, route NotifyRoute, recipient, text string) error {
	if route == RouteNone {
		return nil
	}
	if !ValidNotifyRoute(string(route)) {
		return fmt.Errorf("notify: invalid explicit route %q", route)
	}
	if route == RouteStdout {
		return n.stdout(route, recipient, text)
	}
	if err := n.sendViaMCP(ctx, route, recipient, text); err != nil {
		_ = n.stdout(route, recipient, text)
		return fmt.Errorf("notify %s: MCP self-send failed, wrote diagnostic stdout copy: %w", route, err)
	}
	return nil
}

// sendViaMCP resolves the route to its MCP tool and executes the self-send. A missing
// tool (nil resolver or no matching MCP server mounted) is an error so the caller
// falls back to stdout.
func (n *compositeNotifier) sendViaMCP(ctx context.Context, route NotifyRoute, recipient, text string) error {
	if route == RouteTelegram {
		// Telegram never had an MCP self-send; the Dispatch origin gate delivers it. Being
		// here means that gate declined because no channel owns this identity or no
		// deliverer is wired, so the honest answer is undelivered. Notify then
		// writes the stdout fallback AND returns non-nil, which is the D-22 contract the
		// dispatcher retries on.
		return fmt.Errorf("route %s has no MCP self-send: no live channel owns this task's identity", route)
	}
	if n.resolver == nil {
		return fmt.Errorf("no MCP self-send resolver mounted for route %s", route)
	}
	bareName, args, err := buildSend(route, recipient, text)
	if err != nil {
		return err
	}
	tool, ok := n.resolver.Resolve(bareName)
	if !ok {
		return fmt.Errorf("no mounted MCP tool for route %s (want *%s)", route, bareName)
	}
	if err := tool.Send(ctx, args); err != nil {
		return fmt.Errorf("%s send: %w", bareName, err)
	}
	return nil
}

// buildSend maps a route to its MCP tool bare name + the self-send argument JSON. The
// recipient falls back to AURA_SCHEDULER_NOTIFY_RECIPIENT when the per-task field is
// empty (D-20). The argument shapes match the canonical WhatsApp/mail MCP servers
// (recipient+message / to+subject+body); the upstream schema validates them.
func buildSend(route NotifyRoute, recipient, text string) (string, json.RawMessage, error) {
	if strings.TrimSpace(recipient) == "" {
		recipient = strings.TrimSpace(os.Getenv("AURA_SCHEDULER_NOTIFY_RECIPIENT"))
	}
	whatsapp := func() (string, json.RawMessage, error) {
		args, _ := json.Marshal(map[string]string{
			"recipient": recipient,
			"message":   text,
		})
		return toolSendMessage, args, nil
	}
	switch route {
	case RouteEmail:
		args, _ := json.Marshal(map[string]string{
			"to":      recipient,
			"subject": "Aura scheduled task",
			"body":    text,
		})
		return toolSendEmail, args, nil
	case RouteWhatsApp:
		return whatsapp()
	default:
		return "", nil, fmt.Errorf("route %q has no MCP self-send mapping", route)
	}
}

// stdout writes the notification to the fallback sink (a daemon nobody tails still
// leaves the line in the journal). It returns an error only if the write itself
// fails — the dispatcher treats that as notification-undelivered.
func (n *compositeNotifier) stdout(route NotifyRoute, recipient, text string) error {
	if _, err := fmt.Fprintf(n.out, "[scheduler notify route=%s to=%s] %s\n", route, recipient, text); err != nil {
		return fmt.Errorf("stdout notify: %w", err)
	}
	return nil
}
