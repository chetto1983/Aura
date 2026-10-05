package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeSelfSend records the bare name + args it was resolved/sent with, and can be
// scripted to fail (to drive the stdout fallback + undelivered signal).
type fakeSelfSend struct {
	resolved []string
	sent     []json.RawMessage
	failWith error
	missing  map[string]bool // bare names that resolve to "not mounted"
}

func (f *fakeSelfSend) Resolve(bareName string) (SelfSendTool, bool) {
	f.resolved = append(f.resolved, bareName)
	if f.missing[bareName] {
		return nil, false
	}
	return &fakeSelfSendTool{parent: f}, true
}

type fakeSelfSendTool struct{ parent *fakeSelfSend }

func (t *fakeSelfSendTool) Send(_ context.Context, args json.RawMessage) error {
	t.parent.sent = append(t.parent.sent, args)
	return t.parent.failWith
}

// fakeLinked is the bridge's answer for the identity's paired WhatsApp account.
type fakeLinked struct {
	number string
	err    error
	asked  []string
}

func (f *fakeLinked) LinkedNumber(_ context.Context, identityID string) (string, error) {
	f.asked = append(f.asked, identityID)
	return f.number, f.err
}

const linkedIdentity = "448ddbe1-96ea-405d-8219-4a3d52a425c0"

func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), linkedIdentity)
}

func sentArgs(t *testing.T, fss *fakeSelfSend) map[string]string {
	t.Helper()
	if len(fss.sent) != 1 {
		t.Fatalf("sent %d self-send(s), want 1", len(fss.sent))
	}
	var got map[string]string
	if err := json.Unmarshal(fss.sent[0], &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNotifyWhatsAppConfiguredRecipientWins(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "393331112222")
	fss := &fakeSelfSend{}
	linked := &fakeLinked{number: "390000000000"}
	n := NewNotifier(fss, linked)
	if err := n.Notify(ownerCtx(), RouteWhatsApp, "borsa update"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "send_message" {
		t.Fatalf("whatsapp must resolve send_message, got %v", fss.resolved)
	}
	if got := sentArgs(t, fss); got["recipient"] != "393331112222" || got["message"] != "borsa update" {
		t.Fatalf("unexpected send_message args: %v", got)
	}
	if len(linked.asked) != 0 {
		t.Fatalf("a configured recipient must not ask the bridge, asked for %v", linked.asked)
	}
}

// TestNotifyWhatsAppDefaultsToTheLinkedNumber pins the measured fix (2026-10-05): with
// no configured recipient, every WhatsApp reminder went to an empty one and was refused.
// The default is the account the task's identity linked in the cockpit.
func TestNotifyWhatsAppDefaultsToTheLinkedNumber(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	fss := &fakeSelfSend{}
	linked := &fakeLinked{number: "393331112222"}
	n := NewNotifier(fss, linked)
	if err := n.Notify(ownerCtx(), RouteWhatsApp, "drink water"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got := sentArgs(t, fss); got["recipient"] != "393331112222" {
		t.Fatalf("recipient = %q, want the linked number", got["recipient"])
	}
	if len(linked.asked) != 1 || linked.asked[0] != linkedIdentity {
		t.Fatalf("bridge asked for %v, want the task identity", linked.asked)
	}
}

func TestNotifyWithoutRecipientIsUndeliveredAndSendsNothing(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	for _, tc := range []struct {
		name   string
		route  NotifyRoute
		ctx    context.Context
		linked WhatsAppAccounts
		want   string
	}{
		{"whatsapp not linked", RouteWhatsApp, ownerCtx(), &fakeLinked{err: errors.New("no WhatsApp account is linked")}, "link WhatsApp in the cockpit"},
		{"whatsapp without identity", RouteWhatsApp, context.Background(), &fakeLinked{number: "393331112222"}, "no recipient for whatsapp"},
		{"whatsapp without bridge", RouteWhatsApp, ownerCtx(), nil, "no recipient for whatsapp"},
		{"email has no linked account", RouteEmail, ownerCtx(), &fakeLinked{number: "393331112222"}, "no recipient for email"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fss := &fakeSelfSend{}
			var buf bytes.Buffer
			n := &compositeNotifier{resolver: fss, whatsapp: tc.linked, out: &buf}
			err := n.Notify(tc.ctx, tc.route, "drink water")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Notify error = %v, want it to contain %q", err, tc.want)
			}
			if len(fss.sent) != 0 {
				t.Fatalf("a send with no recipient reached the MCP tool: %s", fss.sent)
			}
			if !strings.Contains(buf.String(), "drink water") {
				t.Fatalf("expected the diagnostic stdout copy, got %q", buf.String())
			}
		})
	}
}

func TestNotifyEmailResolvesSendEmail(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "a@b.com")
	fss := &fakeSelfSend{}
	n := NewNotifier(fss, nil)
	if err := n.Notify(context.Background(), RouteEmail, "report"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "send_email" {
		t.Fatalf("email must resolve send_email, got %v", fss.resolved)
	}
	if got := sentArgs(t, fss); got["to"] != "a@b.com" || got["body"] != "report" || got["subject"] != "Aura scheduled task" {
		t.Fatalf("unexpected send_email args: %v", got)
	}
}

func TestNotifyMCPFailureFallsBackToStdoutAndSignalsUndelivered(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "x")
	fss := &fakeSelfSend{failWith: errors.New("mcp down")}
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: fss, out: &buf}

	err := n.Notify(context.Background(), RouteWhatsApp, "deliver me")
	if err == nil || !strings.Contains(err.Error(), "send_message send: mcp down") {
		t.Fatalf("a failed MCP self-send must return its error (notification-undelivered â†’ D-22 bound retry), got %v", err)
	}
	if !strings.Contains(buf.String(), "deliver me") {
		t.Fatalf("expected the stdout fallback to carry the text, got %q", buf.String())
	}
}

func TestNotifyMissingMountFallsBackToStdout(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "x")
	fss := &fakeSelfSend{missing: map[string]bool{"send_message": true}}
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: fss, out: &buf}
	if err := n.Notify(context.Background(), RouteWhatsApp, "no mount"); err == nil {
		t.Fatal("an unmounted route must surface undelivered (stdout fallback) ")
	}
	if !strings.Contains(buf.String(), "no mount") {
		t.Fatalf("stdout fallback missing the text: %q", buf.String())
	}
}

func TestNotifyStdoutRouteAlwaysDelivers(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: nil, out: &buf}
	if err := n.Notify(context.Background(), RouteStdout, "tick"); err != nil {
		t.Fatalf("stdout route must always deliver, got %v", err)
	}
	if buf.String() != "[scheduler notify route=stdout] tick\n" {
		t.Fatalf("stdout sink = %q", buf.String())
	}
}

func TestNotifyNoneIsSilent(t *testing.T) {
	var buf bytes.Buffer
	fss := &fakeSelfSend{}
	n := &compositeNotifier{resolver: fss, out: &buf}
	if err := n.Notify(context.Background(), RouteNone, "silent"); err != nil {
		t.Fatalf("Notify(none): %v", err)
	}
	if len(fss.resolved) != 0 || buf.Len() != 0 {
		t.Fatalf("none touched a delivery sink: resolved=%v stdout=%q", fss.resolved, buf.String())
	}
}

func TestNotifyRejectsMissingOrUnknownRoute(t *testing.T) {
	t.Parallel()
	for _, route := range []NotifyRoute{"", "carrier-pigeon"} {
		var buf bytes.Buffer
		n := &compositeNotifier{resolver: &fakeSelfSend{}, out: &buf}
		if err := n.Notify(context.Background(), route, "garbled"); err == nil {
			t.Fatalf("route %q = nil error, want explicit validation failure", route)
		}
		if buf.Len() != 0 {
			t.Fatalf("invalid route %q wrote to stdout: %q", route, buf.String())
		}
	}
}

// TestDestinationResolvesWithoutSending is the task tool's schedule-time check: the
// recipient an external route would reach now, or the reason it cannot deliver, with
// nothing sent. Routes that address no one answer "" without touching any tool.
func TestDestinationResolvesWithoutSending(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	fss := &fakeSelfSend{}
	n := &compositeNotifier{resolver: fss, whatsapp: &fakeLinked{number: "393331112222"}}

	got, err := n.Destination(ownerCtx(), RouteWhatsApp)
	if err != nil || got != "393331112222" {
		t.Fatalf("Destination(whatsapp) = %q, %v; want the linked number", got, err)
	}
	if len(fss.sent) != 0 {
		t.Fatal("Destination sent a message")
	}
	if _, err := n.Destination(ownerCtx(), RouteEmail); err == nil {
		t.Fatal("Destination(email) without a configured recipient = nil error")
	}
	resolvedBefore := len(fss.resolved)
	for _, route := range []NotifyRoute{RouteNone, RouteStdout, RouteTelegram} {
		if got, err := n.Destination(ownerCtx(), route); got != "" || err != nil {
			t.Fatalf("Destination(%s) = %q, %v; want \"\", nil", route, got, err)
		}
	}
	if len(fss.resolved) != resolvedBefore {
		t.Fatal("a route that addresses no one resolved an MCP tool")
	}

	unmounted := &compositeNotifier{resolver: &fakeSelfSend{missing: map[string]bool{"send_message": true}}, whatsapp: &fakeLinked{number: "393331112222"}}
	if _, err := unmounted.Destination(ownerCtx(), RouteWhatsApp); err == nil || !strings.Contains(err.Error(), "no mounted MCP tool") {
		t.Fatalf("Destination(whatsapp) with no mounted tool = %v, want the missing-tool reason", err)
	}
}
