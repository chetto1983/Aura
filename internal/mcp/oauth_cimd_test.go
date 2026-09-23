package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// cockpitCallback is where a cockpit-started flow must end up after the relay.
const cockpitCallback = "https://aura.example/api/governance/mcp/authorization/callback"

// oauthMCPFixture is a remote MCP server behind a real OAuth authorization server, both on
// one httptest listener. The authorization server's shape is the variable: ElevenLabs
// publishes CIMD support and no registration endpoint (measured 2026-09-23), Linear
// publishes both, Slack neither.
type oauthMCPFixture struct {
	base string

	mu         sync.Mutex
	challenge  string
	tokenForm  url.Values
	registered int
	issued     string
}

type asShape struct {
	cimd bool
	dcr  bool
}

func startOAuthMCPFixture(t *testing.T, shape asShape) *oauthMCPFixture {
	t.Helper()
	f := &oauthMCPFixture{}
	srv := newSDKFixtureServer(1, 0)
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureJSON(w, map[string]any{
			"resource":              f.base + "/mcp",
			"authorization_servers": []string{f.base},
			"scopes_supported":      []string{"text_to_speech"},
		})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		meta := map[string]any{
			"issuer":                                         f.base,
			"authorization_endpoint":                         f.base + "/authorize",
			"token_endpoint":                                 f.base + "/token",
			"response_types_supported":                       []string{"code"},
			"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":               []string{"S256"},
			"token_endpoint_auth_methods_supported":          []string{"none"},
			"authorization_response_iss_parameter_supported": true,
			"client_id_metadata_document_supported":          shape.cimd,
		}
		if shape.dcr {
			meta["registration_endpoint"] = f.base + "/register"
		}
		writeFixtureJSON(w, meta)
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var metadata map[string]any
		_ = json.NewDecoder(r.Body).Decode(&metadata)
		f.mu.Lock()
		f.registered++
		f.mu.Unlock()
		metadata["client_id"] = "dcr-client-1"
		w.WriteHeader(http.StatusCreated)
		writeFixtureJSON(w, metadata)
	})
	mux.HandleFunc("POST /token", f.serveToken)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		issued := f.issued
		f.mu.Unlock()
		if issued == "" || r.Header.Get("Authorization") != "Bearer "+issued {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+f.base+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewServer(mux)
	f.base = ts.URL
	t.Cleanup(func() {
		for session := range srv.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return f
}

// serveToken is the code exchange, checked the way a real authorization server checks it:
// the PKCE verifier must match the challenge the consent URL carried, and a client whose
// only auth method is "none" identifies itself in the body. ElevenLabs advertises
// none/client_secret_post/private_key_jwt and no client_secret_basic, so an HTTP Basic
// header is an unsupported method there. The SDK leaves a metadata-document client on
// x/oauth2's AuthStyleAutoDetect, which tries that header first and retries in the body
// only when refused — this refusal is what proves the retry carries the sign-in.
func (f *oauthMCPFixture) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		w.WriteHeader(http.StatusUnauthorized)
		writeFixtureJSON(w, map[string]string{"error": "invalid_client"})
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenForm = r.PostForm
	if r.PostForm.Get("code") != "code-1" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeFixtureJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	f.issued = "access-for-" + r.PostForm.Get("client_id")
	writeFixtureJSON(w, map[string]any{"access_token": f.issued, "token_type": "Bearer", "expires_in": 3600, "refresh_token": "r1"})
}

// observed reads what the authorization server saw. Under the lock: the server goroutines
// wrote it, and a loopback TCP round trip is no synchronization the race detector knows.
func (f *oauthMCPFixture) observed() (tokenForm url.Values, registered int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenForm, f.registered
}

func writeFixtureJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// consent plays the human and the relay: it records the consent URL and answers with the
// state exactly as it came back through the redirect.
func (f *oauthMCPFixture) consent(seen *url.Values) auth.AuthorizationCodeFetcher {
	return func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		parsed, err := url.Parse(args.URL)
		if err != nil {
			return nil, err
		}
		*seen = parsed.Query()
		f.mu.Lock()
		f.challenge = seen.Get("code_challenge")
		f.mu.Unlock()
		return &auth.AuthorizationResult{Code: "code-1", State: seen.Get("state"), Iss: f.base}, nil
	}
}

func (f *oauthMCPFixture) server(env ...string) ManagedServer {
	return ManagedServer{Type: ServerTypeStreamableHTTP, URL: f.base + "/mcp", Env: env}
}

func openWithConsent(t *testing.T, f *oauthMCPFixture, server ManagedServer, store GrantStore, fetcher auth.AuthorizationCodeFetcher) (*sdkmcp.ClientSession, error) {
	t.Helper()
	opts := SessionOptions{OAuth: OAuthOptions{Store: store, Fetcher: fetcher, RedirectURL: cockpitCallback}}
	session, err := OpenSDKSession(t.Context(), "remote", server, EgressPolicy{}, opts)
	if session != nil {
		t.Cleanup(func() { _ = session.Close() })
	}
	return session, err
}

// The ElevenLabs shape end to end: no registration endpoint, so the only client Aura can
// present is its metadata document, and the redirect must go through the relay because the
// document names no other.
func TestCIMDFallbackSignsInWhereTheServerOffersNoRegistration(t *testing.T) {
	t.Parallel()
	f := startOAuthMCPFixture(t, asShape{cimd: true})
	store := &rotatingGrantStore{}
	var consentURL url.Values

	session, err := openWithConsent(t, f, f.server(), store, f.consent(&consentURL))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if tools, err := session.ListTools(t.Context(), nil); err != nil || len(tools.Tools) != 1 {
		t.Fatalf("tools/list after sign-in = %v, %v", tools, err)
	}

	if got := consentURL.Get("client_id"); got != auraClientMetadataURL {
		t.Fatalf("consent client_id = %q, want the metadata document URL", got)
	}
	if got := consentURL.Get("redirect_uri"); got != auraOAuthRelayURL {
		t.Fatalf("consent redirect_uri = %q, want the relay", got)
	}
	nonce, packed, ok := strings.Cut(consentURL.Get("state"), ".")
	if !ok || nonce == "" {
		t.Fatalf("state %q carries no callback for the relay", consentURL.Get("state"))
	}
	if callback, _ := base64.RawURLEncoding.DecodeString(packed); string(callback) != cockpitCallback {
		t.Fatalf("relay would forward to %q, want %q", callback, cockpitCallback)
	}
	tokenForm, _ := f.observed()
	if tokenForm.Get("client_id") != auraClientMetadataURL || tokenForm.Get("redirect_uri") != auraOAuthRelayURL {
		t.Fatalf("token request = %v; client_id and redirect_uri must repeat the consent's", tokenForm)
	}
	grant, err := store.Load(t.Context(), "remote")
	if err != nil || grant.AccessToken != "access-for-"+auraClientMetadataURL {
		t.Fatalf("stored grant = %+v, %v; want the token issued to the metadata document", grant, err)
	}
	rc, err := decodeResolvedClient(grant.ClientInfo)
	if err != nil || rc.ClientID != auraClientMetadataURL {
		t.Fatalf("stored client = %+v, %v; a refresh must present the same client_id", rc, err)
	}
}

// Linear publishes CIMD support AND a registration endpoint, and signs in today through
// dynamic registration on the caller's own redirect. The fallback must not touch that.
func TestServerWithDynamicRegistrationKeepsItsSignIn(t *testing.T) {
	t.Parallel()
	f := startOAuthMCPFixture(t, asShape{cimd: true, dcr: true})
	var consentURL url.Values

	if _, err := openWithConsent(t, f, f.server(), &rotatingGrantStore{}, f.consent(&consentURL)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, registered := f.observed(); registered != 1 || consentURL.Get("client_id") != "dcr-client-1" {
		t.Fatalf("registered=%d client_id=%q, want one dynamic registration", registered, consentURL.Get("client_id"))
	}
	if consentURL.Get("redirect_uri") != cockpitCallback {
		t.Fatalf("redirect_uri = %q, want the caller's own, not the relay", consentURL.Get("redirect_uri"))
	}
	if strings.Contains(consentURL.Get("state"), ".") {
		t.Fatalf("state %q was packed for a relay that is not in use", consentURL.Get("state"))
	}
}

// An operator who registered a client by hand has said which client Aura is; the MCP spec
// puts pre-registration first, ahead of any metadata document.
func TestPreregisteredClientWinsOverTheMetadataDocument(t *testing.T) {
	t.Parallel()
	f := startOAuthMCPFixture(t, asShape{cimd: true})
	var consentURL url.Values

	if _, err := openWithConsent(t, f, f.server("MCP_OAUTH_CLIENT_ID=A1"), &rotatingGrantStore{}, f.consent(&consentURL)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if consentURL.Get("client_id") != "A1" || consentURL.Get("redirect_uri") != cockpitCallback {
		t.Fatalf("consent = %v, want the pre-registered client on the caller's redirect", consentURL)
	}
}

// A boot mount has no human. Reaching the metadata-document path must not change that: it
// still refuses with the instruction to authorize, and is not retried.
func TestUnattendedCIMDMountAsksForAuthorization(t *testing.T) {
	t.Parallel()
	f := startOAuthMCPFixture(t, asShape{cimd: true})

	_, err := OpenSDKSession(t.Context(), "remote", f.server(), EgressPolicy{}, SessionOptions{})
	if !errors.Is(err, ErrOAuthAuthorizationRequired) {
		t.Fatalf("err = %v, want ErrOAuthAuthorizationRequired", err)
	}
	if IsTransportError(err) {
		t.Fatal("a mount waiting for a human is being retried")
	}
}

// Slack's shape: neither registration nor a metadata document. Nothing Aura holds can sign
// in, and the error must stay the permanent one it was before the fallback existed.
func TestNoRegistrationMethodAtAllStaysPermanent(t *testing.T) {
	t.Parallel()
	f := startOAuthMCPFixture(t, asShape{})
	var consentURL url.Values

	_, err := openWithConsent(t, f, f.server(), &rotatingGrantStore{}, f.consent(&consentURL))
	if !isNoRegistrationMethod(err) {
		t.Fatalf("err = %v, want the SDK's no-registration error", err)
	}
	if IsTransportError(err) {
		t.Fatal("an unregisterable server is being retried")
	}
}

func TestRelayedFetcherCarriesTheCallbackInState(t *testing.T) {
	t.Parallel()
	var sent string
	inner := func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		sent = args.URL
		return &auth.AuthorizationResult{Code: "c", State: mustQuery(t, args.URL).Get("state"), Iss: "i"}, nil
	}
	got, err := relayedFetcher(cockpitCallback, inner)(t.Context(), &auth.AuthorizationArgs{URL: "https://as.example/authorize?state=S1&scope=a"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	want := "S1." + base64.RawURLEncoding.EncodeToString([]byte(cockpitCallback))
	if state := mustQuery(t, sent).Get("state"); state != want {
		t.Fatalf("sent state = %q, want %q", state, want)
	}
	if got.State != "S1" || got.Code != "c" || got.Iss != "i" {
		t.Fatalf("result = %+v, want the SDK's own state back with code and iss intact", got)
	}
}

// A state the relay did not produce is handed to the SDK untouched, so the SDK's own
// comparison is what refuses it.
func TestRelayedFetcherLeavesAForeignStateForTheSDKToRefuse(t *testing.T) {
	t.Parallel()
	inner := func(context.Context, *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		return &auth.AuthorizationResult{Code: "c", State: "forged"}, nil
	}
	got, err := relayedFetcher(cockpitCallback, inner)(t.Context(), &auth.AuthorizationArgs{URL: "https://as.example/authorize?state=S1"})
	if err != nil || got.State != "forged" {
		t.Fatalf("result = %+v, %v; want the foreign state passed through", got, err)
	}
}

func TestRelayedFetcherPassesRefusalsThrough(t *testing.T) {
	t.Parallel()
	refused := errors.New("access_denied")
	inner := func(context.Context, *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) { return nil, refused }
	if _, err := relayedFetcher(cockpitCallback, inner)(t.Context(), &auth.AuthorizationArgs{URL: "https://as.example/authorize?state=S1"}); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the inner refusal", err)
	}
}

type stubOAuthHandler struct {
	source oauth2.TokenSource
	err    error
	calls  int
}

func (s *stubOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return s.source, nil
}

func (s *stubOAuthHandler) Authorize(context.Context, *http.Request, *http.Response) error {
	s.calls++
	return s.err
}

func tokenOf(t *testing.T, h auth.OAuthHandler) string {
	t.Helper()
	source, err := h.TokenSource(t.Context())
	if err != nil || source == nil {
		t.Fatalf("TokenSource = %v, %v", source, err)
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	return tok.AccessToken
}

func staticSource(token string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
}

func TestRegistrationFallbackStaysOnThePrimaryWhenItWorks(t *testing.T) {
	t.Parallel()
	primary := &stubOAuthHandler{source: staticSource("primary")}
	fallback := &stubOAuthHandler{source: staticSource("fallback")}
	h := newRegistrationFallback(primary, fallback)

	if err := h.Authorize(t.Context(), nil, nil); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if fallback.calls != 0 || tokenOf(t, h) != "primary" {
		t.Fatalf("fallback calls=%d token=%q, want the primary alone", fallback.calls, tokenOf(t, h))
	}
}

func TestRegistrationFallbackReturnsOtherPrimaryErrors(t *testing.T) {
	t.Parallel()
	denied := errors.New("token exchange failed: invalid_grant")
	primary := &stubOAuthHandler{err: denied}
	fallback := &stubOAuthHandler{}
	h := newRegistrationFallback(primary, fallback)

	if err := h.Authorize(t.Context(), nil, nil); !errors.Is(err, denied) || fallback.calls != 0 {
		t.Fatalf("err=%v fallback calls=%d, want the primary's error and no fallback", err, fallback.calls)
	}
}

// Once signed in by metadata document, later authorizations stay there: a 403 the primary
// would answer with "retry" must not hand the session back to a handler holding no token.
func TestRegistrationFallbackKeepsTheFallbackOnceItSignedIn(t *testing.T) {
	t.Parallel()
	primary := &stubOAuthHandler{err: errors.New(sdkNoRegistrationMethod + " by the authorization server")}
	fallback := &stubOAuthHandler{source: staticSource("fallback")}
	h := newRegistrationFallback(primary, fallback)

	for range 2 {
		if err := h.Authorize(t.Context(), nil, nil); err != nil {
			t.Fatalf("Authorize: %v", err)
		}
	}
	if primary.calls != 1 || fallback.calls != 2 || tokenOf(t, h) != "fallback" {
		t.Fatalf("primary=%d fallback=%d token=%q", primary.calls, fallback.calls, tokenOf(t, h))
	}
}
