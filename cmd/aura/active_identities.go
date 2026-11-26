package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/identityctx"
)

// eachActiveIdentity runs fn once for every active identity, with the identity on ctx. One owner's
// error does not stop the others; the errors come back joined, each naming its owner.
func eachActiveIdentity(ctx context.Context, identities runtimeIdentityLister, fn func(ctx context.Context, ownerID string) error) error {
	owners, err := identities.ListIdentities(ctx)
	if err != nil {
		return fmt.Errorf("list identities: %w", err)
	}
	var errs []error
	for _, owner := range owners {
		if owner.ID == "" || owner.Deactivated {
			continue
		}
		if err := fn(identityctx.WithIdentityID(ctx, owner.ID), owner.ID); err != nil {
			errs = append(errs, fmt.Errorf("identity %s: %w", owner.ID, err))
		}
	}
	return errors.Join(errs...)
}
