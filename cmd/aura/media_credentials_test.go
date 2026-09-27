package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/mediagen"
)

// keyStoreFunc adapts a plain function to mediaKeyStore for a local stub — no real secret is
// ever constructed or compared in this file's output.
type keyStoreFunc func(context.Context) (identitykey.Record, error)

func (f keyStoreFunc) Load(ctx context.Context) (identitykey.Record, error) { return f(ctx) }

func storedKey(limitUSD *float64) keyStoreFunc {
	return func(context.Context) (identitykey.Record, error) {
		return identitykey.Record{Key: "sk-or-v1-owner", LimitUSD: limitUSD}, nil
	}
}

func TestMediaCredentialsRefusesAnExhaustedCap(t *testing.T) {
	for name, limit := range map[string]float64{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			base, key, err := mediaCredentials{keys: storedKey(&limit)}.For(context.Background(), "owner")
			if mediagen.ErrorCode(err) != "no_credit" || base != "" || key != "" {
				t.Fatalf("cap %v = %q base=%q key=%q, want no_credit and no usable credential", limit, mediagen.ErrorCode(err), base, key)
			}
		})
	}
}

func TestMediaCredentialsRefusesAnIdentityWithNoKey(t *testing.T) {
	port := mediaCredentials{keys: keyStoreFunc(func(context.Context) (identitykey.Record, error) {
		return identitykey.Record{}, identitykey.ErrNoKey
	})}
	base, key, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_key" || base != "" || key != "" {
		t.Fatalf("no stored key = %q base=%q key=%q, want no_key and no usable credential", mediagen.ErrorCode(err), base, key)
	}
}

func TestMediaCredentialsRefusesWithoutAKeyStore(t *testing.T) {
	base, key, err := mediaCredentials{}.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_key" || base != "" || key != "" {
		t.Fatalf("no key store = %q base=%q key=%q, want no_key and no usable credential", mediagen.ErrorCode(err), base, key)
	}
}

func TestMediaCredentialsRefusesEmptyOwner(t *testing.T) {
	port := mediaCredentials{keys: keyStoreFunc(func(context.Context) (identitykey.Record, error) {
		t.Fatal("the key store must not be read for an empty owner")
		return identitykey.Record{}, nil
	})}
	if _, _, err := port.For(context.Background(), "  "); mediagen.ErrorCode(err) != "no_key" {
		t.Fatalf("ErrorCode = %q, want no_key", mediagen.ErrorCode(err))
	}
}

// TestMediaCredentialsReadsTheOwnersKeyOnOpenRouter pins the split: the port has no view of
// the chat route at all, so an identity whose chat runs on Ollama still generates on OpenRouter
// with its own key, read under its own identity scope.
func TestMediaCredentialsReadsTheOwnersKeyOnOpenRouter(t *testing.T) {
	positive := 5.0
	for name, limit := range map[string]*float64{"no limit": nil, "positive cap": &positive} {
		t.Run(name, func(t *testing.T) {
			port := mediaCredentials{keys: keyStoreFunc(func(ctx context.Context) (identitykey.Record, error) {
				if scoped := identityctx.IdentityID(ctx); scoped != "owner" {
					t.Fatalf("key read scoped to %q, want the owner", scoped)
				}
				return storedKey(limit)(ctx)
			})}
			base, key, err := port.For(context.Background(), " owner ")
			if err != nil || base != "https://openrouter.ai/api/v1" || key != "sk-or-v1-owner" {
				t.Fatalf("For() = %q %q %v, want the owner's own key on OpenRouter", base, key, err)
			}
		})
	}
}

// TestMediaCredentialsPropagatesUnrelatedStoreFailure proves a plain infrastructure error
// surfaces unchanged: it must never be fabricated into no_key or no_credit.
func TestMediaCredentialsPropagatesUnrelatedStoreFailure(t *testing.T) {
	want := errors.New("identitykey: store unavailable")
	port := mediaCredentials{keys: keyStoreFunc(func(context.Context) (identitykey.Record, error) {
		return identitykey.Record{}, want
	})}
	_, _, err := port.For(context.Background(), "owner")
	if !errors.Is(err, want) {
		t.Fatalf("For() error = %v, want the store failure propagated unchanged", err)
	}
	if mediagen.ErrorCode(err) != "job_failed" {
		t.Fatalf("ErrorCode = %q, want job_failed for an infrastructure failure", mediagen.ErrorCode(err))
	}
}
