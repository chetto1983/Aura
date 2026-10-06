//go:build docker_integration

package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// passkeyChallengeFixture stands in for OpenAI's MFA page as it froze a real login on
// 2026-10-06 (prd.md §6): a passkey request starts on load and "Try another method" is the
// only way on. The button leads to the login's callback, as finishing sign-in would.
const passkeyChallengeFixture = `import https from 'node:https';
import { readFileSync } from 'node:fs';
const page = '<!doctype html><button id="other" style="position:absolute;left:540px;top:300px;width:200px;height:40px">Try another method</button><script>' +
  'window.outcome = "pending";' +
  'navigator.credentials.get({ publicKey: { challenge: new Uint8Array(32), rpId: "auth.openai.com", timeout: 300000,' +
  ' allowCredentials: [{ type: "public-key", id: new Uint8Array(32) }] } }).catch((e) => { window.outcome = e.name; });' +
  'const q = new URLSearchParams(location.search);' +
  'document.getElementById("other").onclick = () => {' +
  ' location = q.get("redirect_uri") + "?code=fixture-code&state=" + encodeURIComponent(q.get("state")); };</script>';
const server = https.createServer({ key: readFileSync('/tmp/passkey-fixture.key'), cert: readFileSync('/tmp/passkey-fixture.crt') }, (req, res) => {
  const url = new URL(req.url, 'https://auth.openai.com');
  res.setHeader('Cross-Origin-Opener-Policy', 'same-origin');
  if (url.pathname === '/api/accounts/authorize') res.writeHead(302, { Location: '/mfa-challenge?' + url.searchParams }).end();
  else if (url.pathname === '/mfa-challenge') res.writeHead(200, { 'Content-Type': 'text/html' }).end(page);
  else res.writeHead(404).end();
});
server.listen(8443, '127.0.0.1', () => process.stdout.write('ready\n'));
process.stdin.on('end', () => process.exit(0)).resume();
`

// TestChatGPTLoginSurvivesPasskeyChallenge is that freeze on the real image: the shipped login
// helper, agent-browser wrapper, Chromium and live-view relay, with the page pending on a
// passkey request, must still deliver the viewer's click and so the callback. Only the
// fixture's name resolution and TLS pin are added, through the wrapper's AGENT_BROWSER_ARGS.
func TestChatGPTLoginSurvivesPasskeyChallenge(t *testing.T) {
	ctx, router, h := liveBox(t)
	key, cert, pin := passkeyFixtureCertificate(t)
	for path, content := range map[string][]byte{
		"/tmp/passkey-fixture.key": key,
		"/tmp/passkey-fixture.crt": cert,
		"/tmp/passkey-fixture.mjs": []byte(passkeyChallengeFixture),
		"/tmp/chatgpt-browser.mjs": chatGPTBrowserScript,
	} {
		if err := router.WriteFile(ctx, h, path, content); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	startFixture(t, ctx, router, h)

	var random [12]byte
	_, _ = rand.Read(random[:])
	session := "chatgpt-" + hex.EncodeToString(random[:])
	env := []string{"AGENT_BROWSER_ARGS=--disable-blink-features=AutomationControlled," +
		"--host-resolver-rules=MAP auth.openai.com 127.0.0.1:8443," +
		"--ignore-certificate-errors-spki-list=" + pin}
	in, commands := io.Pipe()
	out, output := io.Pipe()
	handle, err := router.ExecStream(ctx, h, usersandbox.ExecRequest{
		Command: "node /tmp/chatgpt-browser.mjs " + session, Dir: "/workspace", Env: env,
	}, in, output)
	if err != nil {
		t.Fatalf("start login helper: %v", err)
	}
	login := newChatGPTBrowserSession(handle, commands, out, output)
	t.Cleanup(func() { _ = login.Close() })
	ready, err := login.next(ctx, "listening")
	if err != nil {
		t.Fatalf("login helper did not listen: %v", err)
	}
	authorize := url.URL{Scheme: "https", Host: "auth.openai.com", Path: "/api/accounts/authorize",
		RawQuery: url.Values{"redirect_uri": {ready.RedirectURI}, "state": {"fixture-state"}}.Encode()}
	if err = login.Navigate(ctx, authorize.String()); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// The click proves nothing unless the request is really pending: a page Chrome refused
	// WebAuthn to (a TLS error, say) would take the click with or without the fix.
	outcome := ""
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Env: env, Command: "agent-browser --session " + session +
			" --profile /tmp/.aura-chatgpt-profile-" + session + ` --restore-save never eval "window.outcome"`})
		if outcome = strings.TrimSpace(string(res.Stdout)); err == nil && outcome == `"pending"` {
			break
		}
	}
	if outcome != `"pending"` {
		t.Fatalf("passkey request outcome = %s, want it pending on the challenge page", outcome)
	}

	view := openLiveView(t, ctx, router, session)
	view.click(640, 320)
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	query, err := login.Callback(waitCtx)
	if err != nil {
		t.Fatalf("no callback after the relayed click on \"Try another method\": %v", err)
	}
	if query.Get("code") != "fixture-code" || query.Get("state") != "fixture-state" {
		t.Fatalf("callback query = %v, want the fixture's code and state", query)
	}
	view.close()
	if err = login.Close(); err != nil {
		t.Fatalf("login helper cleanup: %v", err)
	}
}

// passkeyFixtureCertificate returns a self-signed auth.openai.com certificate and the SPKI pin
// Chromium's --ignore-certificate-errors-spki-list takes for it.
func passkeyFixtureCertificate(t *testing.T) (keyPEM, certPEM []byte, pin string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("fixture key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "auth.openai.com"},
		DNSNames:     []string{"auth.openai.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("fixture certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("fixture key encoding: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("fixture certificate parse: %v", err)
	}
	sum := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		base64.StdEncoding.EncodeToString(sum[:])
}

// startFixture runs the challenge page in the box until the test ends.
func startFixture(t *testing.T, ctx context.Context, router *usersandbox.SandboxRouter, h usersandbox.BoxHandle) {
	t.Helper()
	in, stdin := io.Pipe()
	outR, outW := io.Pipe()
	fixture, err := router.ExecStream(ctx, h, usersandbox.ExecRequest{Command: "node /tmp/passkey-fixture.mjs"}, in, outW)
	if err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_, _ = fixture.Wait()
		_ = outW.Close()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			fixture.Kill()
			<-exited
		}
	})
	lines := bufio.NewReader(outR)
	if line, err := lines.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("fixture did not start: %q %v", line, err)
	}
	go func() { _, _ = io.Copy(io.Discard, lines) }()
}
