package remotetunnel

import "context"

// PersistedAcceptance carries the browser's authenticated tunnel-path proof across restarts.
// SaveDesired and every unhealthy observation invalidate it in the same state generation.
type PersistedAcceptance struct{ Store StateStore }

// Ready admits only previously accepted evidence for the current desired generation.
func (a PersistedAcceptance) Ready(ctx context.Context, state State) (bool, error) {
	persisted, err := a.Store.Load(ctx)
	return err == nil && persisted.Generation == state.Generation && persisted.ObservedHealthy, err
}

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
	state.ObservedHealthy = true
	state.Phase = PhaseHealthy
	state.LastError = ""
	return r.persist(ctx, &state)
}

// ValidateDesired shares the reconciler's DNS/account validation with configuration writes.
func ValidateDesired(desired Desired) error { return validateDesired(desired) }
