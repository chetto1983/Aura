package skills

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/idroot"
)

// roots.go decides WHERE one identity's skills are read from and exported to (amendment
// #214). It is the only place the layout is decided, so a caller never joins a skills path
// by hand and there is one answer to "which directories does this person's agent see".
//
// The per-identity base is a SEPARATE root, not a subdirectory of the global one. The
// provisioner used to create $AURA_SKILLS_DIR/<id>, which put every identity root inside the
// namespace the Loader scans for skills — siblings of the skills themselves. Nothing broke
// only because a directory without a SKILL.md is skipped in silence; a skill whose name
// happened to match an identity would have. Separate roots make that class of collision
// unrepresentable rather than merely unlikely.

// Layout is the deployment's skill directory layout: the three bases every identity's roots
// are derived from. It carries plain paths on purpose — this package does not import config,
// and the composition root is what reads the env.
type Layout struct {
	// Global holds the deployment-wide skills, read-only to every identity.
	Global string
	// Identities is the base each identity's OWN skills live under, one directory per
	// identity. Empty disables per-identity skills entirely (every identity sees Global only).
	Identities string
	// Export is the base of the trees MaterializeIn copies into the boxes.
	Export string
}

// Roots is one identity's resolved view of the Layout. Identity is empty when the caller is
// unscoped — the CLI listing what the deployment ships, or a single-operator deployment that
// has no per-identity base configured. That case keeps today's behaviour exactly: the global
// root and the global export, unchanged.
type Roots struct {
	Global   string
	Identity string
	Export   string
}

// For resolves the roots one identity reads and writes. Both per-identity paths go through
// idroot, the single traversal guard every operator-keyed root in this codebase already uses
// (D-20/D-21), so a crafted identity cannot escape either base.
func (l Layout) For(identity string) (Roots, error) {
	out := Roots{Global: strings.TrimSpace(l.Global), Export: strings.TrimSpace(l.Export)}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return out, nil
	}
	if base := strings.TrimSpace(l.Identities); base != "" {
		dir, err := idroot.RootIdentityDir(base, identity)
		if err != nil {
			return Roots{}, fmt.Errorf("skills: identity %q cannot name a directory: %w", identity, err)
		}
		out.Identity = dir
	}
	// The export is per identity for the same reason the source is: MaterializeIn copies an
	// export tree into a box, so an export shared by every identity puts every identity's
	// skills in every box no matter how correctly the loader is scoped.
	if out.Export != "" {
		dir, err := idroot.RootIdentityDir(out.Export, identity)
		if err != nil {
			return Roots{}, fmt.Errorf("skills: identity %q cannot name an export directory: %w", identity, err)
		}
		out.Export = dir
	}
	return out, nil
}

// LoaderRoots orders the roots for Loader.Config.Roots.
//
// The identity root comes FIRST and the global root LAST, which is the opposite of how it
// reads: the Loader merges in order with LATER-ROOT-WINS, so the last root is the one that
// survives a name collision. Global last therefore means global WINS (amendment #214,
// D-214-3) — and it is what Loader's own doc comment already assumed when it described "an
// operator override placed in a later root".
//
// That direction is deliberate. A skill the operator ships is house policy; a person quietly
// shadowing it with their own is how the policy stops applying without anyone noticing. The
// person keeps every name the operator has not taken.
func (r Roots) LoaderRoots() []string {
	roots := make([]string, 0, 2)
	if r.Identity != "" {
		roots = append(roots, r.Identity)
	}
	if r.Global != "" {
		roots = append(roots, r.Global)
	}
	return roots
}

// WritableRoot is where a write from this identity lands: their own root when they have one,
// the global root when they do not. It is a method rather than a field so a caller cannot
// pick the wrong one by reading whichever is set.
func (r Roots) WritableRoot() string {
	if r.Identity != "" {
		return r.Identity
	}
	return r.Global
}
