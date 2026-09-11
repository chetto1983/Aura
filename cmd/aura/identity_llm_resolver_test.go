package main

import (
	"strings"
	"testing"
)

// TestIdentityLLMResolverIsOnePerDaemon proves every caller gets the same resolver. The credit
// API invalidates the instance it holds when an admin raises a cap; the runner held another
// one and kept serving its cached refusal (measured 2026-09-11: a topped-up member was refused
// for minutes, until the route or the daemon changed).
func TestIdentityLLMResolverIsOnePerDaemon(t *testing.T) {
	cfg := validBootConfig()
	cfg.AuthulaSecret = strings.Repeat("ab", 32)
	pool := unreachablePool(t)
	t.Cleanup(pool.Close)
	chat := &chatEnv{cfg: cfg, pool: pool}

	first := identityLLMResolver(chat)
	if first == nil {
		t.Fatal("identityLLMResolver = nil with a pool and a valid AURA_AUTHULA_SECRET")
	}
	if second := identityLLMResolver(chat); second != first {
		t.Fatal("a second caller got another resolver: an Invalidate on one leaves the other's cache stale")
	}
}

func TestIdentityLLMResolverIsNilWithoutASecret(t *testing.T) {
	pool := unreachablePool(t)
	t.Cleanup(pool.Close)
	chat := &chatEnv{cfg: validBootConfig(), pool: pool}
	chat.cfg.AuthulaSecret = ""
	if got := identityLLMResolver(chat); got != nil {
		t.Fatalf("identityLLMResolver = %v with no AURA_AUTHULA_SECRET, want nil", got)
	}
}
