package main

import (
	"context"
	"errors"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
)

// mediaBaseURL is where image and video generation run: OpenRouter itself, never the chat LLM's
// base. The credential port and the settings picker's catalog both use it, so the picker fills
// the cache entry the tools read. Reported 2026-09-27: with the chat route on Ollama every
// generation refused, because credential and catalog followed the chat route, although
// OpenRouter serves media whatever the chat runs on — the embedding route's defect of
// 2026-09-23 (config_routes.go).
const mediaBaseURL = llm.DefaultBaseURL

// mediaKeyStore is identitykey.Store narrowed to the one read the media path needs; the caller
// scopes ctx to the identity.
type mediaKeyStore interface {
	Load(ctx context.Context) (identitykey.Record, error)
}

// mediaCredentials implements mediagen.MediaCredentials over the identity's own OpenRouter key.
// It never asks the chat resolver: a keyless local chat route is exempt from credit because it
// bills nothing, while every generation bills on OpenRouter. The key is read on every call, so
// a cap changed from the Credit panel applies to the next generation with nothing to invalidate.
type mediaCredentials struct {
	keys mediaKeyStore
}

var _ mediagen.MediaCredentials = mediaCredentials{}

// For resolves owner's OpenRouter credential through identitykey.Decide, the one credit
// decision, asked for a backend that always bills. It refuses with no_key when there is no key
// store, no owner or no stored key, and with no_credit when the key's cap is exhausted. A store
// failure is returned unchanged: an infrastructure error is never fabricated into a refusal.
// The services key is never used (CRED-07).
func (p mediaCredentials) For(ctx context.Context, owner string) (string, string, error) {
	owner = strings.TrimSpace(owner)
	if p.keys == nil || owner == "" {
		return "", "", &mediagen.Error{Code: "no_key", Message: "No identity credential is available."}
	}
	rec, err := p.keys.Load(identityctx.WithIdentityID(ctx, owner))
	if err != nil && !errors.Is(err, identitykey.ErrNoKey) {
		return "", "", err
	}
	decision, _ := identitykey.Decide(identitykey.DecisionInput{
		IdentityID: owner, HasKey: err == nil, LimitUSD: rec.LimitUSD, BackendBills: true,
	})
	switch decision {
	case identitykey.DecisionAllow:
		return mediaBaseURL, rec.Key, nil
	case identitykey.DecisionRefuseNoCredit:
		return "", "", &mediagen.Error{Code: "no_credit", Message: "This identity has no generation credit."}
	default:
		return "", "", &mediagen.Error{Code: "no_key", Message: "Connect this identity to OpenRouter."}
	}
}
