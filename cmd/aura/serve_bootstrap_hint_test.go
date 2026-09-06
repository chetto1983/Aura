package main

// serve_bootstrap_hint_test.go pins the one explanation that turns a misleading error into
// an actionable one. Measured 2026-09-06 on a live stack: a native `aura serve` failed to
// provision a bucket with "lookup garage on 8.8.8.8:53: no such host", which reads as a DNS
// fault. DNS was fine — Garage's admin API is compose-internal by deliberate design.

import (
	"errors"
	"strings"
	"testing"
)

func TestComposeOnlyAdminAPIHint(t *testing.T) {
	dnsErr := errors.New(`garageadmin: call /v2/CreateBucket: Post "http://garage:3903/v2/CreateBucket": dial tcp: lookup garage on 8.8.8.8:53: no such host`)

	t.Run("native process gets the explanation", func(t *testing.T) {
		t.Setenv("AURA_IN_CONTAINER", "")
		hint := composeOnlyAdminAPIHint(dnsErr)
		if hint == "" {
			t.Fatal("a native process must be told the admin API is compose-internal")
		}
		if !strings.Contains(hint, "AURA_GARAGE_ADMIN_ENDPOINT") {
			t.Fatalf("the hint must name the knob to change, got %q", hint)
		}
	})

	// Inside a container the same text means DNS really is broken, and a confident wrong
	// explanation is worse than none.
	t.Run("in-container stays silent", func(t *testing.T) {
		t.Setenv("AURA_IN_CONTAINER", "1")
		if hint := composeOnlyAdminAPIHint(dnsErr); hint != "" {
			t.Fatalf("in-container must not blame the posture, got %q", hint)
		}
	})

	t.Run("an unrelated failure stays silent", func(t *testing.T) {
		t.Setenv("AURA_IN_CONTAINER", "")
		if hint := composeOnlyAdminAPIHint(errors.New("garageadmin: 403 forbidden")); hint != "" {
			t.Fatalf("only a name-resolution failure earns this hint, got %q", hint)
		}
	})

	t.Run("no error, no hint", func(t *testing.T) {
		t.Setenv("AURA_IN_CONTAINER", "")
		if hint := composeOnlyAdminAPIHint(nil); hint != "" {
			t.Fatalf("nil error must produce no hint, got %q", hint)
		}
	})
}
