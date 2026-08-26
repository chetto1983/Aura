package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// A self-issued grant is worthless unless restoreTokenSource will adopt it: without a
// token endpoint in the stored client blob that function returns (nil, nil), which reads
// as "re-authorization required" — a browser prompt nobody is at, on a sidecar Aura
// launched itself.
func TestAuraIssuedGrantIsRestorable(t *testing.T) {
	expiry := time.Now().Add(15 * time.Minute).UTC()
	grant, err := AuraIssuedGrant{
		ServerName:  "memory",
		ResourceURL: "http://127.0.0.1:8096/mcp/",
		ClientID:    "aura",
		Scopes:      []string{AuraOAuthToolsScope},
		AccessToken: "self-issued-access",
		ExpiresAt:   expiry,
	}.Grant()
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if grant.ServerName != "memory" || grant.ResourceURL != "http://127.0.0.1:8096/mcp/" {
		t.Fatalf("grant = %+v, want the memory sidecar", grant)
	}
	if grant.AccessToken != "self-issued-access" || grant.TokenType != "Bearer" {
		t.Fatalf("grant = %+v, want the self-issued Bearer token", grant)
	}
	// No refresh token: Authula foreign-keys refresh_tokens to a login session, which a
	// daemon minting for itself does not have. See webauth.IssueFirstPartyToken.
	if grant.RefreshToken != "" {
		t.Errorf("refresh token = %q, want none", grant.RefreshToken)
	}
	if !grant.ExpiresAt.Equal(expiry) {
		t.Errorf("ExpiresAt = %s, want the absolute %s", grant.ExpiresAt, expiry)
	}

	client, err := decodeResolvedClient(grant.ClientInfo)
	if err != nil {
		t.Fatalf("decode stored client: %v", err)
	}
	if client.TokenEndpoint != AuraTokenEndpoint() {
		t.Fatalf("token endpoint = %q, want %q", client.TokenEndpoint, AuraTokenEndpoint())
	}
	if client.AuthorizationEndpoint != AuraAuthorizationEndpoint() {
		t.Fatalf("authorization endpoint = %q, want %q", client.AuthorizationEndpoint, AuraAuthorizationEndpoint())
	}
	// Aura's own token endpoint reads client_id from the POST form, so autodetect would
	// cost a failed probe on every refresh a browser-issued grant makes.
	if oauth2.AuthStyle(client.AuthStyle) != oauth2.AuthStyleInParams {
		t.Errorf("auth style = %d, want AuthStyleInParams", client.AuthStyle)
	}

	source, err := restoreTokenSource(t.Context(), "memory", &fakeStore{grant: grant}, nil, nil)
	if err != nil {
		t.Fatalf("restoreTokenSource: %v", err)
	}
	if source == nil {
		t.Fatal("restoreTokenSource refused a grant Aura minted for itself")
	}
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.AccessToken != "self-issued-access" {
		t.Fatalf("access token = %q, want the stored one", token.AccessToken)
	}
}

// The blob is read back by resolvedClient, but it is also the thing an operator stares at
// during triage, so it must stay RFC 8414-shaped rather than Go-shaped.
func TestAuraIssuedGrantClientBlobUsesMetadataNames(t *testing.T) {
	grant, err := AuraIssuedGrant{
		ServerName: "calendar", ResourceURL: "http://127.0.0.1:8093/",
		ClientID: "aura", Scopes: []string{AuraOAuthToolsScope}, AccessToken: "a",
	}.Grant()
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(grant.ClientInfo, &raw); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	for _, key := range []string{"client_id", "token_endpoint", "authorization_endpoint", "auth_style", "scopes"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("stored client blob has no %q", key)
		}
	}
	if _, ok := raw["client_secret"]; ok {
		t.Error("a public first-party client must not carry a client_secret")
	}
}
