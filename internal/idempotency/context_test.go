package idempotency

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

func TestOperationContextRoundTripIsImmutable(t *testing.T) {
	t.Parallel()

	fingerprint := [32]byte{1, 2, 3}
	op := Operation{
		Key:         OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: ScopeHTTPMutation, Key: "web-key-1"},
		Fingerprint: fingerprint,
		ClaimToken:  ClaimToken(7),
		Correlation: "request-1",
	}
	trusted := identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity)
	ctx, err := WithOperation(trusted, op)
	if err != nil {
		t.Fatalf("WithOperation: %v", err)
	}

	got, ok := OperationFromContext(ctx)
	if !ok {
		t.Fatal("OperationFromContext did not find the attached operation")
	}
	if got != op {
		t.Fatalf("operation = %+v, want %+v", got, op)
	}

	got.Key.Key = "mutated"
	got.Fingerprint[0] = 99
	again, ok := OperationFromContext(ctx)
	if !ok || again != op {
		t.Fatalf("caller mutation changed stored operation: %+v", again)
	}
}

func TestOperationContextValidationEdges(t *testing.T) {
	t.Parallel()

	valid := Operation{
		Key:         OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: ScopeCLICommand, Key: "key"},
		Fingerprint: [32]byte{1},
	}
	tests := []Operation{
		{Key: valid.Key},
		{Key: OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: ScopeCLICommand}, Fingerprint: valid.Fingerprint},
		{Key: valid.Key, Fingerprint: valid.Fingerprint, ClaimToken: ClaimToken(-1)},
		{Key: valid.Key, Fingerprint: valid.Fingerprint, Correlation: "bad\ncorrelation"},
		{Key: valid.Key, Fingerprint: valid.Fingerprint, Correlation: strings.Repeat("x", MaxOperationKeyBytes+1)},
	}
	for _, operation := range tests {
		if err := operation.Validate(); !errors.Is(err, ErrOperationContext) {
			t.Errorf("Validate(%+v) error = %v, want ErrOperationContext", operation, err)
		}
	}
	if _, err := WithOperation(nil, valid); !errors.Is(err, ErrIdentityMismatch) { //nolint:staticcheck // explicit nil-defense contract
		t.Fatalf("WithOperation(nil) error = %v, want ErrIdentityMismatch", err)
	}
	if _, ok := OperationFromContext(nil); ok { //nolint:staticcheck // explicit nil-defense contract
		t.Fatal("nil context unexpectedly carried an operation")
	}
}

func TestOperationContextRejectsTrustedIdentityMismatch(t *testing.T) {
	t.Parallel()

	trusted := identityctx.WithIdentityID(context.Background(), "00000000-0000-0000-0000-000000000002")
	_, err := WithOperation(trusted, Operation{
		Key:         OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: ScopeHTTPMutation, Key: "web-key-1"},
		Fingerprint: [32]byte{1},
	})
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("error = %v, want ErrIdentityMismatch", err)
	}
}

func TestOperationContextAddsValidatedClaimToken(t *testing.T) {
	t.Parallel()

	trusted := identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity)
	ctx, err := WithOperation(trusted, Operation{
		Key: OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      ScopeCLICommand,
			Key:        "cli-key-1",
		},
		Fingerprint: [32]byte{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := WithClaimToken(ctx, ClaimToken(9))
	if err != nil {
		t.Fatalf("WithClaimToken: %v", err)
	}
	operation, ok := OperationFromContext(claimed)
	if !ok || operation.ClaimToken != ClaimToken(9) {
		t.Fatalf("claimed operation = %+v, present = %v", operation, ok)
	}
	if _, err := WithClaimToken(ctx, 0); !errors.Is(err, ErrOperationContext) {
		t.Fatalf("WithClaimToken(0) error = %v, want ErrOperationContext", err)
	}
}

func TestOperationContextRejectsMissingTrustedIdentity(t *testing.T) {
	t.Parallel()

	_, err := WithOperation(context.Background(), Operation{
		Key:         OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: ScopeCLICommand, Key: "cli-key-1"},
		Fingerprint: [32]byte{1},
	})
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("error = %v, want ErrIdentityMismatch", err)
	}
}
