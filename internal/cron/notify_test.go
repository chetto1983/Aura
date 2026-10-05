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

// fakeSelfSend records the recipe tool it was resolved for and the args it was sent with,
// and can be scripted to fail (to drive the stdout fallback + undelivered signal).
type fakeSelfSend struct {
	resolved []string
	sent     []json.RawMessage
	failWith error
	missing  map[string]bool // "<recipe> <tool>" that resolve to "not mounted"
}

func (f *fakeSelfSend) Resolve(recipe, tool string) (SelfSendTool, bool) {
	key := recipe + " " + tool
	f.resolved = append(f.resolved, key)
	if f.missing[key] {
		return nil, false
	}
	return &fakeSelfSendTool{parent: f}, true
}

type fakeSelfSendTool struct{ parent *fakeSelfSend }

func (t *fakeSelfSendTool) Send(_ context.Context, args json.RawMessage) error {
	t.parent.sent = append(t.parent.sent, args)
	return t.parent.failWith
}

// fakeOwn answers where the identity itself is reached on each route.
type fakeOwn struct {
	address map[NotifyRoute]string
	err     error
	asked   []string
}

func (f *fakeOwn) OwnAddress(_ context.Context, route NotifyRoute, identityID string) (string, error) {
	f.asked = append(f.asked, string(route)+" "+identityID)
	return f.address[route], f.err
}

const ownerIdentity = "448ddbe1-96ea-405d-8219-4a3d52a425c0"

func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), ownerIdentity)
}

func ownAddressBook() *fakeOwn {
	return &fakeOwn{address: map[NotifyRoute]string{RouteWhatsApp: "393331112222", RouteEmail: "owner@example.com"}}
}

func sentArgs(t *testing.T, fss *fakeSelfSend) map[string]any {
	t.Helper()
	if len(fss.sent) != 1 {
		t.Fatalf("sent %d self-send(s), want 1", len(fss.sent))
	}
	var got map[string]any
	if err := json.Unmarshal(fss.sent[0], &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNotifyConfiguredRecipientWins(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "393331112222")
	fss := &fakeSelfSend{}
	own := ownAddressBook()
	n := NewNotifier(fss, own)
	if err := n.Notify(ownerCtx(), RouteWhatsApp, "borsa update"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "recipe:whatsapp send_message" {
		t.Fatalf("whatsapp must resolve the whatsapp recipe's send_message, got %v", fss.resolved)
	}
	if got := sentArgs(t, fss); got["recipient"] != "393331112222" || got["message"] != "borsa update" {
		t.Fatalf("unexpected send_message args: %v", got)
	}
	if len(own.asked) != 0 {
		t.Fatalf("a configured recipient must not look up an own address, asked %v", own.asked)
	}
}

// TestNotifyDefaultsToTheIdentitysOwnAddress pins the measured fix (2026-10-05): with no
// configured recipient, every WhatsApp reminder went to an empty one and was refused. The
// default is where the task's identity itself is reached on the route.
func TestNotifyDefaultsToTheIdentitysOwnAddress(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	fss := &fakeSelfSend{}
	own := ownAddressBook()
	n := NewNotifier(fss, own)
	if err := n.Notify(ownerCtx(), RouteWhatsApp, "drink water"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got := sentArgs(t, fss); got["recipient"] != "393331112222" {
		t.Fatalf("recipient = %v, want the paired number", got["recipient"])
	}
	if len(own.asked) != 1 || own.asked[0] != "whatsapp "+ownerIdentity {
		t.Fatalf("own address asked %v, want the task identity on whatsapp", own.asked)
	}
}

// TestNotifyEmailRidesThePIMCalendarTool: the PIM multiplexes mail behind its calendar
// tool, so an email notification is that tool's send_email action. Measured 2026-10-05,
// the old bare "send_email" name matched no mounted tool and every email route failed.
func TestNotifyEmailRidesThePIMCalendarTool(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	fss := &fakeSelfSend{}
	n := NewNotifier(fss, ownAddressBook())
	if err := n.Notify(ownerCtx(), RouteEmail, "report"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "recipe:calendar calendar" {
		t.Fatalf("email must resolve the calendar recipe's calendar tool, got %v", fss.resolved)
	}
	got := sentArgs(t, fss)
	to, _ := got["to"].([]any)
	if got["action"] != "send_email" || len(to) != 1 || to[0] != "owner@example.com" ||
		got["subject"] != "Aura scheduled task" || got["body"] != "report" || got["bodyFormat"] != "text" {
		t.Fatalf("unexpected send_email args: %v", got)
	}
	if _, picked := got["accountId"]; picked {
		t.Fatal("the PIM picks the sending account itself; the notifier must not guess one")
	}
}

func TestNotifyWithoutRecipientIsUndeliveredAndSendsNothing(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	for _, tc := range []struct {
		name string
		ctx  context.Context
		own  OwnAddresses
		want string
	}{
		{"no own address", ownerCtx(), &fakeOwn{err: errors.New("no WhatsApp account is linked")}, "no recipient for whatsapp: no WhatsApp account is linked"},
		{"no identity", context.Background(), ownAddressBook(), "no recipient for whatsapp"},
		{"no address book", ownerCtx(), nil, "set AURA_SCHEDULER_NOTIFY_RECIPIENT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fss := &fakeSelfSend{}
			var buf bytes.Buffer
			n := &compositeNotifier{resolver: fss, own: tc.own, out: &buf}
			err := n.Notify(tc.ctx, RouteWhatsApp, "drink water")
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

func TestNotifyMCPFailureFallsBackToStdoutAndSignalsUndelivered(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "x")
	fss := &fakeSelfSend{failWith: errors.New("mcp down")}
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: fss, out: &buf}

	err := n.Notify(context.Background(), RouteWhatsApp, "deliver me")
	if err == nil || !strings.Contains(err.Error(), "send_message send: mcp down") {
		t.Fatalf("a failed MCP self-send must return its error (notification-undelivered → D-22 bound retry), got %v", err)
	}
	if !strings.Contains(buf.String(), "deliver me") {
		t.Fatalf("expected the stdout fallback to carry the text, got %q", buf.String())
	}
}

func TestNotifyMissingMountFallsBackToStdout(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "x")
	fss := &fakeSelfSend{missing: map[string]bool{"recipe:whatsapp send_message": true}}
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
	n := &compositeNotifier{resolver: fss, own: ownAddressBook()}

	for route, want := range map[NotifyRoute]string{RouteWhatsApp: "393331112222", RouteEmail: "owner@example.com"} {
		if got, err := n.Destination(ownerCtx(), route); err != nil || got != want {
			t.Fatalf("Destination(%s) = %q, %v; want %q", route, got, err, want)
		}
	}
	if len(fss.sent) != 0 {
		t.Fatal("Destination sent a message")
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

	unmounted := &compositeNotifier{resolver: &fakeSelfSend{missing: map[string]bool{"recipe:calendar calendar": true}}, own: ownAddressBook()}
	if _, err := unmounted.Destination(ownerCtx(), RouteEmail); err == nil || !strings.Contains(err.Error(), "no MCP tool mounted for route email") {
		t.Fatalf("Destination(email) with no PIM mounted = %v, want the missing-tool reason", err)
	}
}
