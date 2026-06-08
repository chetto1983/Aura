package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// probeStore extends fakeStore with a recording BotProbe so a handler test can
// assert getMe-validate behaviour + that the token is never echoed.
func newServerWithProbe(t *testing.T, probe BotProbe, store Store) *Server {
	t.Helper()
	var out bytes.Buffer
	return NewServer(Deps{
		Store:      store,
		Probe:      probe,
		Token:      "tok",
		IdentityID: "00000000-0000-0000-0000-000000000001",
		TokenOut:   &out,
		QROut:      io.Discard, // silence the onboarding ASCII QR in test output
	})
}

// doJSON posts a JSON body to a gated route with the valid token and returns the
// recorder.
func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	req := httptest.NewRequest(method, path+sep+"token=tok", &buf)
	rec := httptest.NewRecorder()
	srv.Mux().ServeHTTP(rec, req)
	return rec
}

// TestHandleTokenGetMeValidate proves /setup/token validates the bot token via
// the BotProbe (getMe seam) and returns {ok, bot_username} on success (VALIDATION
// UX-03 "/setup/token getMe validate").
func TestHandleTokenGetMeValidate(t *testing.T) {
	var probedToken string
	probe := func(_ context.Context, token string) (string, error) {
		probedToken = token
		return "aura_assistant_bot", nil
	}
	srv := newServerWithProbe(t, probe, &fakeStore{})
	rec := doJSON(t, srv, http.MethodPost, "/setup/token", TokenRequest{Token: "123:ABC-secret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || resp.BotUsername != "aura_assistant_bot" {
		t.Fatalf("got %+v, want {ok:true, bot_username:aura_assistant_bot}", resp)
	}
	if probedToken != "123:ABC-secret" {
		t.Fatalf("probe received %q, want the supplied bot token", probedToken)
	}
}

// TestHandleTokenInvalid proves an invalid bot token (getMe fails) returns a 4xx
// with a non-leaky message (the probe error — which could embed the token — is
// NOT surfaced verbatim).
func TestHandleTokenInvalid(t *testing.T) {
	probe := func(_ context.Context, token string) (string, error) {
		return "", errors.New("telegram getMe 401 for token " + token) // hostile: echoes the token
	}
	srv := newServerWithProbe(t, probe, &fakeStore{})
	rec := doJSON(t, srv, http.MethodPost, "/setup/token", TokenRequest{Token: "leaky-secret-token"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "leaky-secret-token") {
		t.Fatalf("response body leaked the bot token: %q", rec.Body.String())
	}
}

// TestBotTokenNeverLogged (T-13-07-BotTokenLeak) captures the slog output across
// a successful AND a failed /setup/token call and proves the bot token appears
// in NEITHER the logs NOR the response bodies.
func TestBotTokenNeverLogged(t *testing.T) {
	const secret = "987654:SUPER-SECRET-BOT-TOKEN"
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Success path.
	okSrv := newServerWithProbe(t, func(_ context.Context, _ string) (string, error) {
		return "bot", nil
	}, &fakeStore{})
	recOK := doJSON(t, okSrv, http.MethodPost, "/setup/token", TokenRequest{Token: secret})

	// Failure path (the handler logs a warning here).
	failSrv := newServerWithProbe(t, func(_ context.Context, token string) (string, error) {
		return "", errors.New("auth failed: " + token)
	}, &fakeStore{})
	recFail := doJSON(t, failSrv, http.MethodPost, "/setup/token", TokenRequest{Token: secret})

	if strings.Contains(logBuf.String(), secret) {
		t.Fatalf("bot token leaked into logs: %q", logBuf.String())
	}
	if strings.Contains(recOK.Body.String(), secret) || strings.Contains(recFail.Body.String(), secret) {
		t.Fatal("bot token leaked into a response body")
	}
}

// TestHandleOnboardLink proves /setup/onboard-link mints a pending row (TTL ~1h)
// and returns {deep_link, qr_svg:""} (VALIDATION UX-03 "/setup/onboard-link
// {deep_link,qr_svg}"); qr_svg is the empty string this phase (OQ4 deferral).
func TestHandleOnboardLink(t *testing.T) {
	store := &fakeStore{}
	srv := newServerWithProbe(t, func(_ context.Context, _ string) (string, error) {
		return "aura_bot", nil
	}, store)
	// A bot must be configured first (the onboard-link needs the bot username).
	if rec := doJSON(t, srv, http.MethodPost, "/setup/token", TokenRequest{Token: "t"}); rec.Code != http.StatusOK {
		t.Fatalf("setup/token: got %d", rec.Code)
	}

	rec := doJSON(t, srv, http.MethodPost, "/setup/onboard-link", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp OnboardLinkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.DeepLink, "https://t.me/aura_bot?start=") {
		t.Fatalf("deep_link %q is not t.me/<bot>?start=<token>", resp.DeepLink)
	}
	if resp.QRSVG != "" {
		t.Fatalf("qr_svg should be empty this phase (OQ4 deferral), got %q", resp.QRSVG)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("InsertPending called %d times, want 1", len(store.inserted))
	}
	p := store.inserted[0]
	// The deep-link start token IS the minted onboarding token.
	wantToken := strings.TrimPrefix(resp.DeepLink, "https://t.me/aura_bot?start=")
	if p.OnboardingToken != wantToken {
		t.Fatalf("pending token %q != deep-link start token %q", p.OnboardingToken, wantToken)
	}
	if p.IdentityID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("pending FK identity %q, want the local identity", p.IdentityID)
	}
	if ttl := time.Until(store.inserted[0].ExpiresAt); ttl <= 0 || ttl > onboardingTTL+time.Minute {
		t.Fatalf("pending TTL %v out of (0, ~1h] range", ttl)
	}
}

// TestHandleOnboardLinkNoBot proves onboard-link 409s when no bot has been
// configured (no /setup/token yet) — it cannot build a deep-link without the
// username.
func TestHandleOnboardLinkNoBot(t *testing.T) {
	srv := newServerWithProbe(t, func(_ context.Context, _ string) (string, error) { return "b", nil }, &fakeStore{})
	rec := doJSON(t, srv, http.MethodPost, "/setup/onboard-link", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 (bot not configured)", rec.Code)
	}
}

// TestHandleStatus proves /setup/status reports bot_configured + account_count
// (Store.CountAccounts) (VALIDATION UX-03 "/setup/status").
func TestHandleStatus(t *testing.T) {
	store := &fakeStore{count: 3}
	srv := newServerWithProbe(t, func(_ context.Context, _ string) (string, error) { return "bot", nil }, store)
	// Before any /setup/token: bot_configured=false.
	rec := doJSON(t, srv, http.MethodGet, "/setup/status", nil)
	var before StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &before)
	if before.BotConfigured {
		t.Fatal("bot_configured should be false before /setup/token")
	}
	if before.AccountCount != 3 {
		t.Fatalf("account_count %d, want 3", before.AccountCount)
	}
	// After /setup/token: bot_configured=true.
	doJSON(t, srv, http.MethodPost, "/setup/token", TokenRequest{Token: "t"})
	rec = doJSON(t, srv, http.MethodGet, "/setup/status", nil)
	var after StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if !after.BotConfigured {
		t.Fatal("bot_configured should be true after /setup/token")
	}
}
