package chatgptplan

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwt"
)

func TestRegistrationPersistsPerIdentityAndReusesClient(t *testing.T) {
	f := newOIDC(t)
	dir := t.TempDir()
	s := f.service(t, dir)
	flow, q := f.authorization(t, s, "alice")
	for key, want := range map[string]string{"client_id": dynamicClient, "agent_name_hint": "Aura", "resource": resource, "response_type": "code", "redirect_uri": testRedirect, "code_challenge_method": "S256"} {
		if q.Get(key) != want {
			t.Fatalf("%s=%q, want %q", key, q.Get(key), want)
		}
	}
	if !validHostID(q.Get("ext_agent_host_id")) || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Fatal("missing host/state/nonce")
	}
	replayed, err := s.Start(ownerContext("alice"), "alice", testRedirect)
	if err != nil || replayed != flow {
		t.Fatal("start did not retain pending authorization")
	}
	callback := f.callback(t, q, "oaiapp_alice")
	if err = s.Callback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
	if err = s.Callback(context.Background(), callback); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("callback replay accepted")
	}
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || !status.Connected || !status.PlanEnabled || status.Email != "same@example.com" || status.Status != "approved" {
		t.Fatalf("status: %+v %v", status, err)
	}
	jsonStatus, _ := json.Marshal(status)
	if strings.Contains(string(jsonStatus), "access-") || strings.Contains(string(jsonStatus), "refresh-") || strings.Contains(string(jsonStatus), "id_token") {
		t.Fatal("status leaked tokens")
	}
	data, err := os.ReadFile(s.path("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "access-") || strings.Contains(string(data), status.Email) || strings.Contains(string(data), "subject-") {
		t.Fatal("credentials not encrypted")
	}
	restarted := f.service(t, dir)
	if restarted.hostID != s.hostID {
		t.Fatal("host identity changed after restart")
	}
	access, err := restarted.AccessToken(ownerContext("alice"))
	if err != nil || access != "access-oaiapp_alice" {
		t.Fatalf("restart access: %q %v", access, err)
	}
	_, returning := f.authorization(t, restarted, "alice")
	if returning.Get("client_id") != "oaiapp_alice" || returning.Has("agent_name_hint") || returning.Get("id_token_hint") == "" || returning.Get("login_hint") != status.Email {
		t.Fatal("reauthorization lost registration or login hints")
	}
	_, bob := f.authorization(t, restarted, "bob")
	if bob.Get("client_id") != dynamicClient || bob.Get("state") == returning.Get("state") || bob.Get("nonce") == returning.Get("nonce") {
		t.Fatal("identities share registration or flow secrets")
	}
	if _, err = restarted.AccessToken(ownerContext("bob")); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatal("borrowed another identity's credential")
	}
}

func TestCallbackValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(url.Values)
	}{
		{"missing state", func(q url.Values) { q.Del("state") }},
		{"wrong state", func(q url.Values) { q.Set("state", "not-current") }},
		{"duplicated state", func(q url.Values) { q.Add("state", q.Get("state")) }},
		{"missing client", func(q url.Values) { q.Del("client_id") }},
		{"dynamic client", func(q url.Values) { q.Set("client_id", dynamicClient) }},
		{"missing code", func(q url.Values) { q.Del("code") }},
		{"duplicate code", func(q url.Values) { q.Add("code", "second") }},
		{"issuer mismatch", func(q url.Values) { q.Set("iss", "https://attacker.example") }},
		{"declined", func(q url.Values) { q.Set("error", "access_denied") }},
	}
	f := newOIDC(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := f.service(t, t.TempDir())
			_, auth := f.authorization(t, s, "alice")
			query := f.callback(t, auth, "oaiapp_alice")
			tt.change(query)
			if err := s.Callback(context.Background(), query); err == nil {
				t.Fatal("invalid callback accepted")
			}
			status, err := s.Status(ownerContext("alice"), "alice")
			if err != nil || status.Connected {
				t.Fatal("invalid callback stored credentials")
			}
		})
	}
	if f.exchanges != 0 {
		t.Fatal("invalid callbacks reached token endpoint")
	}
}

func TestOIDCRejectsInvalidSignedClaims(t *testing.T) {
	tests := []struct {
		name   string
		modify func(jwt.Token)
	}{
		{"nonce", func(tok jwt.Token) { tok.Set("nonce", "not-the-attempt") }},
		{"audience", func(tok jwt.Token) { tok.Set("aud", []string{"another-client"}) }},
		{"issuer", func(tok jwt.Token) { tok.Set("iss", "https://attacker.example") }},
		{"expired", func(tok jwt.Token) { tok.Set("exp", time.Now().Add(-time.Minute)) }},
		{"missing expiration", func(tok jwt.Token) { tok.Remove("exp") }},
		{"missing subject", func(tok jwt.Token) { tok.Remove("sub") }},
		{"empty subject", func(tok jwt.Token) { tok.Set("sub", "") }},
		{"invalid email", func(tok jwt.Token) { tok.Set("email", 123) }},
	}
	f := newOIDC(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.modifyClaims = tt.modify
			s := f.service(t, t.TempDir())
			_, auth := f.authorization(t, s, "alice")
			if err := s.Callback(context.Background(), f.callback(t, auth, "oaiapp_alice")); err == nil {
				t.Fatal("invalid ID token accepted")
			}
			if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatal("invalid token persisted")
			}
		})
	}
}

func TestGrantedScopesDecidePlanPermission(t *testing.T) {
	f := newOIDC(t)
	f.scope = "openid email profile"
	s := f.service(t, t.TempDir())
	_, auth := f.authorization(t, s, "alice")
	query := f.callback(t, auth, "oaiapp_alice")
	query.Set("scope", planScope)
	if err := s.Callback(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || !status.Connected || status.PlanEnabled {
		t.Fatal("trusted callback scope instead of token grant")
	}
	if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrPlanDisabled) {
		t.Fatal("inference accepted without plan scope")
	}
}

func TestReauthorizationRequestsConsentOnlyWithoutPlanPermission(t *testing.T) {
	for _, test := range []struct {
		name    string
		granted bool
	}{
		{"ordinary login", true},
		{"enable declined plan", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			_, initial := f.authorization(t, s, "alice")
			if initial.Has("prompt") {
				t.Fatal("initial registration forced reconsent")
			}
			if !test.granted {
				f.scope = "openid profile email"
			}
			if err := s.Callback(context.Background(), f.callback(t, initial, "oaiapp_alice")); err != nil {
				t.Fatal(err)
			}
			_, returning := f.authorization(t, s, "alice")
			if returning.Get("client_id") != "oaiapp_alice" || returning.Has("agent_name_hint") {
				t.Fatal("reauthorization replaced the existing registration")
			}
			if want := !test.granted; (returning.Get("prompt") == "consent") != want || (test.granted && returning.Has("prompt")) {
				t.Fatalf("reauthorization prompt = %q, granted = %v", returning.Get("prompt"), test.granted)
			}
			for _, scope := range []string{"openid", "profile", "email", "offline_access", "resource.invoke", planScope} {
				if !strings.Contains(" "+returning.Get("scope")+" ", " "+scope+" ") {
					t.Fatalf("reauthorization omitted scope %q", scope)
				}
			}
			f.scope = "openid profile email offline_access resource.invoke " + planScope
			if err := s.Callback(context.Background(), f.callback(t, returning, "oaiapp_alice")); err != nil {
				t.Fatal(err)
			}
			status, err := s.Status(ownerContext("alice"), "alice")
			if err != nil || !status.Connected || !status.PlanEnabled {
				t.Fatalf("plan consent was not rechecked: %+v %v", status, err)
			}
			_, authorized := f.authorization(t, s, "alice")
			if authorized.Has("prompt") {
				t.Fatal("already-enabled plan kept forcing consent")
			}
		})
	}
}

func TestFlowReplacementExpiryAndReturningAccount(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	_, old := f.authorization(t, s, "alice")
	if _, err := s.Start(ownerContext("alice"), "alice", "http://127.0.0.1:9000/auth/callback"); err != nil {
		t.Fatal(err)
	}
	if err := s.Callback(context.Background(), f.callback(t, old, "oaiapp_alice")); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("replaced state accepted")
	}
	_, fresh := f.authorization(t, s, "bob")
	s.flows["bob"].expires = time.Now().Add(-time.Second)
	if err := s.Callback(context.Background(), f.callback(t, fresh, "oaiapp_bob")); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("expired state accepted")
	}
	f.connect(t, s, "alice")
	_, auth := f.authorization(t, s, "alice")
	query := f.callback(t, auth, "oaiapp_attacker")
	if err := s.Callback(context.Background(), query); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("changed returning client accepted")
	}
	_, auth = f.authorization(t, s, "alice")
	f.modifyClaims = func(tok jwt.Token) { tok.Set("sub", "changed-account") }
	query = f.callback(t, auth, "oaiapp_alice")
	query.Del("client_id")
	if err := s.Callback(context.Background(), query); err == nil {
		t.Fatal("changed returning subject accepted")
	}
	if access, err := s.AccessToken(ownerContext("alice")); err != nil || access != "access-oaiapp_alice" {
		t.Fatal("invalid returning login replaced active account")
	}
}
