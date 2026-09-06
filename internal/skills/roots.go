package skills

import (
	"fmt"
	"path/filepath"
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
	//
	// It hangs off the identity's OWN root and never off the global export, and that is the
	// whole point rather than a filing preference. MaterializeIn tars the deployment export
	// into every box, and tarDir walks the tree: an identity export nested under the global
	// export would therefore be carried into every OTHER identity's box as
	// /skills/<their-id>/<skill>, one directory below the listing anybody would check.
	// A sibling base cannot be reached by that walk at all.
	if out.Identity != "" && out.Export != "" {
		out.Export = filepath.Join(out.Identity, identityExportDirName)
	}
	return out, nil
}

// identityExportDirName is the identity's export tree, inside their own root beside
// .staging. The leading dot is load-bearing: the skill-name grammar (SanitizeName) admits
// only [a-z0-9-], so no skill can ever be written to this name and the collision is
// unrepresentable rather than merely unlikely. The Loader skips it for the same reason it
// skips archived/ — a directory with no SKILL.md is not a skill.
const identityExportDirName = ".export"

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

// DirWithin reports whether dir lies inside root. It is a LEXICAL containment check on
// cleaned paths, deliberately: its one caller decides whether the cockpit offers a person
// the Archive/Delete verbs on a row, and the verbs themselves are already fenced by
// Writer.For(identity), which can only address that identity's own root. So this answers a
// display question, and a symlink-resolving fence here would buy nothing the write path
// does not already enforce — while an EvalSymlinks on every listed row would put a stat
// storm on the board's hot path.
//
// Empty root answers false rather than "everything": an unresolvable root must not read as
// ownership of the whole filesystem.
func DirWithin(root, dir string) bool {
	root = strings.TrimSpace(root)
	dir = strings.TrimSpace(dir)
	if root == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Owns reports whether dir is a skill directory this identity may write to — the one under
// their WritableRoot. A skill loaded from the house root, or from another identity's export
// via a share, is NOT owned: the person reads and runs it, and the cockpit must not offer
// them a verb that would land on a directory Writer.For cannot reach (amendment #218).
func (r Roots) Owns(dir string) bool { return DirWithin(r.WritableRoot(), dir) }
