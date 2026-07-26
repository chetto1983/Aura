package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func TestValidateAdaptiveBenchmarkControlOwnerRequiresUngrantableService(
	t *testing.T,
) {
	t.Parallel()
	controlOwnerID := identityctx.CLIServiceIdentity
	lookupErr := errors.New("lookup failed")
	tests := []struct {
		name    string
		primary uuid.UUID
		store   adaptiveBenchmarkControlOwnerStoreFake
	}{
		{
			name:    "valid",
			primary: uuid.Must(uuid.NewV7()),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identity: identity.Identity{
					ID: controlOwnerID,
				},
			},
		},
		{
			name:    "same owner",
			primary: uuid.MustParse(controlOwnerID),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identity: identity.Identity{ID: controlOwnerID},
			},
		},
		{
			name:    "missing identity",
			primary: uuid.Must(uuid.NewV7()),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identityErr: lookupErr,
			},
		},
		{
			name:    "deactivated identity",
			primary: uuid.Must(uuid.NewV7()),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identity: identity.Identity{
					ID:          controlOwnerID,
					Deactivated: true,
				},
			},
		},
		{
			name:    "user grants",
			primary: uuid.Must(uuid.NewV7()),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identity: identity.Identity{ID: controlOwnerID},
				grants:   []string{"agent.run"},
			},
		},
		{
			name:    "grant lookup failure",
			primary: uuid.Must(uuid.NewV7()),
			store: adaptiveBenchmarkControlOwnerStoreFake{
				identity:  identity.Identity{ID: controlOwnerID},
				grantsErr: lookupErr,
			},
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := validateAdaptiveBenchmarkControlOwner(
				t.Context(),
				&testCase.store,
				testCase.primary,
			)
			if testCase.name == "valid" {
				if err != nil {
					t.Fatalf("valid control owner: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid control owner")
			}
		})
	}
}

type adaptiveBenchmarkControlOwnerStoreFake struct {
	identity    identity.Identity
	identityErr error
	grants      []string
	grantsErr   error
}

func (store *adaptiveBenchmarkControlOwnerStoreFake) GetIdentityByID(
	context.Context,
	string,
) (identity.Identity, error) {
	return store.identity, store.identityErr
}

func (store *adaptiveBenchmarkControlOwnerStoreFake) ListCapabilities(
	context.Context,
	string,
) ([]string, error) {
	return append([]string(nil), store.grants...), store.grantsErr
}
