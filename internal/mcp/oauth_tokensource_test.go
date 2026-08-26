package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// oauth_tokensource_test.go covers SURVIVAL: what is written when an authorization
// completes, what is read back at the next mount, and the states where the honest answer
// is "send the human to a browser again" rather than an error.

// fakeStore is a GrantStore with no Postgres behind it. Everything in this file that
// asserts on restore or persistence runs against it, because the behaviour worth pinning
// — what gets written, when, and what happens when the write fails — is decided in this
// package, not in the database.
type fakeStore struct {
	grant   mcpoauth.Grant
	loadErr error
	saved   []mcpoauth.Grant
	saveErr error
}

type rotatingGrantStore struct {
	mu    sync.Mutex
	grant mcpoauth.Grant
}

func (s *rotatingGrantStore) Load(context.Context, string) (mcpoauth.Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grant, nil
}

func (s *rotatingGrantStore) Save(_ context.Context, grant mcpoauth.Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grant = grant
	return nil
}

func TestRestoredSourcesDoNotReuseARotatedRefreshToken(t *testing.T) {
	t.Parallel()
	var refreshes atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse refresh request: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-1" {
			t.Errorf("refresh token = %q, want refresh-1", got)
		}
		if refreshes.Add(1) > 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-2",
			"refresh_token": "refresh-2",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(tokenServer.Close)

	client, err := resolvedClient{
		ClientID:      "aura",
		TokenEndpoint: tokenServer.URL,
		AuthStyle:     int(oauth2.AuthStyleInParams),
	}.encode()
	if err != nil {
		t.Fatalf("encode client: %v", err)
	}
	store := &rotatingGrantStore{grant: mcpoauth.Grant{
		ServerName: "calendar", AccessToken: "access-1", RefreshToken: "refresh-1",
		TokenType: "Bearer", ClientInfo: client, ExpiresAt: time.Now().Add(-time.Minute),
	}}
	ctx := identityctx.WithIdentityID(t.Context(), "6f1c4d24-27a1-4f0e-9a0a-9a0f0b2c1d33")
	first, err := restoreTokenSource(ctx, "calendar", store, http.DefaultClient, nil)
	if err != nil {
		t.Fatalf("restore first source: %v", err)
	}
	stale, err := restoreTokenSource(ctx, "calendar", store, http.DefaultClient, nil)
	if err != nil {
		t.Fatalf("restore stale source: %v", err)
	}
	if _, err := first.Token(); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	got, err := stale.Token()
	if err != nil {
		t.Fatalf("stale source reused the rotated refresh token: %v", err)
	}
	if got.AccessToken != "access-2" {
		t.Fatalf("stale source access token = %q, want access-2", got.AccessToken)
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want 1", got)
	}
}

// The whole point of the store: a mount for an identity that already authorized must not
// open a browser, and must not even ask the SDK to try.
func TestStoredGrantIsRestoredWithoutAnAuthorizationFlow(t *testing.T) {
	t.Parallel()
	rc := resolvedClient{ClientID: "A1", TokenEndpoint: "https://slack.com/api/oauth.v2.access"}
	blob, err := rc.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	store := &fakeStore{grant: mcpoauth.Grant{
		ServerName:   "notion",
		AccessToken:  "live-token",
		RefreshToken: "r1",
		TokenType:    "Bearer",
		ClientInfo:   blob,
		ExpiresAt:    time.Now().Add(time.Hour),
	}}

	source, err := restoreTokenSource(context.Background(), "notion", store, nil, nil)
	if err != nil {
		t.Fatalf("restoreTokenSource: %v", err)
	}
	if source == nil {
		t.Fatal("a stored grant produced no token source; the mount would re-authorize")
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "live-token" {
		t.Fatalf("access token = %q, want the stored one", tok.AccessToken)
	}
	// A valid stored token must not be written back on the first call: that would
	// rewrite the row on every mount for no reason.
	if len(store.saved) != 0 {
		t.Fatalf("an unchanged token was persisted again: %+v", store.saved)
	}
}

func TestStoredOAuthAccessTokenUsesTheExistingGrantPath(t *testing.T) {
	t.Parallel()
	blob, err := resolvedClient{ClientID: "A1", TokenEndpoint: "https://issuer.example/token"}.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	store := &fakeStore{grant: mcpoauth.Grant{
		ServerName:  "calendar",
		AccessToken: "identity-token",
		ClientInfo:  blob,
		ExpiresAt:   time.Now().Add(time.Hour),
	}}
	got, err := StoredOAuthAccessToken(t.Context(), "calendar", store, EgressPolicy{}, nil)
	if err != nil {
		t.Fatalf("StoredOAuthAccessToken: %v", err)
	}
	if got != "identity-token" {
		t.Fatalf("access token = %q", got)
	}
	if len(store.saved) != 0 {
		t.Fatalf("unchanged grant was persisted: %+v", store.saved)
	}
}

func TestStoredOAuthAccessTokenRequiresAuthorization(t *testing.T) {
	t.Parallel()
	_, err := StoredOAuthAccessToken(t.Context(), "calendar", &fakeStore{loadErr: mcpoauth.ErrNoGrant}, EgressPolicy{}, nil)
	if !errors.Is(err, ErrOAuthAuthorizationRequired) {
		t.Fatalf("err = %v, want ErrOAuthAuthorizationRequired", err)
	}
}

// Each of these is a NORMAL state, not a fault: the answer is "let the human authorize",
// and failing the mount instead would strand the identity with no way back.
func TestRestoreFallsBackToAuthorizationRatherThanFailing(t *testing.T) {
	t.Parallel()
	unrefreshable, err := resolvedClient{ClientID: "A1"}.encode() // no token endpoint
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for name, store := range map[string]*fakeStore{
		"never authorized":  {loadErr: mcpoauth.ErrNoGrant},
		"corrupt blob":      {grant: mcpoauth.Grant{ClientInfo: []byte("{not json")}},
		"no token endpoint": {grant: mcpoauth.Grant{ClientInfo: unrefreshable}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := restoreTokenSource(context.Background(), "notion", store, nil, nil)
			if err != nil {
				t.Fatalf("restoreTokenSource returned an error instead of deferring to the flow: %v", err)
			}
			if source != nil {
				t.Fatal("a token source was built from a grant that cannot be used")
			}
		})
	}
}

// A storage fault that is not ErrNoGrant is NOT a normal state — mounting unauthenticated
// or silently re-authorizing would hide a broken database.
func TestRestorePropagatesARealStorageFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection refused")
	if _, err := restoreTokenSource(context.Background(), "notion", &fakeStore{loadErr: boom}, nil, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the storage error", err)
	}
}

// The post-exchange hook, and the reason a restart does not re-open a browser. It fires
// ONCE, and it is the only moment the discovered token endpoint and a DCR-minted client
// id are visible — Hermes measured what missing it costs: a cold-start refresh falls back
// to a guessed {server}/token, 404s on most real providers, and forces full
// re-authorization.
func TestPersistOnAuthorizationStoresTheEndpointARefreshWillNeed(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	hook := persistOnAuthorization(context.Background(), "notion", "https://mcp.notion.com/mcp", store, nil)

	cfg := &oauth2.Config{
		ClientID:     "dcr-minted-id",
		ClientSecret: "dcr-minted-secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:   "https://api.notion.com/v1/oauth/authorize",
			TokenURL:  "https://api.notion.com/v1/oauth/token",
			AuthStyle: oauth2.AuthStyleInHeader,
		},
		Scopes: []string{"read"},
	}
	expiry := time.Now().Add(time.Hour).Round(0)
	source, err := hook(context.Background(), cfg, &oauth2.Token{
		AccessToken: "fresh", RefreshToken: "r1", TokenType: "Bearer", Expiry: expiry,
	})
	if err != nil {
		t.Fatalf("persistOnAuthorization: %v", err)
	}
	if source == nil {
		t.Fatal("no token source returned; later refreshes would never be persisted")
	}
	if len(store.saved) != 1 {
		t.Fatalf("saves = %d, want the authorization persisted once", len(store.saved))
	}
	got := store.saved[0]
	if got.AccessToken != "fresh" || got.RefreshToken != "r1" || got.ExpiresAt != expiry {
		t.Fatalf("saved tokens = %+v", got)
	}
	if got.ResourceURL != "https://mcp.notion.com/mcp" {
		t.Fatalf("resource url = %q", got.ResourceURL)
	}
	rc, err := decodeResolvedClient(got.ClientInfo)
	if err != nil {
		t.Fatalf("decode stored client: %v", err)
	}
	if rc.TokenEndpoint != cfg.Endpoint.TokenURL {
		t.Fatalf("token endpoint = %q, want the DISCOVERED one — a cold-start refresh has nowhere else to look", rc.TokenEndpoint)
	}
	// The DCR-minted secret is a credential, which is why it rides in the encrypted
	// column rather than beside the row as metadata.
	if rc.ClientID != "dcr-minted-id" || rc.ClientSecret != "dcr-minted-secret" {
		t.Fatalf("stored client = %+v", rc)
	}
	if rc.AuthStyle != int(oauth2.AuthStyleInHeader) {
		t.Fatalf("auth style = %d, want the resolved one preserved", rc.AuthStyle)
	}
}

// A human just finished a consent flow. Throwing that away because a write failed would
// make them do it again with no explanation.
func TestPersistOnAuthorizationSurvivesAStorageFault(t *testing.T) {
	t.Parallel()
	store := &fakeStore{saveErr: errors.New("disk on fire")}
	hook := persistOnAuthorization(context.Background(), "notion", "u", store, nil)

	source, err := hook(context.Background(), &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: "https://x.example/token"}}, &oauth2.Token{AccessToken: "fresh"})
	if err != nil {
		t.Fatalf("a completed authorization was discarded over a storage fault: %v", err)
	}
	if source == nil {
		t.Fatal("no token source returned")
	}
}

func TestPersistingTokenSourceWritesOnlyNewTokens(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	current := &oauth2.Token{AccessToken: "t1", Expiry: time.Now().Add(time.Hour)}
	src := newPersistingTokenSource(context.Background(), oauth2.StaticTokenSource(current), "notion", "https://mcp.notion.com/mcp", resolvedClient{TokenEndpoint: "https://x.example/token"}, store, nil, "", false)

	for range 3 {
		if _, err := src.Token(); err != nil {
			t.Fatalf("Token: %v", err)
		}
	}
	if len(store.saved) != 1 {
		t.Fatalf("saves = %d, want 1 — the same token was persisted repeatedly", len(store.saved))
	}
	if store.saved[0].AccessToken != "t1" || store.saved[0].ServerName != "notion" {
		t.Fatalf("saved = %+v", store.saved[0])
	}
	// Absolute, because a relative expires_in means nothing after a restart.
	if store.saved[0].ExpiresAt != current.Expiry {
		t.Fatalf("expiry = %v, want the token's absolute expiry %v", store.saved[0].ExpiresAt, current.Expiry)
	}
}

// A refreshed token that cannot be stored is still a valid token for THIS process.
// Refusing it would break a working session over a storage fault.
func TestPersistFailureDoesNotBreakTheSession(t *testing.T) {
	t.Parallel()
	store := &fakeStore{saveErr: errors.New("disk on fire")}
	src := newPersistingTokenSource(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t1"}), "notion", "u", resolvedClient{}, store, nil, "", false)

	tok, err := src.Token()
	if err != nil {
		t.Fatalf("Token failed because persistence did: %v", err)
	}
	if tok.AccessToken != "t1" {
		t.Fatalf("token = %+v", tok)
	}
}

// The HTTP twin of the stdio two-context split. A token source built on the handshake
// context refreshes exactly once — never — because the context it captured died when the
// mount returned; and the identity has to survive, because the store scopes every row by
// it.
func TestDetachedRefreshContextOutlivesTheMountButKeepsItsValues(t *testing.T) {
	t.Parallel()
	type identityKey struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), identityKey{}, "id-42"))
	detached := detachedForRefresh(parent)
	cancel()

	if err := detached.Err(); err != nil {
		t.Fatalf("the refresh context died with the mount: %v", err)
	}
	if got := detached.Value(identityKey{}); got != "id-42" {
		t.Fatalf("identity on the detached context = %v, want it preserved", got)
	}
}

func TestResolvedClientSurvivesARoundTrip(t *testing.T) {
	t.Parallel()
	original := resolvedClient{
		ClientID:              "A1",
		ClientSecret:          "s3cr3t",
		AuthorizationEndpoint: "https://slack.com/oauth/v2/authorize",
		TokenEndpoint:         "https://slack.com/api/oauth.v2.access",
		AuthStyle:             int(oauth2.AuthStyleInParams),
		RedirectURI:           "http://127.0.0.1:9999/cb",
		Scopes:                []string{"chat:write"},
	}
	blob, err := original.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeResolvedClient(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round trip changed the client: %+v vs %+v", got, original)
	}
	cfg := got.oauth2Config()
	if cfg.Endpoint.TokenURL != original.TokenEndpoint {
		t.Fatalf("token endpoint = %q", cfg.Endpoint.TokenURL)
	}
	// The auth style is resolved from the server's advertised methods. Losing it costs
	// a failed refresh against every provider that accepts only one of the two.
	if cfg.Endpoint.AuthStyle != oauth2.AuthStyleInParams {
		t.Fatalf("auth style = %v, want it preserved", cfg.Endpoint.AuthStyle)
	}
}

func TestEmptyClientInfoIsNotAnError(t *testing.T) {
	t.Parallel()
	got, err := decodeResolvedClient(nil)
	if err != nil {
		t.Fatalf("decodeResolvedClient(nil): %v", err)
	}
	if got.TokenEndpoint != "" {
		t.Fatalf("got = %+v, want the zero value", got)
	}
}

// A server that advertises no expiry must not be handed an invented one: oauth2 reads a
// zero expiry as "use it until refused", which is the same reading Grant.Expired
// documents. Substituting a guess would refresh tokens that never needed it.
func TestZeroExpiryIsPreservedInBothDirections(t *testing.T) {
	t.Parallel()
	grant, err := grantFrom("notion", "u", &oauth2.Token{AccessToken: "t"}, resolvedClient{})
	if err != nil {
		t.Fatalf("grantFrom: %v", err)
	}
	if !grant.ExpiresAt.IsZero() {
		t.Fatalf("expiry = %v, want zero", grant.ExpiresAt)
	}
	if got := tokenFrom(grant); !got.Expiry.IsZero() {
		t.Fatalf("token expiry = %v, want zero", got.Expiry)
	}
	if grant.Expired(time.Now(), oauthTokenLeeway) {
		t.Fatal("a grant with no advertised expiry read as expired")
	}
}

// TestPersistOnAuthorizationScopesTheGrantToTheMountIdentity pins the 2026-08-24 loss: the
// SDK invokes this hook with a context built from context.Background() (go-sdk v1.7.0
// auth/authorization_code.go:645), so an identity inherited from the request can never
// reach it. Linear consented, the code was exchanged, and the grant was dropped with
// "no identity on context" — a completed human authorization discarded.
func TestPersistOnAuthorizationScopesTheGrantToTheMountIdentity(t *testing.T) {
	t.Parallel()
	const owner = "6f1c4d24-27a1-4f0e-9a0a-9a0f0b2c1d33"
	store := &identityRecordingStore{}
	mountCtx := identityctx.WithIdentityID(context.Background(), owner)
	hook := persistOnAuthorization(mountCtx, "linear", "https://mcp.linear.app/mcp", store, nil)

	// context.Background(), exactly as the SDK hands it over: no identity, no deadline.
	if _, err := hook(context.Background(), &oauth2.Config{
		ClientID: "id",
		Endpoint: oauth2.Endpoint{TokenURL: "https://mcp.linear.app/token"},
	}, &oauth2.Token{AccessToken: "fresh", RefreshToken: "r1", TokenType: "Bearer"}); err != nil {
		t.Fatalf("persistOnAuthorization: %v", err)
	}
	if store.seen != owner {
		t.Fatalf("grant saved for identity %q, want %q — the authorization would be discarded", store.seen, owner)
	}
}

// identityRecordingStore records the identity on the context each Save was given.
type identityRecordingStore struct {
	seen string
}

func (s *identityRecordingStore) Save(ctx context.Context, _ mcpoauth.Grant) error {
	s.seen = identityctx.IdentityID(ctx)
	return nil
}

func (s *identityRecordingStore) Load(context.Context, string) (mcpoauth.Grant, error) {
	return mcpoauth.Grant{}, nil
}
