package idempotency

import (
	"context"
	"errors"
)

// Temporary RED shim: it keeps the contract tests compilable for pre-commit
// checks. GREEN removes this file and supplies the production implementation.
var ErrIdentityMismatch = errors.New("operation identity does not match trusted identity")

type Operation struct {
	Key         OperationKey
	Fingerprint [32]byte
	Correlation string
}

func WithOperation(ctx context.Context, _ Operation) (context.Context, error) { return ctx, nil }

func OperationFromContext(context.Context) (Operation, bool) { return Operation{}, false }
