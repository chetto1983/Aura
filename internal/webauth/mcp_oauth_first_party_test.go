package webauth

import (
	"context"
	"errors"
	"testing"
	"time"

	jwtoptions "github.com/Authula/authula/plugins/jwt/types"

	"github.com/chetto1983/aura/internal/mcp"
)

type stubIssuer struct {
	userID    string
	sessionID string
	claims    map[string]any
	err       error
}

func (s *stubIssuer) GenerateUserToken(_ context.Context, userID, sessionID string, claims map[string]any) (*jwtoptions.TokenPair, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.userID, s.sessionID, s.claims = userID, sessionID, claims
	return &jwtoptions.TokenPair{
		AccessToken: "access-" + sessionID, RefreshToken: "refresh-" + sessionID,
		ExpiresIn: 15 * time.Minute, TokenType: "Bearer",
	}, nil
}

func (*stubIssuer) GenerateMachineToken(context.Context, string, string, []string) (*jwtoptions.TokenPair, error) {
	return &jwtoptions.TokenPair{}, nil
}

func (*stubIssuer) ExtractClaims(context.Context, string) (map[string]any, error) { return nil, nil }

type stubRefreshStore struct {
	token     string
	sessionID string
	expiresAt time.Time
	calls     int
	err       error
}

func (*stubRefreshStore) RefreshTokens(context.Context, string) (*jwtoptions.RefreshTokenResponse, error) {
	return nil, errors.New("not used")
}

func (s *stubRefreshStore) StoreInitialRefreshToken(_ context.Context, token, sessionID string, expiresAt time.Time) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.token, s.sessionID, s.expiresAt = token, sessionID, expiresAt
	return nil
}

func firstPartyTestServer(t *testing.T, now time.Time) (*OAuthServer, *stubIssuer, *stubRefreshStore) {
	t.Helper()
	jwtStub, refreshStub := &stubIssuer{}, &stubRefreshStore{}
	server := &OAuthServer{
		tokens: &mcpTokenPlugin{jwt: jwtStub, refresh: refreshStub},
		now:    func() time.Time { return now },
	}
	return server, jwtStub, refreshStub
}

func TestIssueFirstPartyTokenMintsTheBrowserExchangeClaimShape(t *testing.T) {
	now := time.Unix(1_788_303_600, 0).UTC()
	server, jwtStub, refreshStub := firstPartyTestServer(t, now)
	const identity = "b130c94d-a213-463a-a797-ec124104363a"
	const resource = "http://127.0.0.1:8096/mcp/"

	issued, err := server.IssueFirstPartyToken(t.Context(), resource, identity)
	if err != nil {
		t.Fatalf("IssueFirstPartyToken: %v", err)
	}

	want := mcpTokenClaims(resource, mcp.AuraOAuthToolsScope, mcpOAuthClientID, identity)
	for key, value := range want {
		if got := jwtStub.claims[key]; got != value {
			t.Errorf("claim %q = %#v, want %#v", key, got, value)
		}
	}
	if len(jwtStub.claims) != len(want) {
		t.Errorf("claims = %#v, want exactly the browser exchange shape %#v", jwtStub.claims, want)
	}
	if jwtStub.userID != identity {
		t.Errorf("user id = %q, want the identity %q", jwtStub.userID, identity)
	}
	if issued.AccessToken != "access-"+jwtStub.sessionID || issued.RefreshToken != "refresh-"+jwtStub.sessionID {
		t.Fatalf("issued pair %+v does not come from the minted session %q", issued, jwtStub.sessionID)
	}
	if !issued.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Errorf("ExpiresAt = %s, want %s", issued.ExpiresAt, now.Add(15*time.Minute))
	}
	if refreshStub.token != issued.RefreshToken || refreshStub.sessionID != jwtStub.sessionID {
		t.Errorf("stored refresh %+v does not match the issued pair", refreshStub)
	}
	if !refreshStub.expiresAt.Equal(now.Add(firstPartyRefreshTTL)) {
		t.Errorf("refresh expiry = %s, want %s", refreshStub.expiresAt, now.Add(firstPartyRefreshTTL))
	}
}

// Two identities must never share a subject: the subject IS the tenancy boundary the
// resource servers select a database with.
func TestIssueFirstPartyTokenScopesTheSubjectToTheIdentity(t *testing.T) {
	now := time.Unix(1_788_303_600, 0).UTC()
	server, jwtStub, _ := firstPartyTestServer(t, now)
	const other = "9f1c0f2e-1111-4b2a-9d3e-0f0a0b0c0d0e"

	if _, err := server.IssueFirstPartyToken(t.Context(), "http://127.0.0.1:8093/", other); err != nil {
		t.Fatalf("IssueFirstPartyToken: %v", err)
	}
	if got := jwtStub.claims[mcpSubjectClaim]; got != other {
		t.Fatalf("%s = %#v, want %q", mcpSubjectClaim, got, other)
	}
	if got := jwtStub.claims["aud"]; got != "http://127.0.0.1:8093/" {
		t.Fatalf("aud = %#v, want the calendar resource", got)
	}
}

func TestIssueFirstPartyTokenRefusesForeignResourcesAndBadSubjects(t *testing.T) {
	now := time.Unix(1_788_303_600, 0).UTC()
	const identity = "b130c94d-a213-463a-a797-ec124104363a"
	for name, tc := range map[string]struct {
		resource string
		identity string
		wantErr  error
	}{
		"remote provider":     {"https://mcp.linear.app/mcp", identity, ErrFirstPartyResourceNotOwned},
		"attacker host":       {"http://evil.example/mcp", identity, ErrFirstPartyResourceNotOwned},
		"loopback wrong port": {"http://127.0.0.1:9999/mcp", identity, ErrFirstPartyResourceNotOwned},
		"empty resource":      {"", identity, ErrFirstPartyResourceNotOwned},
		"non-uuid subject":    {"http://127.0.0.1:8096/mcp/", "aura-cli", nil},
	} {
		t.Run(name, func(t *testing.T) {
			server, jwtStub, refreshStub := firstPartyTestServer(t, now)
			_, err := server.IssueFirstPartyToken(t.Context(), tc.resource, tc.identity)
			if err == nil {
				t.Fatal("IssueFirstPartyToken accepted a request it must refuse")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if jwtStub.claims != nil || refreshStub.calls != 0 {
				t.Fatal("a refused request must mint and store nothing")
			}
		})
	}
}

func TestIssueFirstPartyTokenNeedsTokenServices(t *testing.T) {
	var missing *OAuthServer
	if _, err := missing.IssueFirstPartyToken(t.Context(), "http://127.0.0.1:8096/mcp/", "b130c94d-a213-463a-a797-ec124104363a"); err == nil {
		t.Fatal("a nil OAuth server must not issue a token")
	}
	unwired := &OAuthServer{tokens: &mcpTokenPlugin{}}
	if _, err := unwired.IssueFirstPartyToken(t.Context(), "http://127.0.0.1:8096/mcp/", "b130c94d-a213-463a-a797-ec124104363a"); err == nil {
		t.Fatal("an unwired token plugin must not issue a token")
	}
}
