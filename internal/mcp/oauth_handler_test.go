package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/chetto1983/aura/internal/mcpoauth"
)

func (f *fakeStore) Load(_ context.Context, _ string) (mcpoauth.Grant, error) {
	if f.loadErr != nil {
		return mcpoauth.Grant{}, f.loadErr
	}
	return f.grant, nil
}

func (f *fakeStore) Save(_ context.Context, g mcpoauth.Grant) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, g)
	return nil
}

type stubRoundTripper struct {
	seen []*http.Request
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.seen = append(s.seen, req)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func remoteServer() ManagedServer {
	return ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp"}
}

func confidentialSettings() OAuthSettings {
	return OAuthSettings{
		ClientID:         "A1",
		ClientSecret:     "s3cr3t",
		AuthorizationURL: "https://slack.com/oauth/v2/authorize",
		TokenURL:         "https://slack.com/api/oauth.v2.access",
	}
}

// The confused-deputy fix, at the layer that actually sees the secret. OAuthSettings
// refusing an unpinned secret was only half: the SDK builds its config from DISCOVERED
// endpoints, so without this the pin changed nothing about where the secret went.
func TestConfidentialClientCannotPostToAnUnpinnedEndpoint(t *testing.T) {
	t.Parallel()
	stub := &stubRoundTripper{}
	client := pinnedOAuthClient(&http.Client{Transport: stub}, confidentialSettings())

	_, err := client.Post("https://evil.example.invalid/token", "application/x-www-form-urlencoded", strings.NewReader("grant_type=refresh_token"))
	if !errors.Is(err, ErrOAuthEndpointNotPinned) {
		t.Fatalf("err = %v, want ErrOAuthEndpointNotPinned", err)
	}
	if len(stub.seen) != 0 {
		t.Fatalf("the request reached the wire anyway: %v", stub.seen[0].URL)
	}
}

func TestConfidentialClientMayPostToThePinnedTokenEndpoint(t *testing.T) {
	t.Parallel()
	stub := &stubRoundTripper{}
	client := pinnedOAuthClient(&http.Client{Transport: stub}, confidentialSettings())

	resp, err := client.Post(confidentialSettings().TokenURL, "application/x-www-form-urlencoded", strings.NewReader("grant_type=refresh_token"))
	if err != nil {
		t.Fatalf("post to the pinned endpoint: %v", err)
	}
	defer resp.Body.Close()
	if len(stub.seen) != 1 {
		t.Fatalf("requests reaching the wire = %d, want 1", len(stub.seen))
	}
}

// Discovery is a GET and carries no credential, so restricting it would break every
// confidential mount while protecting nothing. This is the line the guard must not cross.
func TestPinnedClientLeavesDiscoveryGETsAlone(t *testing.T) {
	t.Parallel()
	stub := &stubRoundTripper{}
	client := pinnedOAuthClient(&http.Client{Transport: stub}, confidentialSettings())

	resp, err := client.Get("https://mcp.notion.com/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatalf("discovery GET: %v", err)
	}
	defer resp.Body.Close()
	if len(stub.seen) != 1 {
		t.Fatalf("discovery was blocked; requests = %d", len(stub.seen))
	}
}

// A public client has no secret to leak, which is exactly why the MCP spec leaves
// dynamic registration unrestricted. Wrapping it would forbid the DCR POST and break
// Linear and Atlassian, the two providers that need no configuration at all.
func TestPublicClientIsNotRestricted(t *testing.T) {
	t.Parallel()
	base := &http.Client{Transport: &stubRoundTripper{}}
	if got := pinnedOAuthClient(base, OAuthSettings{ClientID: "A1"}); got != base {
		t.Fatal("a public client's http.Client was replaced; DCR would be blocked")
	}
	if got := pinnedOAuthClient(base, OAuthSettings{}); got != base {
		t.Fatal("an unconfigured client's http.Client was replaced")
	}
}

// Phishing, not credential theft: the consent URL also comes from discovery, so a
// hostile server can hand the human a login page the operator never approved.
func TestFetcherRefusesAnUnpinnedAuthorizationURL(t *testing.T) {
	t.Parallel()
	called := false
	fetcher := guardedFetcher("notion", confidentialSettings(), func(context.Context, *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		called = true
		return &auth.AuthorizationResult{Code: "c"}, nil
	})

	_, err := fetcher(context.Background(), &auth.AuthorizationArgs{URL: "https://evil.example.invalid/authorize"})
	if !errors.Is(err, ErrOAuthAuthorizationURLNotPinned) {
		t.Fatalf("err = %v, want ErrOAuthAuthorizationURLNotPinned", err)
	}
	if called {
		t.Fatal("the human was sent to an unpinned authorization URL")
	}
}

// The pin must compare only the parts that decide who answers. The SDK appends state,
// the PKCE challenge and scope to the authorization URL, so a byte comparison would
// reject every real flow — a guard that fires on correct input is worse than none.
func TestFetcherAcceptsThePinnedURLWithFlowParameters(t *testing.T) {
	t.Parallel()
	fetcher := guardedFetcher("notion", confidentialSettings(), func(context.Context, *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		return &auth.AuthorizationResult{Code: "c"}, nil
	})

	got, err := fetcher(context.Background(), &auth.AuthorizationArgs{
		URL: "https://slack.com/oauth/v2/authorize?state=xyz&code_challenge=abc&scope=chat%3Awrite",
	})
	if err != nil {
		t.Fatalf("pinned URL with flow parameters was refused: %v", err)
	}
	if got.Code != "c" {
		t.Fatalf("result = %+v", got)
	}
}

// A mount at boot has no human. The failure has to be an instruction, not a hang and not
// a bare 401 the caller has to interpret.
func TestUnattendedMountRefusesWithAnActionableError(t *testing.T) {
	t.Parallel()
	fetcher := guardedFetcher("notion", OAuthSettings{}, nil)
	_, err := fetcher(context.Background(), &auth.AuthorizationArgs{URL: "https://mcp.notion.com/authorize"})
	if !errors.Is(err, ErrOAuthAuthorizationRequired) {
		t.Fatalf("err = %v, want ErrOAuthAuthorizationRequired", err)
	}
	if !strings.Contains(err.Error(), "notion") {
		t.Fatalf("error does not name the server: %v", err)
	}
}

func TestSameEndpoint(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		got, pinned string
		want        bool
	}{
		"identical":            {"https://a.example/token", "https://a.example/token", true},
		"host case":            {"https://A.EXAMPLE/token", "https://a.example/token", true},
		"trailing slash":       {"https://a.example/token/", "https://a.example/token", true},
		"query ignored":        {"https://a.example/token?x=1", "https://a.example/token", true},
		"different host":       {"https://b.example/token", "https://a.example/token", false},
		"different path":       {"https://a.example/other", "https://a.example/token", false},
		"different scheme":     {"http://a.example/token", "https://a.example/token", false},
		"unparseable input":    {"ht tp://a", "https://a.example/token", false},
		"path is not a prefix": {"https://a.example/token/evil", "https://a.example/token", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := sameEndpoint(tc.got, tc.pinned); got != tc.want {
				t.Fatalf("sameEndpoint(%q, %q) = %v, want %v", tc.got, tc.pinned, got, tc.want)
			}
		})
	}
}

// The Linear/Atlassian shape: the operator configures nothing, and Aura must still build
// DCR metadata, because the SDK refuses to construct a handler with no registration
// method at all. "Zero config" is true of the operator, not of us.
func TestNoOperatorConfigProducesDynamicRegistration(t *testing.T) {
	t.Parallel()
	cfg := &auth.AuthorizationCodeHandlerConfig{}
	applyClientRegistration(cfg, OAuthSettings{Scopes: []string{"read", "write"}}, "http://127.0.0.1:9999/cb")

	if cfg.PreregisteredClient != nil {
		t.Fatal("a client was pre-registered without an operator client id")
	}
	dcr := cfg.DynamicClientRegistrationConfig
	if dcr == nil || dcr.Metadata == nil {
		t.Fatal("no dynamic client registration metadata was built")
	}
	if len(dcr.Metadata.RedirectURIs) != 1 || dcr.Metadata.RedirectURIs[0] != "http://127.0.0.1:9999/cb" {
		t.Fatalf("redirect uris = %v", dcr.Metadata.RedirectURIs)
	}
	// Without refresh_token in the advertised grant types the authorization server has
	// no reason to issue one, and every mount would need a browser again within the
	// hour.
	if !slices.Contains(dcr.Metadata.GrantTypes, "refresh_token") {
		t.Fatalf("grant types = %v, want refresh_token among them", dcr.Metadata.GrantTypes)
	}
	// A DCR client keeps no secret. Claiming a secret method makes the server mint one
	// and then reject every token request that does not send it.
	if dcr.Metadata.TokenEndpointAuthMethod != "none" {
		t.Fatalf("token endpoint auth method = %q, want none", dcr.Metadata.TokenEndpointAuthMethod)
	}
	if dcr.Metadata.Scope != "read write" {
		t.Fatalf("scope = %q, want space-separated", dcr.Metadata.Scope)
	}
}

func TestPreregisteredConfidentialClientCarriesItsSecret(t *testing.T) {
	t.Parallel()
	cfg := &auth.AuthorizationCodeHandlerConfig{}
	applyClientRegistration(cfg, confidentialSettings(), "http://127.0.0.1:9999/cb")

	if cfg.DynamicClientRegistrationConfig != nil {
		t.Fatal("dynamic registration was configured alongside a pre-registered client")
	}
	if cfg.PreregisteredClient == nil || cfg.PreregisteredClient.ClientSecretAuth == nil {
		t.Fatalf("pre-registered client = %+v", cfg.PreregisteredClient)
	}
	if cfg.PreregisteredClient.ClientSecretAuth.ClientSecret != "s3cr3t" {
		t.Fatal("the client secret did not reach the SDK config")
	}
}

// A pre-registered PUBLIC client (Notion's documented shape) must not be handed an empty
// ClientSecretAuth: the SDK's Validate rejects a secret auth method with no secret.
func TestPreregisteredPublicClientHasNoSecretAuth(t *testing.T) {
	t.Parallel()
	cfg := &auth.AuthorizationCodeHandlerConfig{}
	applyClientRegistration(cfg, OAuthSettings{ClientID: "A1"}, "http://127.0.0.1:9999/cb")

	if cfg.PreregisteredClient == nil {
		t.Fatal("no pre-registered client")
	}
	if cfg.PreregisteredClient.ClientSecretAuth != nil {
		t.Fatal("a public client was given secret auth; the SDK would reject it")
	}
	if err := cfg.PreregisteredClient.Validate(); err != nil {
		t.Fatalf("the SDK rejects the credentials we built: %v", err)
	}
}

func TestOAuthHandlerIsNotAttachedWhereItCannotApply(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		server   ManagedServer
		settings OAuthSettings
	}{
		"stdio server":       {ManagedServer{Command: "notes-mcp"}, OAuthSettings{}},
		"operator opted out": {remoteServer(), OAuthSettings{Disabled: true}},
		"static bearer wins": {
			ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp", Env: []string{"MCP_BEARER_TOKEN=static"}},
			OAuthSettings{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := oauthHandlerFor(context.Background(), "s", tc.server, tc.settings, nil, OAuthOptions{}, nil)
			if err != nil {
				t.Fatalf("oauthHandlerFor: %v", err)
			}
			if handler != nil {
				t.Fatal("an authorization flow was attached where it cannot apply")
			}
		})
	}
}

// The one that proves the whole config is acceptable to the SDK rather than merely
// well-shaped. NewAuthorizationCodeHandler validates the registration method, infers an
// application type from the redirect URIs and requires the redirect to be among them —
// three ways our DCR metadata could be rejected at mount time on the providers that need
// no operator configuration at all.
func TestBareRemoteServerProducesAHandlerTheSDKAccepts(t *testing.T) {
	t.Parallel()
	handler, err := oauthHandlerFor(context.Background(), "linear", remoteServer(), OAuthSettings{}, nil, OAuthOptions{}, nil)
	if err != nil {
		t.Fatalf("the SDK rejected the configuration we build for a zero-config server: %v", err)
	}
	if handler == nil {
		t.Fatal("no handler for a remote server; it could never authorize")
	}
	// No stored grant and no fetcher: the token source must be absent so the SDK runs
	// its flow, which then fails with the actionable unattended error.
	source, err := handler.(*auth.AuthorizationCodeHandler).TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if source != nil {
		t.Fatal("an unauthorized mount already had a token source")
	}
}

// End to end through the mount seam: a stored grant must reach the SDK as an
// InitialTokenSource, because that is the field whose absence triggers Authorize.
func TestMountWithAStoredGrantArrivesAlreadyAuthorized(t *testing.T) {
	t.Parallel()
	blob, err := resolvedClient{ClientID: "A1", TokenEndpoint: "https://api.notion.com/v1/oauth/token"}.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	store := &fakeStore{grant: mcpoauth.Grant{
		ServerName: "notion", AccessToken: "live", TokenType: "Bearer",
		ClientInfo: blob, ExpiresAt: time.Now().Add(time.Hour),
	}}

	handler, err := oauthHandlerFor(context.Background(), "notion", remoteServer(), OAuthSettings{}, nil, OAuthOptions{Store: store}, nil)
	if err != nil {
		t.Fatalf("oauthHandlerFor: %v", err)
	}
	source, err := handler.(*auth.AuthorizationCodeHandler).TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if source == nil {
		t.Fatal("the stored grant never reached the SDK; the mount would re-authorize")
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "live" {
		t.Fatalf("access token = %q, want the stored one", tok.AccessToken)
	}
}
