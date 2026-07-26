package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type adaptiveBenchmarkControlOwnerStore interface {
	GetIdentityByID(
		context.Context,
		string,
	) (identity.Identity, error)
	ListCapabilities(context.Context, string) ([]string, error)
}

func validateAdaptiveBenchmarkControlOwner(
	ctx context.Context,
	store adaptiveBenchmarkControlOwnerStore,
	primaryOwnerID uuid.UUID,
) error {
	controlOwnerID := uuid.MustParse(identityctx.CLIServiceIdentity)
	if store == nil ||
		primaryOwnerID == uuid.Nil ||
		primaryOwnerID == controlOwnerID {
		return errors.New(
			"adaptive benchmark control owner scope is invalid",
		)
	}
	controlIdentity, err := store.GetIdentityByID(
		ctx,
		controlOwnerID.String(),
	)
	if err != nil {
		return fmt.Errorf(
			"adaptive benchmark CLI service identity is unavailable: %w",
			err,
		)
	}
	if controlIdentity.ID != controlOwnerID.String() ||
		controlIdentity.Deactivated {
		return errors.New(
			"adaptive benchmark CLI service identity is invalid",
		)
	}
	grants, err := store.ListCapabilities(
		ctx,
		controlOwnerID.String(),
	)
	if err != nil {
		return fmt.Errorf(
			"read adaptive benchmark CLI service grants: %w",
			err,
		)
	}
	if len(grants) != 0 {
		return errors.New(
			"adaptive benchmark CLI service identity has user grants",
		)
	}
	return nil
}
