package chatgptplan

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"go.uber.org/goleak"
	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/identityctx"
)

const testSecret = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
const testRedirect = "http://127.0.0.1:8089/auth/callback"

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type oidcFixture struct {
	t                                                  *testing.T
	server                                             *httptest.Server
	private                                            jwk.Key
	public                                             jwk.Set
	mu                                                 sync.Mutex
	auth                                               map[string]url.Values
	scope                                              string
	refreshScope                                       *string
	modifyClaims                                       func(jwt.Token)
	modifyResponse                                     func(map[string]any, bool)
	tokenFailure, refreshFailure, revokeCode, jwksCode int
	refreshError                                       string
	missingRefresh                                     bool
	exchanges, refreshes, revocations                  int
	refreshSeen                                        []url.Values
	discoveryOverride                                  string
	revokeDrop                                         bool
}

func newOIDC(t *testing.T) *oidcFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, err := jwk.Import(key)
	if err != nil {
		t.Fatal(err)
	}
	if err = private.Set(jwk.KeyIDKey, "fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err = private.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
		t.Fatal(err)
	}
	public, err := private.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err = set.AddKey(public); err != nil {
		t.Fatal(err)
	}
	f := &oidcFixture{t: t, private: private, public: set, auth: make(map[string]url.Values), scope: "openid profile email offline_access resource.invoke " + planScope}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(func() { f.server.Close(); f.server.CloseClientConnections() })
	return f
}

func (f *oidcFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/jwks":
		if f.jwksCode != 0 {
			w.WriteHeader(f.jwksCode)
			return
		}
		json.NewEncoder(w).Encode(f.public)
	case "/discovery":
		endpoint := f.server.URL + "/revoke"
		if f.discoveryOverride != "" {
			endpoint = f.discoveryOverride
		}
		json.NewEncoder(w).Encode(map[string]string{"issuer": f.server.URL, "revocation_endpoint": endpoint})
	case "/revoke":
		f.revocations++
		if f.revokeDrop {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				f.t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		r.ParseForm()
		if r.Form.Get("token_type_hint") != "refresh_token" || !strings.HasPrefix(r.Form.Get("client_id"), "oaiapp_") || !strings.HasPrefix(r.Form.Get("token"), "refresh-") {
			f.t.Error("incorrect revoke form")
		}
		if f.revokeCode != 0 {
			w.WriteHeader(f.revokeCode)
		}
	case "/token":
		r.ParseForm()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Form.Get("resource") != resource || r.Form.Get("client_secret") != "" || r.Header.Get("Authorization") != "" {
			f.t.Error("incorrect public-client token request")
		}
		client := r.Form.Get("client_id")
		if !strings.HasPrefix(client, "oaiapp_") {
			f.t.Error("dynamic client used for token exchange")
		}
		response := map[string]any{"access_token": "access-" + client, "refresh_token": "refresh-" + client, "token_type": "Bearer", "expires_in": 3600}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			f.exchanges++
			if f.tokenFailure != 0 {
				w.WriteHeader(f.tokenFailure)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "SECRET-MUST-NOT-LEAK"})
				return
			}
			q := f.auth[r.Form.Get("code")]
			if q == nil || r.Form.Get("redirect_uri") != q.Get("redirect_uri") || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != q.Get("code_challenge") {
				f.t.Error("code exchange lost PKCE or exact redirect")
			}
			token := jwt.New()
			for claim, value := range map[string]any{"iss": f.server.URL, "sub": "subject-" + client, "aud": []string{client}, "exp": time.Now().Add(time.Hour), "iat": time.Now().Add(-time.Minute), "nonce": q.Get("nonce"), "email": "same@example.com"} {
				if err := token.Set(claim, value); err != nil {
					f.t.Error(err)
				}
			}
			if f.modifyClaims != nil {
				f.modifyClaims(token)
			}
			signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), f.private))
			if err != nil {
				f.t.Error(err)
			}
			response["id_token"], response["scope"] = string(signed), f.scope
		case "refresh_token":
			f.refreshes++
			f.refreshSeen = append(f.refreshSeen, r.Form)
			if r.Form.Get("refresh_token") != "refresh-"+client || r.Form.Has("scope") {
				f.t.Error("refresh combined another account or changed scopes")
			}
			if f.refreshFailure != 0 {
				w.WriteHeader(f.refreshFailure)
				json.NewEncoder(w).Encode(map[string]string{"error": f.refreshError, "error_description": "SECRET-MUST-NOT-LEAK"})
				return
			}
			response["access_token"], response["refresh_token"] = "rotated-access-"+client, "rotated-refresh-"+client
			if f.refreshScope != nil {
				response["scope"] = *f.refreshScope
			}
			if f.missingRefresh {
				delete(response, "refresh_token")
			}
		default:
			f.t.Error("unexpected token grant")
		}
		if f.modifyResponse != nil {
			f.modifyResponse(response, r.Form.Get("grant_type") == "refresh_token")
		}
		json.NewEncoder(w).Encode(response)
	default:
		http.NotFound(w, r)
	}
}

func (f *oidcFixture) service(t *testing.T, dir string) *Service {
	t.Helper()
	s, err := New(dir, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	s.endpoint = endpoints{issuer: f.server.URL, authorize: f.server.URL + "/authorize", token: f.server.URL + "/token", jwks: f.server.URL + "/jwks", discovery: f.server.URL + "/discovery"}
	s.client = f.server.Client()
	s.client.Timeout = 3 * time.Second
	s.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return s
}

func ownerContext(owner string) context.Context {
	return identityctx.WithIdentityID(context.Background(), owner)
}

func (f *oidcFixture) authorization(t *testing.T, s *Service, owner string) (Flow, url.Values) {
	t.Helper()
	flow, err := s.Start(ownerContext(owner), owner, testRedirect)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(flow.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	return flow, u.Query()
}

func (f *oidcFixture) callback(t *testing.T, q url.Values, clientID string) url.Values {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	code := fmt.Sprintf("authorization-code-%d", len(f.auth)+1)
	f.auth[code] = q
	return url.Values{"state": {q.Get("state")}, "code": {code}, "client_id": {clientID}}
}

func (f *oidcFixture) connect(t *testing.T, s *Service, owner string) {
	t.Helper()
	_, q := f.authorization(t, s, owner)
	if err := s.Callback(context.Background(), f.callback(t, q, "oaiapp_"+owner)); err != nil {
		t.Fatal(err)
	}
}

func changeCredential(t *testing.T, s *Service, owner string, change func(*credential)) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.lockStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	c, err := s.load(owner)
	if err != nil || c == nil {
		t.Fatalf("load credential: %v", err)
	}
	change(c)
	if err = s.save(c); err != nil {
		t.Fatal(err)
	}
}
