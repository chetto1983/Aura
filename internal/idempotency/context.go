package idempotency

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/identityctx"
)

var (
	// ErrIdentityMismatch means an operation owner did not match the trusted
	// authenticated identity already carried by the runtime context.
	ErrIdentityMismatch = errors.New("operation identity does not match trusted identity")
	// ErrOperationContext means the runtime operation metadata is malformed.
	ErrOperationContext = errors.New("invalid operation context")
)

// Operation is the immutable-by-value runtime identity for one logical mutation.
// Correlation is audit-only and MUST NOT be used as the public operation key.
type Operation struct {
	Key         OperationKey
	Fingerprint [32]byte
	Correlation string
}

// Validate checks the durable key/fingerprint contract and bounds audit-only data.
func (o Operation) Validate() error {
	if err := o.Key.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrOperationContext, err)
	}
	if o.Fingerprint == ([32]byte{}) {
		return fmt.Errorf("%w: fingerprint is required", ErrOperationContext)
	}
	if len(o.Correlation) > MaxOperationKeyBytes || containsControl(o.Correlation) {
		return fmt.Errorf("%w: correlation is invalid", ErrOperationContext)
	}
	return nil
}

type operationContextKey struct{}

// WithOperation attaches a validated operation only when its owner matches the
// trusted identity already on ctx. Values, not pointers, prevent downstream code
// from mutating the operation shared by retry attempts.
func WithOperation(ctx context.Context, operation Operation) (context.Context, error) {
	if ctx == nil {
		return nil, ErrIdentityMismatch
	}
	trustedIdentity := identityctx.IdentityID(ctx)
	if trustedIdentity == "" || trustedIdentity != operation.Key.IdentityID {
		return nil, ErrIdentityMismatch
	}
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, operationContextKey{}, operation), nil
}

// OperationFromContext returns a value copy of the trusted operation.
func OperationFromContext(ctx context.Context) (Operation, bool) {
	if ctx == nil {
		return Operation{}, false
	}
	operation, ok := ctx.Value(operationContextKey{}).(Operation)
	return operation, ok
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
