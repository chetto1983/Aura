package remotetunnel

import (
	"cmp"
	"context"
	"slices"
)

// deletion removes one resource. A non-nil abandon marks a resource the operator can do without
// removing: when the token is denied access to it, the pass lists it and goes on.
type deletion struct {
	id      *string
	read    func() error
	remove  func() error
	abandon *AbandonedResource
}

func (r *Reconciler) deletions(ctx context.Context, s *State) []deletion {
	account := s.Resources.AccountID
	return append(r.routeDeletions(ctx, s, routes(s)[0], false), deletion{&s.Resources.TunnelID, func() error {
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
	}, func() error { return r.cloud.DeleteTunnel(ctx, account, s.Resources.TunnelID) }, nil})
}

// routeDeletions removes DNS first, then policy, then application, each in reverse route order.
// routeDeletions removes the route's DNS record, then its policy, then its application. A step
// carries an abandon entry only when the caller can live without the resource being removed.
func (r *Reconciler) routeDeletions(ctx context.Context, s *State, route route, abandon bool) []deletion {
	account := s.Resources.AccountID
	entry := func(kind, name string) *AbandonedResource {
		if !abandon {
			return nil
		}
		return &AbandonedResource{Kind: kind, Name: name}
	}
	return []deletion{{route.dns, func() error {
		d, err := r.cloud.GetDNS(ctx, s.Resources.ZoneID, *route.dns)
		if err != nil {
			return err
		}
		if !ownedDNS(s, route, d) {
			return ErrOwnershipConflict
		}
		return nil
	}, func() error { return r.cloud.DeleteDNS(ctx, s.Resources.ZoneID, *route.dns, s.TunnelName) }, entry("dns", route.hostname)},
		{route.policy, func() error {
			p, err := r.cloud.GetPolicy(ctx, account, *route.app, *route.policy)
			if err != nil {
				return err
			}
			if p.ID != *route.policy || p.Name != resourceName(s, route.role+"-members") {
				return ErrOwnershipConflict
			}
			return nil
		}, func() error { return r.cloud.DeletePolicy(ctx, account, *route.app, *route.policy) }, entry("access_policy", resourceName(s, route.role+"-members"))},
		{route.app, func() error {
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
		}, func() error { return r.cloud.DeleteApplication(ctx, account, *route.app) }, entry("access_app", resourceName(s, route.role))}}
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
	if err := r.retireWARP(ctx, s); err != nil {
		return err
	}
	if r.projection == nil {
		return ErrConfiguration
	}
	if err := r.projection.Apply(ctx, ProjectionState{Generation: s.Generation}); err != nil {
		return err
	}
	if s.Resources.AccountID != s.Desired.AccountID {
		return ErrOwnershipConflict
	}
	if err := r.runDeletions(ctx, s, r.deletions(ctx, s)); err != nil {
		return err
	}
	// Zone and account-wide OTP remain in Cloudflare; only detach their local references.
	s.Resources.ZoneID = ""
	s.Resources.OTPProviderID = ""
	s.TunnelName = ""
	s.Phase = PhaseDisabled
	s.ObservedHealthy = false
	s.Reason = ""
	return r.persist(ctx, s)
}

// runDeletions removes each step's owned resource, treating a 404 as already gone, and persists
// every cleared ID so a pass interrupted halfway resumes where it stopped.
func (r *Reconciler) runDeletions(ctx context.Context, s *State, steps []deletion) error {
	var visible *bool
	// skippable reports whether a denial lets the pass go on without this step. A token that
	// cannot see the account is not a denial of one resource, so it stops the retirement.
	skippable := func(step deletion, err error) (bool, error) {
		if step.abandon == nil || !denied(err) {
			return false, nil
		}
		if visible == nil {
			v, e := r.accountVisible(ctx, s)
			if e != nil {
				return false, e
			}
			visible = &v
		}
		if !*visible {
			return false, errBlindToken
		}
		return true, nil
	}
	// Verify the entire persisted set first so one foreign ID cannot trigger a partial teardown.
	for _, step := range steps {
		if *step.id == "" {
			continue
		}
		if !validOwner(s.TunnelName) {
			return ErrOwnershipConflict
		}
		if err := step.read(); err != nil && !notFound(err) {
			if skip, e := skippable(step, err); e != nil || !skip {
				return cmp.Or(e, err)
			}
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
				if skip, e := skippable(step, err); e != nil || !skip {
					return cmp.Or(e, err)
				}
				if err = r.abandonStep(ctx, s, step); err != nil {
					return err
				}
				continue
			}
			err = step.read()
			if err == nil {
				return ErrRemotePresent
			}
		} else if skip, e := skippable(step, err); e != nil {
			return e
		} else if skip {
			if err = r.abandonStep(ctx, s, step); err != nil {
				return err
			}
			continue
		}
		if !notFound(err) {
			return err
		}
		*step.id = ""
		if err = r.persist(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// abandonStep lists a resource the token cannot touch, then forgets its ID. The list is written
// before the ID goes, so a crash between the two repeats the step instead of losing the entry.
func (r *Reconciler) abandonStep(ctx context.Context, s *State, step deletion) error {
	if !slices.Contains(s.Abandoned, *step.abandon) {
		if err := r.store.AddAbandoned(ctx, []AbandonedResource{*step.abandon}); err != nil {
			return err
		}
		s.Abandoned = append(s.Abandoned, *step.abandon)
	}
	*step.id = ""
	return r.persist(ctx, s)
}
