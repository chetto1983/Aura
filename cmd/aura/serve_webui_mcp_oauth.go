package main

import (
	"net/http"

	"github.com/chetto1983/aura/internal/webauth"
)

type mcpOAuthProvider interface {
	MCPOAuth() *webauth.OAuthServer
}

func registerMCPOAuthRoutes(mux *http.ServeMux, provider credentialProvider) {
	configured, ok := provider.(mcpOAuthProvider)
	if !ok || configured.MCPOAuth() == nil {
		return
	}
	server := configured.MCPOAuth()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", server.MetadataHandler)
	mux.HandleFunc("GET /.well-known/openid-configuration", server.MetadataHandler)
	mux.HandleFunc("GET /oauth/jwks", server.JWKSHandler)
	mux.HandleFunc("POST /oauth/register", server.RegistrationHandler)
	mux.HandleFunc("GET /oauth/authorize", server.AuthorizationHandler)
	mux.HandleFunc("POST /oauth/token", server.TokenHandler)
}

func isPublicMCPOAuthRoute(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /.well-known/oauth-authorization-server",
		"GET /.well-known/openid-configuration",
		"GET /oauth/jwks",
		"POST /oauth/register",
		"POST /oauth/token":
		return true
	default:
		return false
	}
}
