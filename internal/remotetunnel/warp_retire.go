package remotetunnel

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// errBlindToken stops a retirement whose token cannot see the account: every denial it meets
// says nothing about one resource, so nothing is listed as abandoned and no ID is dropped.
var errBlindToken = errors.New("remote access: token does not see the account")

// retireWARP deletes the WARP leg older installs still hold: the second hostname, its Access
// application and policy, and the Gateway posture check. Every deletion passes the same ownership
// predicates as teardown, so a foreign resource stops the pass instead of being removed. A
// resource the token is denied is listed as abandoned and forgotten, so it cannot block the
// public address; a token that sees no account leaves everything for a later pass.
func (r *Reconciler) retireWARP(ctx context.Context, s *State) error {
	res := &s.Resources
	if res.LegacyWARPHostname == "" {
		return nil
	}
	if res.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	warp := route{"warp", res.LegacyWARPHostname, &res.WARPAppID, &res.WARPPolicyID, &res.WARPDNSID}
	steps := append(r.routeDeletions(ctx, s, warp, true), deletion{&res.GatewayPostureID, func() error {
		p, err := r.cloud.GetPosture(ctx, res.AccountID, res.GatewayPostureID)
		if err != nil {
			return err
		}
		if !ownedPosture(s, p) {
			return ErrOwnershipConflict
		}
		return nil
	}, func() error {
		return r.cloud.DeletePosture(ctx, res.AccountID, res.GatewayPostureID, resourceName(s, "gateway"))
	}, []AbandonedResource{{Kind: "posture", Name: resourceName(s, "gateway")}}, nil})
	if err := r.runDeletions(ctx, s, steps); err != nil {
		if errors.Is(err, errBlindToken) {
			return nil
		}
		return err
	}
	res.LegacyWARPHostname = ""
	return nil
}

func (r *Reconciler) accountVisible(ctx context.Context, s *State) (bool, error) {
	accounts, err := r.cloud.ListAccounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range accounts {
		if a.ID == s.Resources.AccountID {
			return true, nil
		}
	}
	return false, nil
}

func ownedPosture(s *State, p cloudflareapi.Posture) bool {
	return validOwner(s.TunnelName) && p.ID == s.Resources.GatewayPostureID && p.Name == resourceName(s, "gateway") && p.Type == "gateway"
}
