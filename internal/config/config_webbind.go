package config

// config_webbind.go carries the cockpit listener's boot policy, split out of
// config.go on touch to keep that file under the 600-LOC cap. It is the one
// decision in this package that can refuse a boot, so it reads better with its
// own file than buried between two loaders.

import (
	"fmt"
	"net"
)

// GuardWebBind is the WEB-02 fail-fast boot policy for the cockpit listener (D-05).
// A loopback bind always boots with no credential, exactly as before; a non-loopback
// bind boots ONLY when EITHER Authula web auth is configured OR trustProxy is true
// (the operator vouches a reverse proxy terminates auth).
// A non-loopback bind with neither credential returns an actionable error so the daemon
// refuses to silently expose an unauthenticated surface. It is a pure function — total
// (no panic path) and table-test-friendly — mirroring Validate's "config: …" posture.
//
// Wildcards (0.0.0.0, ::, [::]) are NOT special-cased: net.ParseIP(...).IsLoopback()
// returns false for them, so they fall through to the gated branch, which is correct.
func GuardWebBind(bind string, authConfigured bool, trustProxy bool) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind // tolerate a bare host with no port
	}
	ip := net.ParseIP(host)
	isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if isLoopback {
		return nil // loopback always bootable, exactly as before (D-05)
	}
	if authConfigured || trustProxy {
		return nil // unlocked by either credential (D-05)
	}
	return fmt.Errorf("config: AURA_AGUI_BIND=%q is non-loopback but web auth is not configured; "+
		"set AURA_AUTHULA_SECRET with AURA_AUTHULA_DATABASE_URL or AURA_DB_URL, set "+
		"AURA_WEB_TRUST_PROXY=true (a reverse proxy terminates auth), or bind a loopback address", bind)
}
