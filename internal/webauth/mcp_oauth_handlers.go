package webauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/chetto1983/aura/internal/mcp"
)

const mcpOAuthBodyLimit = 64 << 10

// MetadataHandler serves RFC 8414 authorization-server metadata.
func (s *OAuthServer) MetadataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	endpointBase := mcp.AuraAuthorizationServerIssuer
	if strings.EqualFold(r.Host, "aura:9080") {
		endpointBase = "http://aura:9080"
	}
	writeOAuthJSON(w, http.StatusOK, oauthex.AuthServerMeta{
		Issuer:                            mcp.AuraAuthorizationServerIssuer,
		AuthorizationEndpoint:             s.authorizationEndpoint(),
		TokenEndpoint:                     endpointBase + "/oauth/token",
		JWKSURI:                           endpointBase + "/oauth/jwks",
		RegistrationEndpoint:              endpointBase + "/oauth/register",
		ScopesSupported:                   []string{mcp.AuraOAuthToolsScope, mcp.AuraOAuthManageScope, "offline_access"},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		AuthorizationResponseIssParameterSupported: true,
	})
}

// JWKSHandler serves the public Ed25519 keys used by MCP resource servers.
func (s *OAuthServer) JWKSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	keys, err := s.tokens.keys.GetJWKSWithFallback(r.Context())
	if err != nil {
		http.Error(w, "jwks unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeOAuthJSON(w, http.StatusOK, keys)
}

// RegistrationHandler registers Aura's public PKCE client through RFC 7591.
func (s *OAuthServer) RegistrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpOAuthBodyLimit)
	var metadata oauthex.ClientRegistrationMetadata
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid registration metadata")
		return
	}
	if len(metadata.RedirectURIs) == 0 || metadata.TokenEndpointAuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Aura accepts public PKCE clients only")
		return
	}
	for _, redirect := range metadata.RedirectURIs {
		if !s.redirectAllowed(redirect) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect URI is outside Aura's callbacks")
			return
		}
	}
	metadata.ClientName = "Aura"
	metadata.TokenEndpointAuthMethod = "none"
	// The SDK's RFC 7591 timestamp serializer has a pointer receiver; passing a value
	// falls back to time.Time's RFC 3339 JSON and breaks every conforming MCP client.
	writeOAuthJSON(w, http.StatusCreated, &oauthex.ClientRegistrationResponse{
		ClientRegistrationMetadata: metadata,
		ClientID:                   mcpOAuthClientID,
		ClientIDIssuedAt:           s.now().UTC(),
	})
}

// AuthorizationHandler authenticates the cockpit session and issues a one-time
// authorization code bound to the requested MCP resource and callback.
func (s *OAuthServer) AuthorizationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	redirect := query.Get("redirect_uri")
	target, redirectOK := s.validatedRedirect(redirect)
	resource := query.Get("resource")
	scope, scopeOK := selectScopes(query.Get("scope"))
	if query.Get("response_type") != "code" || query.Get("client_id") != mcpOAuthClientID ||
		query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" ||
		!scopeOK || !redirectOK || !allowedMCPResource(resource) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid OAuth authorization request")
		return
	}
	identity, err := s.validator.SessionIdentity(r)
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	code, err := randomOAuthValue()
	if err != nil {
		http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
		return
	}
	s.storeCode(code, oauthAuthorizationCode{
		identityID: identity.IdentityID, userID: identity.UserID, sessionID: identity.SessionID,
		clientID: mcpOAuthClientID, redirectURI: redirect, resource: resource, scope: scope,
		codeChallenge: query.Get("code_challenge"), expiresAt: s.now().Add(mcpOAuthCodeTTL),
	})
	values := target.Query()
	values.Set("code", code)
	values.Set("state", query.Get("state"))
	values.Set("iss", mcp.AuraAuthorizationServerIssuer)
	target.RawQuery = values.Encode()
	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusFound)
}

// TokenHandler exchanges authorization codes or refresh tokens for MCP access tokens.
func (s *OAuthServer) TokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpOAuthBodyLimit)
	if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != mcpOAuthClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid token request")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r)
	case "refresh_token":
		s.exchangeRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant type")
	}
}

func (s *OAuthServer) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code, ok := s.consumeCode(r.Form.Get("code"))
	if !ok || code.clientID != r.Form.Get("client_id") || code.redirectURI != r.Form.Get("redirect_uri") ||
		!validCodeVerifier(r.Form.Get("code_verifier"), code.codeChallenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	extra := mcpTokenClaims(code.resource, code.scope, code.clientID, code.identityID)
	pair, err := s.tokens.jwt.GenerateUserToken(r.Context(), code.userID, code.sessionID, extra)
	if err != nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "token generation failed")
		return
	}
	if err := s.tokens.refresh.StoreInitialRefreshToken(r.Context(), pair.RefreshToken, code.sessionID,
		s.now().Add(7*24*time.Hour)); err != nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token storage failed")
		return
	}
	writeTokenResponse(w, pair.AccessToken, pair.RefreshToken, int64(pair.ExpiresIn.Seconds()), code.scope)
}

func (s *OAuthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.Form.Get("refresh_token")
	claims, err := s.tokens.jwt.ExtractClaims(r.Context(), refreshToken)
	clientID, _ := claims["client_id"].(string)
	if err != nil {
		slog.Warn("mcp oauth server: refresh token rejected", "stage", "decode_claims", "error", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	if clientID != mcpOAuthClientID {
		slog.Warn("mcp oauth server: refresh token rejected", "stage", "client_binding")
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	pair, err := s.tokens.refresh.RefreshTokens(r.Context(), refreshToken)
	if err != nil {
		slog.Warn("mcp oauth server: refresh token rejected", "stage", "rotation", "error", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	scope, _ := claims["scope"].(string)
	writeTokenResponse(w, pair.AccessToken, pair.RefreshToken, 900, scope)
}

func (s *OAuthServer) storeCode(value string, code oauthAuthorizationCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, existing := range s.codes {
		if !existing.expiresAt.After(s.now()) {
			delete(s.codes, key)
		}
	}
	s.codes[value] = code
}

func (s *OAuthServer) consumeCode(value string) (oauthAuthorizationCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[value]
	delete(s.codes, value)
	return code, ok && code.expiresAt.After(s.now())
}

func validCodeVerifier(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:]) == challenge
}

func writeTokenResponse(w http.ResponseWriter, access, refresh string, expires int64, scope string) {
	w.Header().Set("Cache-Control", "no-store")
	writeOAuthJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "refresh_token": refresh, "token_type": "Bearer",
		"expires_in": expires, "scope": scope,
	})
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeOAuthJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeOAuthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
