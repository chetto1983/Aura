package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpoauth"
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

// A slow remote MCP must not hold the appliance's required memory mount behind its
// network timeout. Within each ownership class the order stays deterministic.
func TestDeferredOAuthMountsPrioritizeFirstPartyServers(t *testing.T) {
	remote := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   "https://mcp.linear.app/mcp",
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	policies := map[string]mcp.ManagedServer{
		"z-remote": remote,
		"memory":   firstPartyMemoryServer(t),
		"a-remote": remote,
	}

	got := deferredOAuthMountNames(policies)
	want := []string{"memory", "a-remote", "z-remote"}
	if !slices.Equal(got, want) {
		t.Fatalf("mount order = %v, want first-party servers before remote servers %v", got, want)
	}
}

type fakeOwnerStore struct {
	owners     []string
	ownerOfHit int
}

func (f *fakeOwnerStore) OwnersOf(_ context.Context, serverName string, candidates []string) ([]string, error) {
	out := make([]string, 0, len(f.owners))
	for _, candidate := range candidates {
		for _, owner := range f.owners {
			if candidate == owner {
				out = append(out, candidate)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %q", mcpoauth.ErrNoGrant, serverName)
	}
	return out, nil
}

func (f *fakeOwnerStore) OwnerOf(ctx context.Context, serverName string, candidates []string) (string, error) {
	f.ownerOfHit++
	owners, err := f.OwnersOf(ctx, serverName, candidates)
	if err != nil {
		return "", err
	}
	if len(owners) > 1 {
		return "", fmt.Errorf("%w: %q", mcpoauth.ErrAmbiguousOwner, serverName)
	}
	return owners[0], nil
}

func firstPartyMemoryServer(t *testing.T) mcp.ManagedServer {
	t.Helper()
	recipe, ok := mcpmanager.LookupCatalog("memory")
	if !ok {
		t.Fatal("catalog lost the memory recipe")
	}
	return recipe.Server
}

// Enrolling a second person must not unmount Aura's own memory for everybody. The owner
// is the first candidate, and ListIdentities orders by created_at, so the pick is the
// deployment's oldest identity and it is the same on every boot.
func TestFirstPartyMountSurvivesSeveralGrantOwners(t *testing.T) {
	const oldest, newer = "b130c94d-a213-463a-a797-ec124104363a", "9f1c0f2e-1111-4b2a-9d3e-0f0a0b0c0d0e"
	store := &fakeOwnerStore{owners: []string{oldest, newer}}

	owner, err := mcpGrantOwner(context.Background(), store, "memory", firstPartyMemoryServer(t), []string{oldest, newer})
	if err != nil {
		t.Fatalf("mcpGrantOwner: %v", err)
	}
	if owner != oldest {
		t.Fatalf("owner = %q, want the oldest identity %q", owner, oldest)
	}
	if store.ownerOfHit != 0 {
		t.Fatal("a first-party server must not go through the single-owner lookup")
	}
	if _, err := mcpGrantOwner(context.Background(), store, "memory", firstPartyMemoryServer(t), []string{"33333333-3333-4333-8333-333333333333"}); !errors.Is(err, mcpoauth.ErrNoGrant) {
		t.Fatalf("err = %v, want ErrNoGrant when nobody holds one", err)
	}
}

// The ambiguity refusal is what stops one person's remote tools running on another
// person's token, and it must stay exactly as it was for everything Aura does not ship.
func TestThirdPartyMountStillRefusesTwoGrantOwners(t *testing.T) {
	const first, second = "b130c94d-a213-463a-a797-ec124104363a", "9f1c0f2e-1111-4b2a-9d3e-0f0a0b0c0d0e"
	store := &fakeOwnerStore{owners: []string{first, second}}
	remote := mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.linear.app/mcp",
		Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}

	if _, err := mcpGrantOwner(context.Background(), store, "linear", remote, []string{first, second}); !errors.Is(err, mcpoauth.ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
	}
	owner, err := mcpGrantOwner(context.Background(), store, "linear", remote, []string{first})
	if err != nil || owner != first {
		t.Fatalf("mcpGrantOwner = (%q, %v), want (%q, nil)", owner, err, first)
	}
}
