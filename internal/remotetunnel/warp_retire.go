package remotetunnel

import "context"

// retireWARP deletes the WARP leg older installs still hold: the second hostname, its Access
// application and policy, and the Gateway posture check. Every deletion passes the same ownership
// predicates as teardown, so a foreign resource stops the pass instead of being removed.
func (r *Reconciler) retireWARP(ctx context.Context, s *State) error {
	res := &s.Resources
	if res.LegacyWARPHostname == "" {
		return nil
	}
	if res.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	warp := route{"warp", res.LegacyWARPHostname, &res.WARPAppID, &res.WARPPolicyID, &res.WARPDNSID}
	steps := append(r.routeDeletions(ctx, s, []route{warp}), deletion{&res.GatewayPostureID, func() error {
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
	}})
	if err := r.runDeletions(ctx, s, steps); err != nil {
		return err
	}
	res.LegacyWARPHostname = ""
	return nil
}
