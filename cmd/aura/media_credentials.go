package main

import (
	"context"
	"errors"
	"strings"

	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/runner"
)

// snapshotResolver is the seam mediaCredentials needs onto the daemon's one
// per-identity LLM resolver (identityLLMResolver): identityLLMResolver(chat)'s
// *runner.IdentityLLMResolver satisfies it structurally. Narrowed so a test can
// substitute a stub without a live pool.
type snapshotResolver interface {
	SnapshotFor(context.Context, string) (llm.RuntimeSnapshot, error)
}

// mediaCredentials implements mediagen.MediaCredentials over the daemon's
// singleton identity LLM resolver, so a cap change invalidated on that resolver
// also invalidates the media path's view of the same identity — there is no
// second credit decision to keep in sync.
type mediaCredentials struct {
	resolver snapshotResolver
}

var _ mediagen.MediaCredentials = mediaCredentials{}

// For resolves owner's OpenRouter base URL + API key, refusing with a
// *mediagen.Error when generation is unavailable: no_key (no resolver, empty
// owner, no stored key, or a route that is not billable OpenRouter — including
// a keyless local backend, which is legitimate for chat but has no credential
// this port can hand a generation call) and no_credit (the identity's cap is
// exhausted, recognized either from identitykey.ErrNoCredit or from the
// snapshot carrying cmd/aura's creditExhaustedClient sentinel — the resolver
// caches DecisionRefuseNoCredit as a snapshot with a nil error, so the client
// type is the only signal at that point). Any other resolver error is returned
// unchanged: an infrastructure failure is never fabricated into no_credit.
func (p mediaCredentials) For(ctx context.Context, owner string) (string, string, error) {
	if p.resolver == nil || strings.TrimSpace(owner) == "" {
		return "", "", &mediagen.Error{Code: "no_key", Message: "No identity credential is available."}
	}
	snap, err := p.resolver.SnapshotFor(ctx, owner)
	_, noCredit := snap.Client.(creditExhaustedClient)
	if errors.Is(err, identitykey.ErrNoCredit) || (err == nil && noCredit) {
		return "", "", &mediagen.Error{Code: "no_credit", Message: "This identity has no generation credit."}
	}
	if errors.Is(err, runner.ErrNoIdentityLLMKey) {
		return "", "", &mediagen.Error{Code: "no_key", Message: "Connect this identity to OpenRouter."}
	}
	if err != nil {
		return "", "", err
	}
	if !openRouterMediaRoute(snap.Config) || strings.TrimSpace(snap.Config.APIKey) == "" {
		return "", "", &mediagen.Error{Code: "no_key", Message: "Generation requires the OpenRouter route."}
	}
	return snap.Config.BaseURL, snap.Config.APIKey, nil
}

// openRouterMediaRoute reports whether cfg is a route generation can run on: OpenRouter, and
// not a keyless local host even when it is labelled openrouter. The credential port and the
// settings picker's catalog decide it here, so they never disagree about a route.
func openRouterMediaRoute(cfg llm.Config) bool {
	return llm.ReasoningTarget(cfg.Provider, cfg.BaseURL) == llm.ReasoningTargetOpenRouter &&
		!llm.IsKeylessLocalBaseURL(cfg.BaseURL)
}
