package main

import (
	"context"

	"github.com/chetto1983/aura/internal/approvalpolicies"
	"github.com/chetto1983/aura/internal/gateway"
)

// gatewayPolicies adapts the policy store to the gateway's seam, which declares its own
// Policy type so the gateway imports no store package.
type gatewayPolicies struct{ store *approvalpolicies.Store }

func (a gatewayPolicies) Get(ctx context.Context, identityID, tool, action string) (gateway.Policy, bool, error) {
	policy, ok, err := a.store.Get(ctx, identityID, tool, action)
	return gateway.Policy(policy), ok, err
}
