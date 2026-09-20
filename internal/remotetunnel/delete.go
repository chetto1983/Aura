package remotetunnel

import (
	"context"
	"slices"
)

type deletion struct {
	id     *string
	read   func() error
	remove func() error
}

func (r *Reconciler) deletions(ctx context.Context, s *State) []deletion {
	steps := []deletion{}
	account := s.Resources.AccountID
	rs := routes(s)
	for _, route := range slices.Backward(rs) {
		steps = append(steps, deletion{route.dns, func() error {
			d, err := r.cloud.GetDNS(ctx, s.Resources.ZoneID, *route.dns)
			if err != nil {
				return err
			}
			if !ownedDNS(s, route, d) {
				return ErrOwnershipConflict
			}
			return nil
		}, func() error { return r.cloud.DeleteDNS(ctx, s.Resources.ZoneID, *route.dns, s.TunnelName) }})
	}
	for _, route := range slices.Backward(rs) {
		steps = append(steps, deletion{route.policy, func() error {
			p, err := r.cloud.GetPolicy(ctx, account, *route.app, *route.policy)
			if err != nil {
				return err
			}
			if p.ID != *route.policy || p.Name != resourceName(s, route.role+"-members") {
				return ErrOwnershipConflict
			}
			return nil
		}, func() error { return r.cloud.DeletePolicy(ctx, account, *route.app, *route.policy) }})
	}
	for _, route := range slices.Backward(rs) {
		steps = append(steps, deletion{route.app, func() error {
			a, err := r.cloud.GetApplication(ctx, account, *route.app)
			if err != nil {
				return err
			}
			if a.ID != *route.app || a.Name != resourceName(s, route.role) || a.Type != "self_hosted" {
				return ErrOwnershipConflict
			}
			policies, err := r.cloud.ListPolicies(ctx, account, *route.app)
			if err != nil {
				return err
			}
			for _, p := range policies {
				if p.ID != *route.policy {
					return ErrOwnershipConflict
				}
			}
			return nil
		}, func() error { return r.cloud.DeleteApplication(ctx, account, *route.app) }})
	}
	steps = append(steps, deletion{&s.Resources.GatewayPostureID, func() error {
		p, err := r.cloud.GetPosture(ctx, account, s.Resources.GatewayPostureID)
		if err != nil {
			return err
		}
		if !ownedPosture(s, p) {
			return ErrOwnershipConflict
		}
		return nil
	}, func() error { return r.cloud.DeletePosture(ctx, account, s.Resources.GatewayPostureID, s.TunnelName) }})
	steps = append(steps, deletion{&s.Resources.TunnelID, func() error {
		t, err := r.cloud.GetTunnel(ctx, account, s.Resources.TunnelID)
		if err != nil {
			return err
		}
		if !ownedTunnel(*s, t) {
			return ErrOwnershipConflict
		}
		// Cloudflare retains deleted_at tombstones; ownership must still match before detaching.
		if t.DeletedAt != nil {
			return errResourceAbsent
		}
		return nil
	}, func() error { return r.cloud.DeleteTunnel(ctx, account, s.Resources.TunnelID) }})
	return steps
}

func (r *Reconciler) verifyOwned(ctx context.Context, s *State) error {
	if s.Resources.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	for _, step := range r.deletions(ctx, s) {
		if *step.id == "" {
			continue
		}
		if !validOwner(s.TunnelName) {
			return ErrOwnershipConflict
		}
		if err := step.read(); err != nil {
			return err
		}
	}
	return nil
}

// Delete detaches the zone and OTP provider, deleting only verified owned resources.
func (r *Reconciler) Delete(ctx context.Context, by string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.store.Load(ctx)
	if err != nil {
		return err
	}
	s.Desired.Enabled = false
	s, err = r.store.SaveDesired(ctx, s.Generation, s.Desired, by)
	if err != nil {
		return err
	}
	s.Phase = PhaseDeleting
	s.ObservedHealthy = false
	if err = r.persist(ctx, &s); err != nil {
		return err
	}
	return r.delete(ctx, &s)
}

func (r *Reconciler) delete(ctx context.Context, s *State) error {
	err := r.deleteResources(ctx, s)
	if err == nil {
		return nil
	}
	return r.failed(ctx, s, err)
}

func (r *Reconciler) deleteResources(ctx context.Context, s *State) error {
	if r.projection == nil {
		return ErrConfiguration
	}
	if err := r.projection.Apply(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	if s.Resources.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	steps := r.deletions(ctx, s)
	// Verify the entire persisted set first so one foreign ID cannot trigger a partial teardown.
	for _, step := range steps {
		if *step.id == "" {
			continue
		}
		if !validOwner(s.TunnelName) {
			return ErrOwnershipConflict
		}
		if err := step.read(); err != nil && !notFound(err) {
			return err
		}
	}
	for _, step := range steps {
		if *step.id == "" {
			continue
		}
		if err := r.current(ctx, s); err != nil {
			return err
		}
		err := step.read()
		if err == nil {
			if err = step.remove(); err != nil && !notFound(err) {
				return err
			}
			err = step.read()
			if err == nil {
				return ErrRemotePresent
			}
		}
		if !notFound(err) {
			return err
		}
		*step.id = ""
		if err = r.persist(ctx, s); err != nil {
			return err
		}
	}
	// Zone and account-wide OTP remain in Cloudflare; only detach their local references.
	s.Resources.ZoneID = ""
	s.Resources.OTPProviderID = ""
	s.TunnelName = ""
	s.Phase = PhaseDisabled
	s.ObservedHealthy = false
	s.LastError = ""
	return r.persist(ctx, s)
}
