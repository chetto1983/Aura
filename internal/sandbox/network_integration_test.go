//go:build sandbox_integration && db_integration

// Live egress-allowlist tier (ROADMAP CAP-02 criterion 4a/4b) + the HIGHEST landmine
// #3 live reachability spike (A2, RESEARCH Pitfall 1). It builds a live SessionManager
// with the EGRESS posture — a non-empty allowlist (pypi.org), the connect-allowing
// session seccomp variant, the egress bridge, and the HTTP(S)_PROXY env pointing at the
// host forward proxy at the BRIDGE GATEWAY IP (NOT container 127.0.0.1) — then proves:
//
//	4a (TestNetwork_PyPIAllowed): a live `pip install` to pypi.org SUCCEEDS through the
//	    proxy. THIS IS THE LANDMINE-3 REACHABILITY SPIKE: if the session container cannot
//	    reach the host proxy at the bridge gateway (the 2a egressless bridge + connect-
//	    denied seccomp would block it), pip hangs/refuses and THIS test fails — proving
//	    the bridge/seccomp posture wrong. The fix is the posture (08-08), never the test.
//	4b (TestNetwork_NonAllowlistRefused): in the SAME egress posture, reaching a
//	    non-allowlisted host (1.1.1.1 / example.com) is REFUSED by the deny-wins proxy
//	    (baseline-deny — only pypi.org is allowlisted).
//
// Requires the full live stack (BOTH tags) + the egress wiring env (08-08):
//
//	AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org,files.pythonhosted.org
//	AURA_SANDBOX_EGRESS_NETWORK=<egress bridge name>
//	AURA_SANDBOX_PROXY_ENV="HTTP_PROXY=http://<gw-ip>:<port>;HTTPS_PROXY=http://<gw-ip>:<port>"
//	AURA_SANDBOX_SESSION_SECCOMP=<connect-allowing session profile path>
//
// No-skip-as-green: egressEnvOrSkip t.Fatals under $CI when the egress wiring is unset
// (a skipped egress tier would falsely green the single highest-risk criterion).
package sandbox

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// egressEnvOrSkip resolves a required egress-wiring var. Under $CI an unset var
// t.Fatals (no-skip-as-green — criterion 4 is the highest-risk one and a skip here is
// the most dangerous false green); locally it skips so a dev without the egress bridge
// is not blocked.
func egressEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	if v := os.Getenv(key); v != "" {
		return v
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("live egress tier requires %s, unset under CI — a skipped egress test must not "+
			"pass as green; wire the proxy/bridge env in sandbox.yml (landmine #3, criterion 4)", key)
	}
	t.Skipf("live egress tier requires %s + the host proxy reachable at the bridge gateway", key)
	return ""
}

// egressManager builds a live SessionManager in the egress posture from the wiring env.
// The proxy must already be running at the bridge gateway (08-08 composition wires it);
// this manager only docker-runs the session container with the egress bridge + proxy env
// + connect-allowing seccomp so user code routes egress through the host proxy.
func egressManager(t *testing.T, allow string) (*SessionManager, *pgxpool.Pool) {
	t.Helper()
	pool := migratedPool(t)
	egressNet := egressEnvOrSkip(t, "AURA_SANDBOX_EGRESS_NETWORK")
	proxyEnvRaw := egressEnvOrSkip(t, "AURA_SANDBOX_PROXY_ENV")
	// The connect-allowing session seccomp variant is required so the container can
	// connect(2) to the host proxy at all (landmine #3). liveManager reads it via
	// sessionSeccomp(AURA_SANDBOX_SESSION_SECCOMP); we assert it is set here so the
	// egress posture fails loud (not silently egressless) if the operator forgot it.
	_ = egressEnvOrSkip(t, "AURA_SANDBOX_SESSION_SECCOMP")

	var proxyEnv []string
	for _, kv := range strings.Split(proxyEnvRaw, ";") {
		if kv = strings.TrimSpace(kv); kv != "" {
			proxyEnv = append(proxyEnv, kv)
		}
	}
	m, _ := liveManager(t, pool, 1800*time.Second, time.Hour, allow, egressNet, proxyEnv)
	return m, pool
}

// TestNetwork_PyPIAllowed is ROADMAP CAP-02 criterion 4a AND the landmine-3 live
// reachability spike: a `pip install` to pypi.org succeeds through the host proxy
// reached at the bridge gateway. A connect-denied seccomp profile or an unreachable
// proxy makes pip fail here — that failure is the signal the posture is wrong (fix
// the posture, not the test).
func TestNetwork_PyPIAllowed(t *testing.T) {
	allow := egressEnvOrSkip(t, "AURA_SANDBOX_NETWORK_ALLOW_HOSTS")
	m, pool := egressManager(t, allow)
	runner := integrationRunner(t)
	ctx := ctxT(t)

	convID := newConversationRow(t, pool).String()
	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		// A privacy-mode fail-fast or cap error is a real failure here, not a skip.
		t.Fatalf("Acquire (egress posture): %v", err)
	}
	defer m.Release(sess)

	// A small, dependency-free pure-python wheel keeps the install fast; the point is
	// that the proxy route to pypi works at all (reachability), not the package.
	res, err := runner.RunShellSession(ctx, convID,
		"pip install --no-cache-dir --target /workspace/site --disable-pip-version-check 'six' && python -c 'import sys; sys.path.insert(0,\"/workspace/site\"); import six; print(\"OK\", six.__name__)'", 120)
	if err != nil {
		t.Fatalf("pip install to pypi (landmine-3 reachability spike) errored: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("LANDMINE-3 SPIKE FAILED: pip install to pypi did not succeed through the proxy "+
			"(bridge-gateway unreachable or connect seccomp-denied — fix the posture, not the test): "+
			"exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "OK six") {
		t.Fatalf("pip-installed package must import, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

// TestNetwork_NonAllowlistRefused is ROADMAP CAP-02 criterion 4b: in the SAME egress
// posture (only pypi.org allowlisted), reaching a non-allowlisted host is refused by
// the deny-wins proxy (baseline-deny). A successful connect to 1.1.1.1 / example.com
// is an allowlist bypass (T-08-06-INFO-EXFIL) and fails the test.
func TestNetwork_NonAllowlistRefused(t *testing.T) {
	allow := egressEnvOrSkip(t, "AURA_SANDBOX_NETWORK_ALLOW_HOSTS")
	m, pool := egressManager(t, allow)
	runner := integrationRunner(t)
	ctx := ctxT(t)

	convID := newConversationRow(t, pool).String()
	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		t.Fatalf("Acquire (egress posture): %v", err)
	}
	defer m.Release(sess)

	// urlopen to a non-allowlisted host must be refused by the proxy (407/403/connection
	// refused), surfacing as a Python exception → non-zero exit. A clean exit 0 with a
	// fetched body is a bypass.
	code := `import urllib.request
try:
    urllib.request.urlopen("https://example.com", timeout=15).read()
    print("FETCHED")
except Exception as e:
    print("REFUSED", type(e).__name__)`
	res, err := runner.RunPythonSession(ctx, convID, code, 30)
	if err != nil {
		t.Fatalf("non-allowlist probe errored: %v", err)
	}
	if strings.Contains(res.Stdout, "FETCHED") {
		t.Fatalf("ALLOWLIST BYPASS: non-allowlisted host was reachable in the egress posture "+
			"(only pypi.org allowed) — stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "REFUSED") {
		t.Fatalf("expected the non-allowlisted fetch to be REFUSED, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}
