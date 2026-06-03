// Boot wiring for the sandbox 2b egress forward proxy (08-10 Task 5). When an
// egress allowlist is configured, the per-conv session container routes its egress
// through a HOST forward proxy reachable at the bridge-GATEWAY IP (NOT container
// 127.0.0.1 — landmine #3 / 08-DECISIONS OQ1/A2). This file starts ONE boot-scoped
// proxy bound at that gateway addr and registers a goleak-clean shutdown.
//
// Egress env contract (operator/CI-exported; the live egress tests do NOT boot this
// composition root, so the operator starts `aura serve`/this proxy before them):
//
//	AURA_SANDBOX_NETWORK_ALLOW_HOSTS — CSV allowlist (empty ⇒ egressless, no proxy)
//	AURA_SANDBOX_EGRESS_NETWORK      — the docker bridge runArgv attaches to
//	                                   (aura_aura-sandbox-egressless — a plain
//	                                   `driver: bridge` with masquerade off, NOT
//	                                   internal:true, so the loopback `-p` published
//	                                   sidecar port IS reachable AND the gateway-
//	                                   routed proxy works — BLOCKER 2 resolved by
//	                                   fact, do NOT make the bridge internal)
//	AURA_SANDBOX_PROXY_ENV           — HTTP_PROXY/HTTPS_PROXY at <gw-ip>:<port>
//	                                   (semicolon-separated); the container's egress
//	                                   target AND the proxy's LISTEN addr (they must
//	                                   match — the gw-ip:port is parsed out here)
//	AURA_SANDBOX_SESSION_SECCOMP     — connect-allowing session seccomp profile path
//
// FLAG 3 (DNS-pin scope deviation, recorded as AR-08-02 in 08-SECURITY): a SINGLE
// boot proxy uses ONE convID-scoped DNS-pin cache for ALL conversations, weakening
// D-25 per-conversation rebind isolation. Accepted for v1 because the pin TTL is
// short and the allowlist is operator-opt-in; the single boot proxy matches the
// live tests' single allowlist. (The alternative — per-Acquire proxies — is
// deferred.) Task 6 must NOT flip 08-SECURITY threats_open while this deviation is
// undocumented.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox"
)

// Egress env-contract var names. Named constants (not inline) so the boot wiring
// reads as policy; mirror the integration-test env (network_integration_test.go).
const (
	envSandboxProxyEnv = "AURA_SANDBOX_PROXY_ENV"
	// proxyConvScope is the single-boot-proxy convID scope for the shared DNS-pin
	// cache (FLAG 3 / AR-08-02). All conversations share this one scope under the
	// boot proxy; per-Acquire proxies would key by the real conversation id.
	proxyConvScope = "sandbox-boot-proxy"
)

const sandboxProxyUsage = "usage: aura sandbox proxy --listen <host:port> --allow <csv> [--deny <csv>]"

// proxyArgs is the parsed `aura sandbox proxy` flag set.
type proxyArgs struct {
	listen string
	allow  string
	deny   string
}

// parseSandboxProxyArgs parses --listen/--allow/--deny for `aura sandbox proxy`.
// --listen is the one REQUIRED flag (the gateway host:port the session containers
// reach across the egress bridge); a missing/blank --listen — or any unknown flag —
// is a usage error (exit 64) with a diagnostic on errOut, so the proxy never binds a
// wildcard/empty address. An empty --allow is valid (the egressless baseline-deny
// posture). Pure + io.Writer-injected so the flag contract is unit-testable without
// binding a socket (mirrors parseSince).
func parseSandboxProxyArgs(args []string, errOut io.Writer) (proxyArgs, int) {
	fs := flag.NewFlagSet("sandbox proxy", flag.ContinueOnError)
	fs.SetOutput(errOut)
	listen := fs.String("listen", "", "host CONNECT proxy listen address (e.g. 172.20.0.1:18999)")
	allow := fs.String("allow", "", "comma-separated egress allowlist (e.g. pypi.org,files.pythonhosted.org)")
	deny := fs.String("deny", "", "optional comma-separated egress denylist")
	if err := fs.Parse(args); err != nil {
		return proxyArgs{}, exitUsage
	}
	if strings.TrimSpace(*listen) == "" {
		_, _ = fmt.Fprintln(errOut, sandboxProxyUsage)
		return proxyArgs{}, exitUsage
	}
	return proxyArgs{listen: *listen, allow: *allow, deny: *deny}, 0
}

// runSandboxProxy implements `aura sandbox proxy --listen <host:port> --allow <csv>
// [--deny <csv>]`: a standalone foreground CONNECT forward proxy for the session
// egress allowlist. It is the operator/CI entrypoint the boot wiring
// (startSandboxProxy) could not provide — the session containers reach it across the
// egress bridge at the gateway IP. It wraps the SAME sandbox.NewSessionProxy + Serve
// the boot path uses (no duplicate policy/guard logic), reading the DNS-pin TTL +
// privacy mode from LoadDB (no LLM key needed, mirroring runDB). Serve binds at
// --listen, then runs until SIGINT/SIGTERM (CI tears it down with a signal). It adds
// NO new trust boundary: the allowlist + resolve-then-pin + no-MITM posture is
// identical to the boot proxy (AR-08-02).
func runSandboxProxy(args []string) {
	pa, code := parseSandboxProxyArgs(args, os.Stderr)
	if code != 0 {
		os.Exit(code)
	}

	cfg := config.LoadDB()
	// Mirror startSandboxProxy / D-10: an allowlist under local-only privacy is an
	// operator misconfiguration, surfaced loud rather than silently bound.
	if cfg.PrivacyMode == "local-only" && pa.allow != "" {
		fmt.Fprintln(os.Stderr, "aura sandbox proxy: egress allowlist set under AURA_PRIVACY_MODE=local-only (D-10 fail-fast)")
		os.Exit(exitUsage)
	}

	proxy, err := sandbox.NewSessionProxy(pa.listen, pa.allow, pa.deny, proxyConvScope, cfg.WebDNSPinTTLSec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura sandbox proxy:", err)
		os.Exit(exitInfra)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "aura sandbox proxy: listening at %s (allow=%q)\n", pa.listen, pa.allow)
	if err := proxy.Serve(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "aura sandbox proxy:", err)
		os.Exit(exitInfra)
	}
}

// parseSandboxProxyEnv splits the semicolon-separated HTTP_PROXY/HTTPS_PROXY env
// (e.g. "HTTP_PROXY=http://172.20.0.1:8881;HTTPS_PROXY=http://172.20.0.1:8881")
// into the []string the container receives as --env entries. Empty ⇒ nil.
func parseSandboxProxyEnv(raw string) []string {
	var out []string
	for _, kv := range strings.Split(raw, ";") {
		if kv = strings.TrimSpace(kv); kv != "" {
			out = append(out, kv)
		}
	}
	return out
}

// proxyListenAddr extracts the <gw-ip>:<port> the container's HTTP_PROXY points at
// — that exact addr is the proxy's LISTEN addr (they must match). It reads the first
// HTTP_PROXY/HTTPS_PROXY entry's http://host:port and returns host:port. An empty or
// malformed proxy env yields an error so a misconfigured egress posture fails loud
// rather than binding the wrong addr.
func proxyListenAddr(proxyEnv []string) (string, error) {
	for _, kv := range proxyEnv {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(kv[:eq]))
		if key != "HTTP_PROXY" && key != "HTTPS_PROXY" {
			continue
		}
		val := strings.TrimSpace(kv[eq+1:])
		val = strings.TrimPrefix(val, "http://")
		val = strings.TrimPrefix(val, "https://")
		val = strings.TrimSuffix(val, "/")
		if val != "" {
			return val, nil
		}
	}
	return "", fmt.Errorf("%s has no HTTP_PROXY/HTTPS_PROXY http://host:port entry", envSandboxProxyEnv)
}

// startSandboxProxy starts the boot egress proxy when an allowlist is configured.
// An empty allowlist starts NO proxy (the 2a egressless posture). A local-only
// privacy mode WITH a non-empty allowlist is an operator misconfiguration surfaced
// as a boot error (mirrors the D-10 Acquire fail-fast). The Serve loop runs on the
// boot ctx in a goroutine and shuts down when ctx is cancelled (goleak-clean).
func startSandboxProxy(ctx context.Context, cfg *config.Config) error {
	if cfg.SandboxNetworkAllowHosts == "" {
		return nil // egressless posture — no proxy
	}
	if cfg.PrivacyMode == "local-only" {
		return fmt.Errorf("sandbox egress allowlist set under AURA_PRIVACY_MODE=local-only — " +
			"remove the allowlist or the privacy mode (D-10 fail-fast)")
	}
	proxyEnv := parseSandboxProxyEnv(os.Getenv(envSandboxProxyEnv))
	listenAddr, err := proxyListenAddr(proxyEnv)
	if err != nil {
		return fmt.Errorf("sandbox egress proxy: %w", err)
	}
	proxy, err := sandbox.NewSessionProxy(listenAddr, cfg.SandboxNetworkAllowHosts, "", proxyConvScope, cfg.WebDNSPinTTLSec)
	if err != nil {
		return fmt.Errorf("sandbox egress proxy: %w", err)
	}
	go func() {
		// Serve returns context.Canceled on the boot-ctx teardown (clean stop); any
		// other error is logged but never crashes the REPL.
		if sErr := proxy.Serve(ctx); sErr != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "warn: sandbox egress proxy stopped:", sErr)
		}
	}()
	return nil
}
