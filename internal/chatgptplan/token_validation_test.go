package chatgptplan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

func TestCodeExchangeFailureRetainsPendingIssuedClient(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	_, q := f.authorization(t, s, "alice")
	f.tokenFailure = 400
	err := s.Callback(context.Background(), f.callback(t, q, "oaiapp_alice"))
	if err == nil || strings.Contains(err.Error(), "SECRET-MUST-NOT-LEAK") {
		t.Fatal("exchange failed unsafely")
	}
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || status.Status != "error" || status.Connected || status.Error == "" {
		t.Fatal("failed exchange not reported")
	}
	_, next := f.authorization(t, s, "alice")
	if next.Get("client_id") != "oaiapp_alice" || next.Has("agent_name_hint") {
		t.Fatal("exchange retry registered another client")
	}
	f.tokenFailure = 0
	callback := f.callback(t, next, "oaiapp_alice")
	callback.Del("client_id")
	if err := s.Callback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidTokenResponsesNeverPersist(t *testing.T) {
	f := newOIDC(t)
	for _, test := range []struct {
		name   string
		modify func(map[string]any, bool)
	}{
		{"empty bearer", func(m map[string]any, _ bool) { m["access_token"] = "" }},
		{"wrong type", func(m map[string]any, _ bool) { m["token_type"] = "MAC" }},
		{"missing expiry", func(m map[string]any, _ bool) { delete(m, "expires_in") }},
		{"missing ID token", func(m map[string]any, _ bool) { delete(m, "id_token") }},
		{"signature", func(m map[string]any, _ bool) {
			raw := m["id_token"].(string)
			parts := strings.Split(raw, ".")
			parts[2] = "AAAAAAAA"
			m["id_token"] = strings.Join(parts, ".")
		}},
		{"offline grant missing refresh", func(m map[string]any, _ bool) { delete(m, "refresh_token") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.modifyResponse = test.modify
			s := f.service(t, t.TempDir())
			_, q := f.authorization(t, s, "alice")
			if err := s.Callback(context.Background(), f.callback(t, q, "oaiapp_alice")); err == nil {
				t.Fatal("invalid token response accepted")
			}
			if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatal("invalid token stored")
			}
		})
	}
	f.modifyResponse = nil
	f.jwksCode = 503
	s := f.service(t, t.TempDir())
	_, q := f.authorization(t, s, "alice")
	if err := s.Callback(context.Background(), f.callback(t, q, "oaiapp_alice")); err == nil {
		t.Fatal("failed JWKS fetch accepted")
	}
}

func TestRefreshIDTokenRemainsBoundToAccount(t *testing.T) {
	for _, subject := range []string{"subject-oaiapp_alice", "another-account"} {
		t.Run(subject, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			f.connect(t, s, "alice")
			changeCredential(t, s, "alice", func(c *credential) { c.ExpiresAt = time.Now().Add(-time.Second) })
			id := jwt.New()
			for claim, value := range map[string]any{"iss": f.server.URL, "sub": subject, "aud": []string{"oaiapp_alice"}, "exp": time.Now().Add(time.Hour), "email": "renewed@example.com"} {
				if err := id.Set(claim, value); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := jwt.Sign(id, jwt.WithKey(jwa.RS256(), f.private))
			if err != nil {
				t.Fatal(err)
			}
			f.modifyResponse = func(m map[string]any, refresh bool) {
				if refresh {
					m["id_token"] = string(raw)
				}
			}
			_, err = s.AccessToken(ownerContext("alice"))
			if subject == "another-account" {
				if err == nil {
					t.Fatal("refresh accepted another account's ID token")
				}
				c, loadErr := s.load("alice")
				if loadErr != nil || c.Email != "same@example.com" || c.RefreshToken != "refresh-oaiapp_alice" {
					t.Fatal("invalid refresh replaced credentials")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				status, err := s.Status(ownerContext("alice"), "alice")
				if err != nil || status.Email != "renewed@example.com" {
					t.Fatal("validated refresh ID token did not update account")
				}
			}
		})
	}
}
