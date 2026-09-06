package main

// auth_jwks_report_test.go covers what the sidecar SAYS when it cannot reach the
// authorization server's JWKS. Measured 2026-09-06: with Aura running natively rather than
// inside compose, the default jwks_url is a compose service name that does not resolve, so
// every `initialize` answered "Internal Server Error" against a completely empty log. The
// failure was fully diagnosable from inside the process and was simply never stated.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// unreachableIssuer names a JWKS URL nothing answers, which is what a compose DNS name
// looks like from outside compose.
func unreachableIssuer() trustedIssuer {
	return trustedIssuer{Issuer: "http://aura:9080", JWKSURL: "http://aura:9080/oauth/jwks"}
}

func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(prev) }
}

// TestUnreachableJWKSIsReportedOnce is the whole point: say it, and say it once. A server
// called on every agent turn that logged each failure would answer one misconfiguration
// with a log flood — the same shape of defect as the one being fixed.
func TestUnreachableJWKSIsReportedOnce(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	issuer := unreachableIssuer()
	verifier := newArcadeTokenVerifier(
		oauthResourceConfig{Issuers: []trustedIssuer{issuer}, Resource: "http://memory.example/mcp/"},
		&http.Client{Timeout: time.Second},
	)
	frozen := time.Now()
	verifier.now = func() time.Time { return frozen }

	for range 5 {
		if _, err := verifier.keySet(context.Background(), issuer, true); err == nil {
			t.Fatal("an unreachable JWKS must fail, not succeed silently")
		}
	}

	lines := reportedJWKSFailures(t, buf)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 report for 5 identical failures, got %d:\n%s", len(lines), buf.String())
	}
	// The report has to carry the URL: the URL IS the bug in the case this was written for.
	if got := lines[0]["jwks_url"]; got != issuer.JWKSURL {
		t.Fatalf("report does not name the JWKS URL: %v", lines[0])
	}
	if lines[0]["level"] != "ERROR" {
		t.Fatalf("an unverifiable server is an error, got level %v", lines[0]["level"])
	}
}

// TestJWKSFailureIsRestatedAfterTheQuietWindow: suppression must not mean silence forever.
// An outage that has run for an hour still has to be visible in the last minute of logs.
func TestJWKSFailureIsRestatedAfterTheQuietWindow(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	issuer := unreachableIssuer()
	verifier := newArcadeTokenVerifier(
		oauthResourceConfig{Issuers: []trustedIssuer{issuer}, Resource: "http://memory.example/mcp/"},
		&http.Client{Timeout: time.Second},
	)
	now := time.Now()
	verifier.now = func() time.Time { return now }

	_, _ = verifier.keySet(context.Background(), issuer, true)
	now = now.Add(jwksFailureRestateAfter + time.Second)
	_, _ = verifier.keySet(context.Background(), issuer, true)

	if lines := reportedJWKSFailures(t, buf); len(lines) != 2 {
		t.Fatalf("want the failure restated after the quiet window, got %d reports:\n%s", len(lines), buf.String())
	}
}

func reportedJWKSFailures(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for raw := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not JSON: %q", raw)
		}
		if msg, _ := line["msg"].(string); strings.Contains(msg, "JWKS is unreachable") {
			out = append(out, line)
		}
	}
	return out
}
