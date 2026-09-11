package agui

// deprovision_route.go is the DELETE /api/admin/identities/{id} HTTP wrapper over the
// deprovision.go saga (RBAC-05), on audit_api.go's Set*-after-construct pattern.
//
// The identityRemover port's two methods (Deactivate, PurgeOne) are named identically
// to *Deprovisioner's own two methods so *Deprovisioner (same package) satisfies this
// interface with NO adapter code -- the composition root wires the SAME
// *agui.Deprovisioner instance the CLI and the cron sweep already use
// (cmd/aura/serve_provisioning.go's buildDeprovisioner), not a second copy. A narrow
// interface is still declared (rather than depending on *Deprovisioner directly) so
// this route's own unit tests inject a fake with no live saga sub-ports at all.
//
// D-06's sequence -- deactivate then purge, immediately, never the 7-day grace window
// -- is exactly Deactivate followed by PurgeOne: PurgeOne resolves the target and runs
// the same identity.CanRemoveIdentity check Purge itself enforces, so the last-admin
// refusal fires from the saga, not from a copy of the policy at this route.
//
// The teardown runs synchronously: a 200 response means every plane converged
// (deactivate + full purge), matching the UI-SPEC's "Removing {{name}}..." /
// aria-busy state contract a later plan's cockpit reads literally -- there is no
// accepted-with-status shape here for that plan to poll.
//
// What this route cannot do, and the removal dialog's copy (a later plan) is where a
// human is told so: OpenRouter retains the deleted key's consumption in its own
// analytics (02-OPENROUTER-API.md). Revoking (deprovision.go's sagaStepOpenRouterKey,
// which PurgeOne reaches) stops future spend and access; it does not erase the
// identity's past usage from the provider's own records.

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/identity"
)

// identityRemover is the narrow consumer-side port this route calls.
type identityRemover interface {
	Deactivate(ctx context.Context, identityID string) error
	PurgeOne(ctx context.Context, identityID string) error
}

// *Deprovisioner (deprovision.go, same package) satisfies identityRemover with no
// adapter code -- asserted at compile time so a signature drift on either method
// fails the build here, not silently at the composition root.
var _ identityRemover = (*Deprovisioner)(nil)

// SetIdentityRemover wires the removal route. Until called, DELETE answers 503
// (matches SetAuditStore's precedent).
func (s *Server) SetIdentityRemover(r identityRemover) { s.idRemover = r }

func (s *Server) registerIdentityRemovalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /api/admin/identities/{id}", s.handleRemoveIdentity)
}

// removalOutcome is the singleflight.Group payload for one coalesced removal --
// empty, since a successful Do() call means "the saga converged"; the error return
// alone carries failure.
type removalOutcome struct{}

func (s *Server) handleRemoveIdentity(w http.ResponseWriter, r *http.Request) {
	if s.idRemover == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity remover not configured"})
		return
	}
	targetID := r.PathValue("id")
	if _, err := uuid.Parse(targetID); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid identity id"})
		return
	}

	// The saga outlives the request: a caller that goes away between the legs would otherwise
	// leave the identity deactivated but not purged, with its OpenRouter key still live.
	ctx := context.WithoutCancel(r.Context())
	// Two simultaneous DELETEs for the same identity coalesce into ONE saga run
	// (RBAC-05 concurrency probe): the loser never calls Deactivate/PurgeOne itself,
	// it waits on and receives the SAME outcome the first caller's run produces.
	_, err, _ := s.idRemovalGroup.Do(targetID, func() (any, error) {
		if err := s.idRemover.Deactivate(ctx, targetID); err != nil {
			return removalOutcome{}, err
		}
		return removalOutcome{}, s.idRemover.PurgeOne(ctx, targetID)
	})
	if err != nil {
		if errors.Is(err, identity.ErrLastAdministrator) {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": SanitizeString(err.Error())})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "identity removal failed"})
		return
	}
	writeJSON(w, map[string]any{"identity_id": targetID, "status": "removed"})
}
