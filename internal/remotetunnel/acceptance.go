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
	if !tunnelServes(tunnel) {
		// An accepted address passed the phone test once; an outage must not ask for it again.
		if !state.Accepted() {
			state.ObservedHealthy = false
			state.Phase = PhaseConnecting
			if err = r.persist(ctx, &state); err != nil {
				return err
			}
		}
		return ErrConfiguration
	}
	state.AcceptedHostname = PublicHostname(state.Desired)
	state.ObservedHealthy = true
	state.Phase = PhaseHealthy
	state.Reason = ""
	return r.persist(ctx, &state)
}

// ValidateDesired shares the reconciler's DNS/account validation with configuration writes. A
// disabled row with no domain is the wizard's first step, which saves the account alone.
func ValidateDesired(desired Desired) error {
	if !desired.Enabled && desired.ZoneName == "" {
		if desired.AccountID == "" {
			return ErrConfiguration
		}
		return nil
	}
	return validateDesired(desired)
}

// PublicHostname is the address the operator reaches Aura at; the one place that spells it.
func PublicHostname(d Desired) string { return d.PublicLabel + "." + d.ZoneName }

// Accepted reports whether the phone test passed for the address the row now asks for.
func (s State) Accepted() bool {
	return s.AcceptedHostname != "" && s.AcceptedHostname == PublicHostname(s.Desired)
}
