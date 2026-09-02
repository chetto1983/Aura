package webauth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	mcpOAuthClientID = "aura"
	mcpOAuthCodeTTL  = 5 * time.Minute
)

type oauthAuthorizationCode struct {
	identityID    string
	userID        string
	sessionID     string
	clientID      string
	redirectURI   string
	resource      string
	scope         string
	codeChallenge string
	expiresAt     time.Time
}

// OAuthServer implements Aura's standard authorization-code and refresh-token
// endpoints for the built-in MCP resource servers.
type OAuthServer struct {
	publicURL string
	tokens    *mcpTokenPlugin
	validator *Validator

	mu    sync.Mutex
	codes map[string]oauthAuthorizationCode
	now   func() time.Time
}

func newOAuthServer(publicURL string, tokens *mcpTokenPlugin, validator *Validator) (*OAuthServer, error) {
	if tokens == nil || tokens.jwt == nil || tokens.refresh == nil || tokens.keys == nil {
		return nil, errors.New("webauth: MCP token services are unavailable")
	}
	trimmed := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if trimmed != "" {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, errors.New("webauth: AURA_WEB_PUBLIC_URL must be an absolute URL for MCP OAuth")
		}
	}
	return &OAuthServer{
		publicURL: trimmed,
		tokens:    tokens,
		validator: validator,
		codes:     make(map[string]oauthAuthorizationCode),
		now:       time.Now,
	}, nil
}

// mcpTokenClaims is the ONE claim shape every Aura-issued MCP access token carries, and
// the reason it is a function rather than a literal in each caller: the browser exchange
// and the first-party self-issuance mint tokens for the same three resource servers, and
// those servers validate iss, aud, scope and sub exactly. A claim that drifted in one
// path would be a token the sidecar silently refuses, diagnosed as a transport fault.
//
// subject lands in mcp_subject, which subjectTokenService promotes to the standard `sub`
// — the only tenant selector the resource servers read (Amendment #147).
func mcpTokenClaims(resource, scope, clientID, subject string) map[string]any {
	return map[string]any{
		"iss": mcp.AuraAuthorizationServerIssuer, "aud": resource, "scope": scope,
		"client_id": clientID, mcpSubjectClaim: subject,
	}
}

func (s *OAuthServer) authorizationEndpoint() string {
	if s.publicURL != "" {
		return s.publicURL + "/oauth/authorize"
	}
	return mcp.AuraAuthorizationServerIssuer + "/oauth/authorize"
}

func (s *OAuthServer) redirectAllowed(raw string) bool {
	_, ok := s.validatedRedirect(raw)
	return ok
}

func (s *OAuthServer) validatedRedirect(raw string) (*url.URL, bool) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || raw != trimmed || parsed.Fragment != "" || parsed.User != nil ||
		parsed.Scheme == "" || parsed.Host == "" {
		return nil, false
	}
	// Any loopback callback is accepted, on any port and any path. RFC 8252 §7.3 makes
	// that the redirect for native and public clients precisely because they cannot
	// pre-register a fixed one: an MCP client picks a free port at launch and invents
	// its own path. Pinning two hardcoded paths meant Dynamic Client Registration
	// existed but rejected every real client -- measured 2026-09-02, Claude Code's
	// registration came back "invalid_redirect_uri: redirect URI is outside Aura's
	// callbacks", so the mount could never be authorized at all.
	//
	// The exposure this adds is bounded to a process already running on the operator's
	// own machine, and the code it could intercept is still useless without the PKCE
	// verifier, which the authorization endpoint requires as S256.
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return parsed, true
	}
	switch parsed.Path {
	case "/api/governance/mcp/authorization/callback":
		if s.publicURL == "" {
			if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
				return parsed, true
			}
			break
		}
		base, _ := url.Parse(s.publicURL)
		if strings.EqualFold(parsed.Scheme, base.Scheme) && strings.EqualFold(parsed.Host, base.Host) {
			return parsed, true
		}
	}
	return nil, false
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func allowedMCPResource(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	switch {
	case host == "aura-pim-mcp" && parsed.Port() == "8080" && path == "":
		return true
	case host == "whatsapp" && parsed.Port() == "8080" && path == "/mcp":
		return true
	case host == "aura-arcadedb-mcp" && parsed.Port() == "8096" && path == "/mcp":
		return true
	case isLoopbackHost(host) && parsed.Port() == "8093" && path == "":
		return true
	case isLoopbackHost(host) && parsed.Port() == "8092" && path == "/mcp":
		return true
	case isLoopbackHost(host) && parsed.Port() == "8096" && path == "/mcp":
		return true
	default:
		return false
	}
}

func selectScopes(raw string) (string, bool) {
	requested := strings.Fields(raw)
	if len(requested) == 0 {
		return mcp.AuraOAuthToolsScope, true
	}
	seen := make(map[string]struct{}, len(requested))
	for _, scope := range requested {
		switch scope {
		case mcp.AuraOAuthToolsScope, mcp.AuraOAuthManageScope, "offline_access":
			seen[scope] = struct{}{}
		default:
			return "", false
		}
	}
	ordered := make([]string, 0, 3)
	for _, scope := range []string{mcp.AuraOAuthToolsScope, mcp.AuraOAuthManageScope, "offline_access"} {
		if _, ok := seen[scope]; ok {
			ordered = append(ordered, scope)
		}
	}
	return strings.Join(ordered, " "), true
}

func randomOAuthValue() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
