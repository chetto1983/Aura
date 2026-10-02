package chatgptplan

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRefreshRotationSerializesAcrossServicesAndIdentities(t *testing.T) {
	f := newOIDC(t)
	dir := t.TempDir()
	s := f.service(t, dir)
	f.connect(t, s, "alice")
	f.connect(t, s, "bob")
	changeCredential(t, s, "alice", func(c *credential) { c.ExpiresAt = time.Now().Add(-time.Second) })
	restarted := f.service(t, dir)
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			service := s
			if i%2 == 0 {
				service = restarted
			}
			access, err := service.AccessToken(ownerContext("alice"))
			if err != nil || access != "rotated-access-oaiapp_alice" {
				t.Errorf("rotated token: %q %v", access, err)
			}
		})
	}
	wg.Wait()
	if f.refreshes != 1 || len(f.refreshSeen) != 1 {
		t.Fatal("rotating refresh token was used concurrently")
	}
	c, err := restarted.load("alice")
	if err != nil || c.RefreshToken != "rotated-refresh-oaiapp_alice" || !slices.Contains(c.Scopes, planScope) {
		t.Fatal("rotation not persisted together or omitted scope lost grant")
	}
	if access, err := s.AccessToken(ownerContext("bob")); err != nil || access != "access-oaiapp_bob" {
		t.Fatal("refresh replaced another identity")
	}
}

func TestRefreshFailuresAndScopeReduction(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*oidcFixture)
		want         error
		disconnected bool
	}{
		{"transient", func(f *oidcFixture) { f.refreshFailure = 503; f.refreshError = "temporarily_unavailable" }, nil, false},
		{"invalid client", func(f *oidcFixture) { f.refreshFailure = 400; f.refreshError = "invalid_client" }, nil, false},
		{"scope reduced", func(f *oidcFixture) { scope := "openid email"; f.refreshScope = &scope }, ErrPlanDisabled, false},
		{"missing rotation", func(f *oidcFixture) { f.missingRefresh = true }, nil, false},
	}
	for _, code := range []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"} {
		tests = append(tests, struct {
			name         string
			setup        func(*oidcFixture)
			want         error
			disconnected bool
		}{code, func(f *oidcFixture) { f.refreshFailure = 400; f.refreshError = code }, ErrAuthorizationRequired, true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			f.connect(t, s, "alice")
			changeCredential(t, s, "alice", func(c *credential) { c.ExpiresAt = time.Now().Add(-time.Second) })
			tt.setup(f)
			_, err := s.AccessToken(ownerContext("alice"))
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if strings.Contains(err.Error(), "SECRET-MUST-NOT-LEAK") {
				t.Fatal("provider response leaked")
			}
			c, loadErr := s.load("alice")
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if tt.disconnected && (c.AccessToken != "" || c.RefreshToken != "" || c.IDToken != "") {
				t.Fatal("revoked credentials retained")
			}
			if tt.disconnected {
				status, statusErr := s.Status(ownerContext("alice"), "alice")
				if statusErr != nil || status.Connected {
					t.Fatal("revoked session appears connected")
				}
				_, q := f.authorization(t, s, "alice")
				if q.Get("client_id") != "oaiapp_alice" || q.Has("id_token_hint") {
					t.Fatal("revoked reauthorization lost client mapping or retained session hint")
				}
				f.refreshFailure = 0
				if err := s.Callback(context.Background(), f.callback(t, q, "oaiapp_alice")); err != nil {
					t.Fatal(err)
				}
				if access, err := s.AccessToken(ownerContext("alice")); err != nil || access != "access-oaiapp_alice" {
					t.Fatalf("reauthorized account is not usable: %q %v", access, err)
				}
			} else if tt.name == "transient" || tt.name == "invalid client" {
				if c.AccessToken != "access-oaiapp_alice" || c.RefreshToken != "refresh-oaiapp_alice" || c.IDToken == "" {
					t.Fatal("non-terminal refresh error cleared credentials")
				}
				f.refreshFailure = 0
				if access, err := s.AccessToken(ownerContext("alice")); err != nil || access != "rotated-access-oaiapp_alice" {
					t.Fatalf("later refresh did not recover: %q %v", access, err)
				}
			}
		})
	}
}

func TestRefreshEarliestTimeAndMissingRefreshGrant(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	f.connect(t, s, "alice")
	changeCredential(t, s, "alice", func(c *credential) {
		c.ExpiresAt = time.Now().Add(30 * time.Second)
		c.EarliestRefreshAt = time.Now().Add(time.Minute)
	})
	if access, err := s.AccessToken(ownerContext("alice")); err != nil || access != "access-oaiapp_alice" {
		t.Fatal("early refresh should retain still-valid token")
	}
	changeCredential(t, s, "alice", func(c *credential) { c.ExpiresAt = time.Now().Add(-time.Second) })
	if _, err := s.AccessToken(ownerContext("alice")); err == nil {
		t.Fatal("expired token used before earliest_refresh_at")
	}
	if f.refreshes != 0 {
		t.Fatal("refreshed before earliest_refresh_at")
	}
	changeCredential(t, s, "alice", func(c *credential) { c.RefreshToken = ""; c.EarliestRefreshAt = time.Time{} })
	if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatal("expired token without refresh was accepted")
	}
	for _, v := range []any{float64(1790000000), "1790000000", "2026-09-21T14:13:20Z"} {
		if refreshTime(v).IsZero() {
			t.Fatalf("refresh time rejected %v", v)
		}
	}
	for _, v := range []any{nil, "bad", float64(-1), "-1"} {
		if !refreshTime(v).IsZero() {
			t.Fatalf("invalid refresh time accepted %v", v)
		}
	}
}

func TestDisconnectRevokesAndRetainsMapping(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    int
		foreign bool
		want    error
	}{
		{"success", 0, false, nil}, {"server failure", 503, false, ErrRevocationUnconfirmed}, {"client failure", 400, false, ErrRevocationUnconfirmed}, {"untrusted endpoint", 0, true, ErrRevocationUnconfirmed},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			f.connect(t, s, "alice")
			f.connect(t, s, "bob")
			f.revokeCode = test.code
			if test.foreign {
				f.discoveryOverride = "https://attacker.example/revoke"
			}
			if err := s.Disconnect(ownerContext("alice"), "alice"); !errors.Is(err, test.want) {
				t.Fatalf("disconnect=%v, want %v", err, test.want)
			}
			status, err := s.Status(ownerContext("alice"), "alice")
			if err != nil || status.Connected || status.PlanEnabled || status.Email != "same@example.com" {
				t.Fatal("logout lost account mapping or retained live tokens")
			}
			if _, err = s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatal("disconnected token still usable")
			}
			_, q := f.authorization(t, s, "alice")
			if q.Get("client_id") != "oaiapp_alice" || q.Has("id_token_hint") {
				t.Fatal("logout lost issued client mapping or retained token hint")
			}
			if err = s.Callback(context.Background(), f.callback(t, q, "oaiapp_alice")); err != nil {
				t.Fatal(err)
			}
			if _, err = s.AccessToken(ownerContext("bob")); err != nil {
				t.Fatal("logout affected another identity")
			}
		})
	}
}
