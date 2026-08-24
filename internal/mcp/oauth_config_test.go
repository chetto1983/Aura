package mcp

import (
	"errors"
	"strings"
	"testing"
)

// The zero value is the COMMON case, not an edge one: Linear and Atlassian implement dynamic
// client registration, so a correct mount for them configures nothing at all. A parser that
// treated "no oauth keys" as an error would refuse exactly the servers that need the least
// work.
func TestNoOAuthKeysIsAValidPublicClient(t *testing.T) {
	t.Parallel()
	got, err := OAuthSettingsFromEnv([]string{"MCP_HEADER_X_TEAM=acme", "UNRELATED=1"})
	if err != nil {
		t.Fatalf("OAuthSettingsFromEnv: %v", err)
	}
	if got.Preregistered() || got.Confidential() || got.Disabled {
		t.Fatalf("bare env produced a configured client: %+v", got)
	}
}

// The confused-deputy refusal. A hostile MCP server names its own authorization server in the
// protected-resource metadata it serves; posting the operator's secret to whatever endpoint
// came back would hand it over. A public client has nothing to lose and stays unrestricted.
func TestClientSecretIsRefusedWithoutPinnedEndpoints(t *testing.T) {
	t.Parallel()
	for name, env := range map[string][]string{
		"neither url": {
			"MCP_OAUTH_CLIENT_ID=A1", "MCP_OAUTH_CLIENT_SECRET=s3cr3t",
		},
		"only the authorization url": {
			"MCP_OAUTH_CLIENT_ID=A1", "MCP_OAUTH_CLIENT_SECRET=s3cr3t",
			"MCP_OAUTH_AUTHORIZATION_URL=https://slack.com/oauth/v2/authorize",
		},
		"only the token url": {
			"MCP_OAUTH_CLIENT_ID=A1", "MCP_OAUTH_CLIENT_SECRET=s3cr3t",
			"MCP_OAUTH_TOKEN_URL=https://slack.com/api/oauth.v2.access",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := OAuthSettingsFromEnv(env)
			if !errors.Is(err, ErrOAuthSecretNeedsPinnedEndpoints) {
				t.Fatalf("err = %v, want ErrOAuthSecretNeedsPinnedEndpoints", err)
			}
		})
	}
}

// The whole Slack/GitHub case: an operator-registered app, both endpoints written down.
func TestConfidentialClientWithBothEndpointsPinnedIsAccepted(t *testing.T) {
	t.Parallel()
	got, err := OAuthSettingsFromEnv([]string{
		"MCP_OAUTH_CLIENT_ID=A1",
		"MCP_OAUTH_CLIENT_SECRET=s3cr3t",
		"MCP_OAUTH_AUTHORIZATION_URL=https://slack.com/oauth/v2/authorize",
		"MCP_OAUTH_TOKEN_URL=https://slack.com/api/oauth.v2.access",
		"MCP_OAUTH_SCOPES=channels:read,chat:write",
	})
	if err != nil {
		t.Fatalf("OAuthSettingsFromEnv: %v", err)
	}
	if !got.Confidential() || !got.Preregistered() {
		t.Fatalf("settings did not read as a confidential pre-registered client: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "channels:read" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
}

func TestClientSecretWithoutClientIDIsRefused(t *testing.T) {
	t.Parallel()
	_, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_CLIENT_SECRET=s3cr3t"})
	if !errors.Is(err, ErrOAuthSecretWithoutClientID) {
		t.Fatalf("err = %v, want ErrOAuthSecretWithoutClientID", err)
	}
}

// A client_id ALONE is legitimate: that is a public pre-registered client using PKCE, which
// is what Notion's own client guidance describes. Requiring a secret alongside it would
// refuse a valid configuration.
func TestClientIDAloneIsAPublicPreregisteredClient(t *testing.T) {
	t.Parallel()
	got, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_CLIENT_ID=A1"})
	if err != nil {
		t.Fatalf("OAuthSettingsFromEnv: %v", err)
	}
	if !got.Preregistered() || got.Confidential() {
		t.Fatalf("settings = %+v, want pre-registered and public", got)
	}
}

// Pinning an endpoint is what makes the secret safe to send; pinning a plaintext one throws
// that away, because anyone on the path becomes the "known party" the operator pinned.
func TestPinnedEndpointsMustBeHTTPS(t *testing.T) {
	t.Parallel()
	_, err := OAuthSettingsFromEnv([]string{
		"MCP_OAUTH_CLIENT_ID=A1",
		"MCP_OAUTH_CLIENT_SECRET=s3cr3t",
		"MCP_OAUTH_AUTHORIZATION_URL=http://slack.com/oauth/v2/authorize",
		"MCP_OAUTH_TOKEN_URL=https://slack.com/api/oauth.v2.access",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want an https complaint", err)
	}
}

// OAuth spells scopes space-separated; an operator types commas. Guessing one costs a silent
// scope mismatch that surfaces much later as a permission error against the provider.
func TestScopesAcceptBothSpellings(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"spaces": "read write admin",
		"commas": "read,write,admin",
		"mixed":  "read, write ,admin",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_SCOPES=" + raw})
			if err != nil {
				t.Fatalf("OAuthSettingsFromEnv: %v", err)
			}
			if len(got.Scopes) != 3 || got.Scopes[0] != "read" || got.Scopes[2] != "admin" {
				t.Fatalf("scopes = %v, want [read write admin]", got.Scopes)
			}
		})
	}
	empty, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_SCOPES=   "})
	if err != nil {
		t.Fatalf("OAuthSettingsFromEnv: %v", err)
	}
	if empty.Scopes != nil {
		t.Fatalf("blank scopes = %v, want nil", empty.Scopes)
	}
}

// A typo must not silently switch OAuth off — that would look like "the server does not
// support authorization" and send someone hunting the wrong bug.
func TestDisabledOnlyAcceptsRecognizableTruth(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		got, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_DISABLED=" + value})
		if err != nil {
			t.Fatalf("OAuthSettingsFromEnv(%q): %v", value, err)
		}
		if !got.Disabled {
			t.Errorf("%q did not disable oauth", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "nope", "ture"} {
		got, err := OAuthSettingsFromEnv([]string{"MCP_OAUTH_DISABLED=" + value})
		if err != nil {
			t.Fatalf("OAuthSettingsFromEnv(%q): %v", value, err)
		}
		if got.Disabled {
			t.Errorf("%q disabled oauth; only recognizable truth may", value)
		}
	}
}

func TestUsesOAuth(t *testing.T) {
	t.Parallel()
	remote := ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp"}
	stdio := ManagedServer{Command: "notes-mcp"}

	for name, tc := range map[string]struct {
		server   ManagedServer
		settings OAuthSettings
		want     bool
	}{
		// The Linear/Atlassian case: nothing configured, and OAuth is still correct.
		"remote with no configuration at all": {remote, OAuthSettings{}, true},
		"remote with a pre-registered client": {remote, OAuthSettings{ClientID: "A1"}, true},
		"remote the operator opted out of":    {remote, OAuthSettings{Disabled: true}, false},
		// A stdio server has no HTTP request to carry an Authorization header on.
		"stdio server": {stdio, OAuthSettings{}, false},
		// An explicit static token is an instruction, and starting a browser flow over the
		// top of it would be Aura overriding the operator.
		"remote with a static bearer token": {
			ManagedServer{
				Type: ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp",
				Env: []string{"MCP_BEARER_TOKEN=static"},
			},
			OAuthSettings{}, false,
		},
		// A static HEADER is not the same statement: an operator may pin a routing or tenant
		// header and still expect the identity's own OAuth token to authenticate them.
		"remote with a static non-auth header": {
			ManagedServer{
				Type: ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp",
				Env: []string{"MCP_HEADER_X_TEAM=acme"},
			},
			OAuthSettings{}, true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := UsesOAuth(tc.server, tc.settings); got != tc.want {
				t.Fatalf("UsesOAuth = %v, want %v", got, tc.want)
			}
		})
	}
}

// An unclassifiable config must not open a browser. The mount path deliberately resolves the
// same ambiguity the other way so it can surface the classification error; this one must not
// inherit that, and the two directions are easy to conflate.
func TestUnclassifiableServerDoesNotGetAnAuthorizationFlow(t *testing.T) {
	t.Parallel()
	// Both a command and a URL: Classify refuses it as ambiguous.
	ambiguous := ManagedServer{Command: "notes-mcp", URL: "https://mcp.example.invalid/mcp"}
	if _, _, err := Classify(ambiguous); err == nil {
		t.Fatal("fixture is no longer ambiguous — Classify accepted it, so this test proves nothing")
	}
	if UsesOAuth(ambiguous, OAuthSettings{}) {
		t.Fatal("an unclassifiable server was given an authorization flow")
	}
}
