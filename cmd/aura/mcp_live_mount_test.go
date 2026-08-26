package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestOAuthMountDeferralIsLimitedToServeBootstrap(t *testing.T) {
	oauthServer := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   "http://127.0.0.1:8093/",
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	if shouldDeferOAuthMount(context.Background(), oauthServer) {
		t.Fatal("ordinary chat and tool-pipe boots must mount OAuth servers synchronously")
	}
	if !shouldDeferOAuthMount(deferOAuthMountsUntilListener(context.Background()), oauthServer) {
		t.Fatal("serve bootstrap must defer OAuth until its listener can refresh a grant")
	}
}

func TestOAuthMountDeferralPreservesNonOAuthAndInvalidConfiguration(t *testing.T) {
	ctx := deferOAuthMountsUntilListener(context.Background())
	for name, server := range map[string]mcp.ManagedServer{
		"disabled": {
			Type: mcp.ServerTypeStreamableHTTP,
			URL:  "http://127.0.0.1:8093/",
			Env:  []string{"MCP_OAUTH_DISABLED=true"},
		},
		"static bearer": {
			Type: mcp.ServerTypeStreamableHTTP,
			URL:  "http://127.0.0.1:8093/",
			Env:  []string{"MCP_BEARER_TOKEN=fixture"},
		},
		"invalid confidential client": {
			Type: mcp.ServerTypeStreamableHTTP,
			URL:  "http://127.0.0.1:8093/",
			Env:  []string{"MCP_OAUTH_CLIENT_SECRET=fixture"},
		},
		"stdio": {Command: "fixture"},
	} {
		t.Run(name, func(t *testing.T) {
			if shouldDeferOAuthMount(ctx, server) {
				t.Fatal("server must remain on the synchronous mount path")
			}
		})
	}
}
