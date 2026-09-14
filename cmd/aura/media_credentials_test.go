package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/runner"
)

// snapshotFunc adapts a plain function to snapshotResolver for a local stub —
// no real secret is ever constructed or compared in this file's output.
type snapshotFunc func(context.Context, string) (llm.RuntimeSnapshot, error)

func (f snapshotFunc) SnapshotFor(ctx context.Context, id string) (llm.RuntimeSnapshot, error) {
	return f(ctx, id)
}

func TestMediaCredentialsRefusesCreditSentinel(t *testing.T) {
	exhausted := creditExhaustedClient{}
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{
				Client: exhausted,
				Config: llm.Config{BaseURL: "https://openrouter.ai/api/v1"},
			}, nil
		}),
	}
	base, key, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_credit" || base != "" || key != "" {
		t.Fatalf("credit refusal = %q base=%q key=%q, want no_credit and no usable credential", mediagen.ErrorCode(err), base, key)
	}
}

func TestMediaCredentialsRefusesExplicitNoCreditError(t *testing.T) {
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{}, identitykey.ErrNoCredit
		}),
	}
	_, _, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_credit" {
		t.Fatalf("ErrorCode = %q, want no_credit", mediagen.ErrorCode(err))
	}
}

func TestMediaCredentialsRefusesNoIdentityKey(t *testing.T) {
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{}, runner.ErrNoIdentityLLMKey
		}),
	}
	_, _, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_key" {
		t.Fatalf("ErrorCode = %q, want no_key", mediagen.ErrorCode(err))
	}
}

func TestMediaCredentialsRefusesNilResolver(t *testing.T) {
	port := mediaCredentials{}
	base, key, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_key" || base != "" || key != "" {
		t.Fatalf("nil resolver = %q base=%q key=%q, want no_key and no usable credential", mediagen.ErrorCode(err), base, key)
	}
}

func TestMediaCredentialsRefusesEmptyOwner(t *testing.T) {
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			t.Fatal("resolver must not be called for an empty owner")
			return llm.RuntimeSnapshot{}, nil
		}),
	}
	_, _, err := port.For(context.Background(), "")
	if mediagen.ErrorCode(err) != "no_key" {
		t.Fatalf("ErrorCode = %q, want no_key", mediagen.ErrorCode(err))
	}
}

// TestMediaCredentialsRefusesKeylessLocalRoute pins the D-13 local-backend
// exemption's snapshot: a deployment key riding along on a keyless local route
// (e.g. the D-13 exemption snapshot) is still refused, because a local backend
// bills nothing and has no credential a generation call can present upstream.
func TestMediaCredentialsRefusesKeylessLocalRoute(t *testing.T) {
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{
				Config: llm.Config{Provider: "llamacpp", BaseURL: "http://localhost:8080", APIKey: "unused-deployment-key"},
			}, nil
		}),
	}
	base, key, err := port.For(context.Background(), "owner")
	if mediagen.ErrorCode(err) != "no_key" || base != "" || key != "" {
		t.Fatalf("local route = %q base=%q key=%q, want no_key and no usable credential", mediagen.ErrorCode(err), base, key)
	}
}

func TestMediaCredentialsReturnsValidOwnerKey(t *testing.T) {
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{
				Config: llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-or-v1-owner"},
			}, nil
		}),
	}
	base, key, err := port.For(context.Background(), "owner")
	if err != nil || base != "https://openrouter.ai/api/v1" || key != "sk-or-v1-owner" {
		t.Fatalf("For() = %q %q %v, want the owner's own OpenRouter credential", base, key, err)
	}
}

// TestMediaCredentialsPropagatesUnrelatedStoreFailure proves a plain
// infrastructure error surfaces unchanged: it must never be fabricated into
// no_credit just because it came back alongside an empty snapshot.
func TestMediaCredentialsPropagatesUnrelatedStoreFailure(t *testing.T) {
	want := errors.New("identitykey: store unavailable")
	port := mediaCredentials{
		resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
			return llm.RuntimeSnapshot{}, want
		}),
	}
	_, _, err := port.For(context.Background(), "owner")
	if !errors.Is(err, want) {
		t.Fatalf("For() error = %v, want the store failure propagated unchanged", err)
	}
	if mediagen.ErrorCode(err) != "job_failed" {
		t.Fatalf("ErrorCode = %q, want job_failed for an infrastructure failure", mediagen.ErrorCode(err))
	}
}
