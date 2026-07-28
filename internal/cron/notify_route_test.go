package cron

import (
	"strings"
	"testing"
)

// TestValidNotifyRoute pins the enum every validating caller shares (the task tool, the
// cockpit scheduler API, the CLI flag). The empty string is VALID — it means "use the
// default route" — and that asymmetry is exactly what a re-listed enum at a call site
// gets wrong.
func TestValidNotifyRoute(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"", "whatsapp", "email", "stdout", "telegram"} {
		if !ValidNotifyRoute(ok) {
			t.Errorf("ValidNotifyRoute(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"Telegram", "telegram ", "slack", "sms", "stdout;drop", "whatsap"} {
		if ValidNotifyRoute(bad) {
			t.Errorf("ValidNotifyRoute(%q) = true, want false", bad)
		}
	}
}

// TestOriginPreferringSplitsRouteIntent guards the distinction the origin gate turns on:
// whatsapp/email are alternate DESTINATIONS that pre-empt the origin channel (R7), while
// "", stdout and telegram all mean "deliver where this came from".
func TestOriginPreferringSplitsRouteIntent(t *testing.T) {
	t.Parallel()
	for _, defers := range []string{"", "stdout", "telegram"} {
		if !originPreferring(defers) {
			t.Errorf("originPreferring(%q) = false, want true (must defer to the origin channel)", defers)
		}
	}
	for _, preempts := range []string{"whatsapp", "email"} {
		if originPreferring(preempts) {
			t.Errorf("originPreferring(%q) = true, want false (R7: a deliberate alternate channel)", preempts)
		}
	}
}

// TestSendViaMCPRefusesTelegram: reaching the composite with route=telegram means the
// origin gate already declined, so the honest answer is an error (Notify then writes the
// stdout fallback AND reports undelivered, the D-22 retry contract). Silently mapping it
// onto the WhatsApp self-send would deliver the operator's reminder to the wrong service.
func TestSendViaMCPRefusesTelegram(t *testing.T) {
	t.Parallel()
	n := &compositeNotifier{}
	err := n.sendViaMCP(t.Context(), RouteTelegram, "someone", "text")
	if err == nil {
		t.Fatal("sendViaMCP(telegram) = nil, want an undelivered error")
	}
	if got := err.Error(); !strings.Contains(got, "no MCP self-send") {
		t.Errorf("error = %q, want it to say telegram has no MCP self-send", got)
	}
}
