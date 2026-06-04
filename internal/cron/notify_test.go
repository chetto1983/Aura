package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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

func TestNotifyWhatsAppResolvesSendMessage(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "393331112222")
	fss := &fakeSelfSend{}
	n := NewNotifier(fss)
	if err := n.Notify(context.Background(), RouteWhatsApp, "", "borsa update"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "send_message" {
		t.Fatalf("whatsapp must resolve send_message, got %v", fss.resolved)
	}
	var got map[string]string
	if err := json.Unmarshal(fss.sent[0], &got); err != nil {
		t.Fatal(err)
	}
	if got["recipient"] != "393331112222" || got["message"] != "borsa update" {
		t.Fatalf("unexpected send_message args: %v", got)
	}
}

func TestNotifyEmailResolvesSendEmail(t *testing.T) {
	t.Parallel()
	fss := &fakeSelfSend{}
	n := NewNotifier(fss)
	if err := n.Notify(context.Background(), RouteEmail, "a@b.com", "report"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "send_email" {
		t.Fatalf("email must resolve send_email, got %v", fss.resolved)
	}
	var got map[string]string
	_ = json.Unmarshal(fss.sent[0], &got)
	if got["to"] != "a@b.com" || got["body"] != "report" {
		t.Fatalf("unexpected send_email args: %v", got)
	}
}

func TestNotifyMCPFailureFallsBackToStdoutAndSignalsUndelivered(t *testing.T) {
	t.Parallel()
	fss := &fakeSelfSend{failWith: errors.New("mcp down")}
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: fss, out: &buf}

	err := n.Notify(context.Background(), RouteWhatsApp, "x", "deliver me")
	if err == nil {
		t.Fatal("a failed MCP self-send must return a non-nil error (notification-undelivered → D-22 bound retry)")
	}
	if !strings.Contains(buf.String(), "deliver me") {
		t.Fatalf("expected the stdout fallback to carry the text, got %q", buf.String())
	}
}

func TestNotifyMissingMountFallsBackToStdout(t *testing.T) {
	t.Parallel()
	fss := &fakeSelfSend{missing: map[string]bool{"send_message": true}}
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: fss, out: &buf}
	if err := n.Notify(context.Background(), RouteWhatsApp, "x", "no mount"); err == nil {
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
	if err := n.Notify(context.Background(), RouteStdout, "ops", "tick"); err != nil {
		t.Fatalf("stdout route must always deliver, got %v", err)
	}
	if !strings.Contains(buf.String(), "tick") {
		t.Fatalf("stdout sink missing text: %q", buf.String())
	}
}

func TestNotifyDefaultRouteFromEnv(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_DEFAULT", "email")
	fss := &fakeSelfSend{}
	n := NewNotifier(fss)
	if err := n.Notify(context.Background(), "", "a@b.com", "via default"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(fss.resolved) != 1 || fss.resolved[0] != "send_email" {
		t.Fatalf("empty route should fall through to AURA_SCHEDULER_NOTIFY_DEFAULT=email, got %v", fss.resolved)
	}
}

func TestNotifyUnknownRouteDegradesToStdout(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n := &compositeNotifier{resolver: &fakeSelfSend{}, out: &buf}
	if err := n.Notify(context.Background(), NotifyRoute("carrier-pigeon"), "x", "garbled"); err != nil {
		t.Fatalf("an unknown route must degrade to stdout, got %v", err)
	}
	if !strings.Contains(buf.String(), "garbled") {
		t.Fatalf("unknown route should still write to stdout, got %q", buf.String())
	}
}
