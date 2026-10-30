package remotetunnel

import "context"

// AcceptExternal must only be called after the HTTP boundary verifies the administrator,
// configured hostname, and the marker owned by Caddy's private tunnel listener.
func (r *Reconciler) AcceptExternal(ctx context.Context, generation int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	if state.Generation != generation {
		return ErrStaleGeneration
	}
	if !state.Desired.Enabled || state.Resources.TunnelID == "" || (state.Phase != PhaseConnecting && state.Phase != PhaseHealthy) {
		return ErrConfiguration
	}
	tunnel, err := r.cloud.GetTunnel(ctx, state.Resources.AccountID, state.Resources.TunnelID)
	if err != nil {
		return r.failed(ctx, &state, err)
	}
	if !ownedTunnel(state, tunnel) || tunnel.DeletedAt != nil {
		return r.failed(ctx, &state, ErrOwnershipConflict)
	}
	if tunnel.Status != "healthy" {
		state.ObservedHealthy = false
		state.Phase = PhaseConnecting
		if err = r.persist(ctx, &state); err != nil {
			return err
		}
		return ErrConfiguration
	}
	state.AcceptedHostname = publicHostname(state.Desired)
	state.ObservedHealthy = true
	state.Phase = PhaseHealthy
	state.Reason = ""
	return r.persist(ctx, &state)
}

// ValidateDesired shares the reconciler's DNS/account validation with configuration writes.
func ValidateDesired(desired Desired) error { return validateDesired(desired) }

func publicHostname(d Desired) string { return d.PublicLabel + "." + d.ZoneName }
