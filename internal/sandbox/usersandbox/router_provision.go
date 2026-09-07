// router_provision.go adds EnsureBox (D-09/T-01-02): the EXPLICIT-identity get-or-create
// seam the provisioning saga's sandbox leg calls, alongside Route's context-derived seam
// every tool call uses. The two differ in exactly one way: Route resolves the caller
// principal from context and falls back to the seeded `local` identity when none is
// scoped (the CLI / no-principal case); EnsureBox takes identityID as an explicit
// parameter and REFUSES a blank one instead of silently falling back — a saga leg always
// has a real identity id in hand by the time it reaches here, so a dropped stamp is a bug
// to fail loudly on, never a legitimate "use local instead" case (a caller could otherwise
// provision one identity's box under another's key). Both seams share the same
// resolve-and-track body (resolveAndTrack) so the get-or-create logic exists once
// (CLAUDE.md REUSABLE CODE; golangci-lint's dupl is on at threshold 100).
package usersandbox

import (
	"context"
	"errors"
	"strings"
)

// EnsureBox is the provisioning saga's explicit-identity get-or-create seam (D-09). It
// fails CLOSED on a nil receiver, a nil backend, or a blank identityID — never falling
// back to the seeded `local` identity the way Route's context-derived identityID does
// (T-01-02). On success it resolves (creates or reuses) the identity's box and records
// lastUsed/handles under r.mu exactly as Route does, so the idle reaper and Destroy see a
// box EnsureBox created the same way they see one Route created.
func (r *SandboxRouter) EnsureBox(ctx context.Context, identityID string) (BoxHandle, error) {
	if r == nil || r.backend == nil {
		return BoxHandle{}, errBackendUnavailable
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return BoxHandle{}, errors.New("sandbox ensure box: identity is required")
	}
	return r.resolveAndTrack(ctx, identityID)
}

// resolveAndTrack is the get-or-create body Route and EnsureBox share: resolve the box for
// id via the backend, and on success record lastUsed/handles under r.mu. Callers are
// responsible for the nil-receiver/nil-backend/blank-id checks before calling this — it
// assumes a valid router and a non-empty id.
func (r *SandboxRouter) resolveAndTrack(ctx context.Context, id string) (BoxHandle, error) {
	h, err := r.backend.Resolve(ctx, r.specFor(id))
	if err != nil {
		return BoxHandle{}, err // fail-CLOSED (D-09/GATE-01) — the caller denies, never host
	}
	r.mu.Lock()
	r.lastUsed[id] = r.clock()
	r.handles[id] = h
	r.mu.Unlock()
	return h, nil
}
