package gateway

import (
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// ValidateClassifiable is the boot-time, fail-loud wiring guard for the gateway
// classifier (D-02d). Mirroring tools.Registry.Register's duplicate-name panic
// (spec.go:111-117) and Registry.Validate's fail-closed boot check (:138-146), it
// asserts that every registered Mutating + Multiplexed tool has a dedicated
// per-action classifier in multiplexedClassifiers.
//
// The threat it closes (RESEARCH Pitfall 2): a newly-added multiplexed tool that
// forgets its multiplexedClassifiers entry would fall through classify's generic
// Mutating branch to a single FLAT tier, silently losing the per-action gating its
// `action` discriminator implies. That is a static wiring bug no caller can recover
// from, so it panics at boot rather than under-gating a live turn. Call once after
// all Register calls (wired into the serve boot in plan 35-03).
func ValidateClassifiable(reg *tools.Registry) {
	for _, t := range reg.All() {
		spec := t.Spec()
		if !spec.Mutating || !spec.Multiplexed {
			continue
		}
		if _, ok := multiplexedClassifiers[spec.Name]; !ok {
			panic(fmt.Sprintf(
				"gateway.ValidateClassifiable: mutating multiplexed tool %q has no per-action "+
					"classifier — add it to multiplexedClassifiers, or the gateway will under-gate "+
					"its actions", spec.Name))
		}
	}
}
