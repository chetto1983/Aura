// Package breakglass holds the pure, DB-free and TTY-free primitives behind the
// offline `aura identity recover-operator` command: the operator-resolution guard
// (selectSoleOperator) and the password + recovery Q&A sourcing engine (Sourcer).
// The orchestration, Authula wiring and CLI glue that consume these symbols live
// in later waves; keeping the high-branch-density decision trees here makes them
// fully unit-testable and counted toward the owned-surface coverage floor.
package breakglass

import (
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/identity"
)

// selectSoleOperator resolves the single operator identity break-glass recovery
// targets, or refuses. It filters ids to kind=="user" (the seeded 'local' row is
// kind='system' and is excluded, as are 'channel'/'service') and applies the
// locked D-11 active/deactivated rule:
//
//   - exactly one ACTIVE operator wins, even alongside deactivated stragglers;
//   - >1 ACTIVE operators is ambiguous and refused (the >1 guard counts only
//     active operators, so a deactivated stale identity never trips it);
//   - with zero active, a lone deactivated operator is returned (a plausible,
//     recoverable lockout), but >1 deactivated-with-none-active is refused;
//   - zero kind='user' identities is refused.
//
// On any refusal it returns the zero Identity and a secret-free error, so the
// caller performs ZERO writes.
func selectSoleOperator(ids []identity.Identity) (identity.Identity, error) {
	var users, active []identity.Identity
	for _, id := range ids {
		if id.Kind != "user" {
			continue
		}
		users = append(users, id)
		if !id.Deactivated {
			active = append(active, id)
		}
	}

	switch {
	case len(active) == 1:
		return active[0], nil
	case len(active) > 1:
		return identity.Identity{}, fmt.Errorf("%d active operator identities found; refusing (break-glass targets a single active operator)", len(active))
	}

	// Zero active operators: fall back to the full user set — a lone deactivated
	// operator is a recoverable lockout, but anything else is ambiguous.
	switch len(users) {
	case 0:
		return identity.Identity{}, errors.New("no operator (kind='user') identity found")
	case 1:
		return users[0], nil
	default:
		return identity.Identity{}, fmt.Errorf("%d deactivated operator identities found and none active; refusing (break-glass cannot disambiguate)", len(users))
	}
}
