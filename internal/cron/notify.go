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

// NotifyRoute is the per-task delivery channel (D-20). An unset route falls through
// to AURA_SCHEDULER_NOTIFY_DEFAULT then the stdout sink.
type NotifyRoute string

// The three delivery routes: WhatsApp + email self-send via MCP (D-19), and the
// always-available stdout fallback sink.
const (
	RouteWhatsApp NotifyRoute = "whatsapp"
	RouteEmail    NotifyRoute = "email"
	RouteStdout   NotifyRoute = "stdout"
)

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
	// dispatcher marks the run notification-undelivered and bound-retries (D-22). An
	// empty route resolves to the configured default.
	Notify(ctx context.Context, route NotifyRoute, recipient, text string) error
}

// compositeNotifier resolves a route to an MCP self-send tool and falls back to
// stdout. A nil resolver (no MCP mounted) degrades every route to the stdout sink,
// which is always available.
type compositeNotifier struct {
	resolver SelfSendResolver
	out      io.Writer
}

// NewNotifier builds the composite Notifier over the self-send resolver. A nil
// resolver is valid: delivery degrades to stdout (the fallback sink is never absent).
func NewNotifier(resolver SelfSendResolver) Notifier {
	return &compositeNotifier{resolver: resolver, out: os.Stdout}
}

// Notify resolves the route (per-task → AURA_SCHEDULER_NOTIFY_DEFAULT → stdout),
// attempts the MCP self-send, and on ANY failure falls back to stdout while
// surfacing the undelivered signal (D-22). The bounded retry across ticks is the
// dispatcher's concern.
func (n *compositeNotifier) Notify(ctx context.Context, route NotifyRoute, recipient, text string) error {
	resolved := n.resolveRoute(route)
	if resolved == RouteStdout {
		return n.stdout(resolved, recipient, text)
	}
	if err := n.sendViaMCP(ctx, resolved, recipient, text); err != nil {
		_ = n.stdout(resolved, recipient, text) // fail-soft fallback (D-22)
		return fmt.Errorf("notify %s: MCP self-send failed, fell back to stdout: %w", resolved, err)
	}
	return nil
}

// resolveRoute applies the route precedence: explicit per-task route →
// AURA_SCHEDULER_NOTIFY_DEFAULT → stdout. An unknown value degrades to stdout
// (fail-safe: a misconfigured route never silently swallows output).
func (n *compositeNotifier) resolveRoute(route NotifyRoute) NotifyRoute {
	r := route
	if r == "" {
		r = NotifyRoute(strings.TrimSpace(os.Getenv("AURA_SCHEDULER_NOTIFY_DEFAULT")))
	}
	switch r {
	case RouteWhatsApp, RouteEmail, RouteStdout:
		return r
	default:
		return RouteStdout
	}
}

// sendViaMCP resolves the route to its MCP tool and executes the self-send. A missing
// tool (nil resolver or no matching MCP server mounted) is an error so the caller
// falls back to stdout.
func (n *compositeNotifier) sendViaMCP(ctx context.Context, route NotifyRoute, recipient, text string) error {
	if n.resolver == nil {
		return fmt.Errorf("no MCP self-send resolver mounted for route %s", route)
	}
	bareName, args := buildSend(route, recipient, text)
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
func buildSend(route NotifyRoute, recipient, text string) (string, json.RawMessage) {
	if strings.TrimSpace(recipient) == "" {
		recipient = strings.TrimSpace(os.Getenv("AURA_SCHEDULER_NOTIFY_RECIPIENT"))
	}
	switch route {
	case RouteEmail:
		args, _ := json.Marshal(map[string]string{
			"to":      recipient,
			"subject": "Aura scheduled task",
			"body":    text,
		})
		return toolSendEmail, args
	default: // whatsapp
		args, _ := json.Marshal(map[string]string{
			"recipient": recipient,
			"message":   text,
		})
		return toolSendMessage, args
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
