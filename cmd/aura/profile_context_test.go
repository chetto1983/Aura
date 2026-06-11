package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/profile"
)

func TestProfileContextProviderReadsIdentityName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agents")
	store := profile.NewStore(root)
	if err := store.WriteProfile("local", profile.Profile{
		AgentMD: "# Agent.md\n\n## Facts\n- Name: Davide\n",
		Change:  "seed profile",
	}); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	provider := profileContextProvider(&config.Config{ProfileDir: root})
	block := provider(context.Background(), identity.Identity{
		ID: "00000000-0000-0000-0000-000000000001", Name: "local", Kind: "system",
	})
	if !strings.Contains(block, profile.ProfileBlockStart) || !strings.Contains(block, "Name: Davide") {
		t.Fatalf("profile block missing marker/content:\n%s", block)
	}
}

func TestProfileContextProviderMissingProfileIsEmpty(t *testing.T) {
	provider := profileContextProvider(&config.Config{ProfileDir: filepath.Join(t.TempDir(), "agents")})
	block := provider(context.Background(), identity.Identity{
		ID: "00000000-0000-0000-0000-000000000001", Name: "local", Kind: "system",
	})
	if block != "" {
		t.Fatalf("missing profile block = %q, want empty", block)
	}
}
