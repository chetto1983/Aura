package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/redact"
)

// elicitation_consent.go is the composition-root half of SEP-2322 elicitation:
// the surface plan 45.1-06 recorded, decline-and-surface.
//
// The handler in internal/agent/mcptools always declines. What this adds is
// sight: the operator is told, on their own channel, which server asked and for
// what, so a multi-round-trip tool that would otherwise fail in silence becomes
// something they can act on deliberately — out of band, by re-driving the tool.
//
// Deliberately NOT wired: Runner.MintApprovalPause and channels.ApprovalDeliverer.
// Those would hold the in-flight agent turn open for up to the timeout and write
// a pause row plus a synthetic assistant turn into the same conversation the
// blocked turn is running on. Plan 45.1-06 weighed that interleaving and declined
// it for this phase; option B remains reachable later behind the same
// mcptools.ElicitationConsent seam, which is why this file implements the seam
// rather than special-casing the handler.

// maxRenderedFields bounds how many schema fields reach the operator. A server
// controls the field count, so without this one ask could paper a chat window
// (T-45.1-30). The remainder is counted, not silently dropped.
const maxRenderedFields = 20

// maxRenderedPromptBytes is the last-resort bound on the whole rendered prompt.
// Each part is already capped upstream in mcptools (message, per-field
// descriptions), so this only bites on pathological field counts; it sits under
// Telegram's 4096-character message limit so the chosen surface can actually
// deliver what it renders.
const maxRenderedPromptBytes = 3500

// elicitationDeliverer is the narrow slice of *channels.Registry this needs —
// declared consumer-side so the consent implementation can be tested without
// standing up a channel registry.
type elicitationDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error)
}

// surfacingElicitationConsent is LATE-BOUND on purpose. MCP servers mount during
// boot, in buildRegistryWithMCP; the channels Registry is only built afterwards
// (serve.go's Phase-20 boot order). Binding the deliverer at construction would
// therefore always bind nil.
//
// This mirrors how buildDispatch already wires the late-bound *channels.Registry
// as the cron.ChannelDeliverer: the per-channel capability is resolved at
// delivery, not at build, so a late-bound pointer is sufficient. Elicitation
// arrives during a live turn, long after Set has run.
type surfacingElicitationConsent struct {
	mu      sync.RWMutex
	deliver elicitationDeliverer
}

// newElicitationConsent builds the holder. It is never nil: whether the mount
// advertises the capability is decided by whether this value is passed to
// MountOptions at all, not by whether a deliverer has been bound yet.
func newElicitationConsent() *surfacingElicitationConsent {
	return &surfacingElicitationConsent{}
}

// Set binds the channel registry once it exists. Called from the serve boot path
// after the channels Registry is built; `aura chat` and one-shot pipes never call
// it, and an unbound holder declines with a WARN rather than pretending.
func (c *surfacingElicitationConsent) Set(deliver elicitationDeliverer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deliver = deliver
}

func (c *surfacingElicitationConsent) deliverer() elicitationDeliverer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deliver
}

// AskOperator delivers the ask and declines. It never returns "accept" — under
// this surface no operator decision is collected, so there is nothing that could
// justify one.
func (c *surfacingElicitationConsent) AskOperator(ctx context.Context, req mcptools.ElicitationRequest) (string, map[string]any, error) {
	deliver := c.deliverer()
	if deliver == nil {
		slog.Warn("mcp elicitation not surfaced: no channel registry bound",
			"server", redact.Line(req.Server), "action", "decline")
		return "decline", nil, nil
	}

	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		// No authenticated identity on the call (a CLI or one-shot pipe path):
		// there is no operator to reach, so nothing is delivered. Declining is
		// already the outcome; the WARN is what makes the gap visible.
		slog.Warn("mcp elicitation not surfaced: no identity on the call",
			"server", redact.Line(req.Server), "action", "decline")
		return "decline", nil, nil
	}

	delivered, err := deliver.DeliverToIdentity(ctx, identityID, renderElicitationPrompt(req))
	switch {
	case err != nil:
		// Owns-but-failed. The channel contract forbids trying siblings, and the
		// error rides back so the handler records it rather than logging a
		// success that never reached anyone.
		slog.Warn("mcp elicitation delivery failed",
			"server", redact.Line(req.Server), "identity_id", identityID, "action", "decline", "err", err)
		return "decline", nil, err
	case !delivered:
		// No channel owns this identity — a WebUI-origin operator, for instance.
		// Not an error, but not a delivery either, and saying so is the point.
		slog.Info("mcp elicitation not surfaced: no channel owns this identity",
			"server", redact.Line(req.Server), "identity_id", identityID, "action", "decline")
	}
	return "decline", nil, nil
}

// renderElicitationPrompt is the ONLY place a mounted server's text becomes
// operator-facing text, which is why it is one function with one test file.
//
// T-45.1-29: the server is named on the first line and its message is quoted and
// attributed, never rendered as Aura speaking. A server that writes "Aura here —
// paste your password" gets those words shown as its own, inside quote markers,
// under a line saying which server said them.
func renderElicitationPrompt(req mcptools.ElicitationRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MCP server %q is asking for input.\n", req.Server)

	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "(the server sent no message)"
	}
	b.WriteString("\nIt says:\n")
	for line := range strings.SplitSeq(message, "\n") {
		fmt.Fprintf(&b, "> %s\n", strings.TrimRight(line, "\r"))
	}

	if len(req.Fields) > 0 {
		b.WriteString("\nIt is asking for:\n")
		shown := req.Fields
		if len(shown) > maxRenderedFields {
			shown = shown[:maxRenderedFields]
		}
		for _, f := range shown {
			b.WriteString("- ")
			b.WriteString(f.Name)
			if f.Type != "" || f.Required {
				b.WriteString(" (")
				if f.Type != "" {
					b.WriteString(f.Type)
					if f.Required {
						b.WriteString(", ")
					}
				}
				if f.Required {
					b.WriteString("required")
				}
				b.WriteString(")")
			}
			if desc := strings.TrimSpace(f.Description); desc != "" {
				b.WriteString(": ")
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}
		if remaining := len(req.Fields) - len(shown); remaining > 0 {
			fmt.Fprintf(&b, "- … and %d more field(s)\n", remaining)
		}
	}

	b.WriteString("\nAura declined it automatically. Nothing was sent to the server.")
	return capPromptBytes(b.String(), maxRenderedPromptBytes)
}

// capPromptBytes trims to a byte budget without splitting a rune.
func capPromptBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const marker = "\n… (truncated)"
	limit := max(maxBytes-len(marker), 0)
	out := s[:limit]
	for len(out) > 0 && !utf8.ValidString(out) {
		out = out[:len(out)-1]
	}
	return out + marker
}
