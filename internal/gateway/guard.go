package gateway

import (
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// ValidateClassifiable is the boot-time, fail-loud wiring guard for the gateway
// classifier (D-02d). Mirroring tools.Registry.Register's duplicate-name panic
// (spec.go:111-117) and Registry.Validate's fail-closed boot check (:138-146), it
// runs two assertions over every registered tool: every Mutating tool declares
// complete operation metadata, and every Mutating + Multiplexed tool has a
// dedicated per-action classifier in multiplexedClassifiers.
//
// The multiplexed-classifier threat it closes (RESEARCH Pitfall 2): a newly-added
// multiplexed tool that forgets its multiplexedClassifiers entry would fall through
// classify's generic Mutating branch to a single FLAT tier, silently losing the
// per-action gating its `action` discriminator implies. That is a static wiring bug
// no caller can recover from, so it panics at boot rather than under-gating a live
// turn. Call once after all Register calls (wired into the serve boot in plan 35-03).
func ValidateClassifiable(reg *tools.Registry) {
	for _, t := range reg.All() {
		spec := t.Spec()
		if spec.Mutating {
			validateOperationMetadata(spec)
		}
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

// validateOperationMetadata is the second boot-time wiring guard (D-09), closing the
// gap the withdrawn second ReplayPolicy value was standing in for. beginOperation
// (reserve.go) and OperationFingerprint (tools/spec.go) both expect a Mutating tool's
// OperationScope, OperationNormalizer and ReplayPolicy to all be set; nothing
// enforced that at registration time before this, so an incomplete mutating tool
// would reach the idempotency layer with an undefined replay posture. Scoped to
// Mutating tools only — a non-mutating tool never reaches beginOperation, so
// requiring these fields on it would be a spurious constraint on every registered
// tool rather than only the ones that mutate. It checks EMPTINESS only, never a
// specific ReplayPolicy value: ReplayToolResult is the only value that exists today,
// and comparing against it here would duplicate reserve.go:43's exact-match guard
// under a different name (D-07).
func validateOperationMetadata(spec tools.Spec) {
	if spec.OperationScope == "" {
		panic(fmt.Sprintf(
			"gateway.ValidateClassifiable: mutating tool %q has an empty OperationScope — "+
				"a mutating tool must declare its idempotency scope or the gateway cannot reason "+
				"about replay for it", spec.Name))
	}
	if spec.OperationNormalizer == "" {
		panic(fmt.Sprintf(
			"gateway.ValidateClassifiable: mutating tool %q has an empty OperationNormalizer — "+
				"a mutating tool must declare its argument normalizer or the gateway cannot reason "+
				"about replay for it", spec.Name))
	}
	if spec.ReplayPolicy == "" {
		panic(fmt.Sprintf(
			"gateway.ValidateClassifiable: mutating tool %q has an empty ReplayPolicy — "+
				"a mutating tool must declare its replay policy or the gateway cannot reason "+
				"about replay for it", spec.Name))
	}
}
