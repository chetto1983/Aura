package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func layout() Layout {
	return Layout{
		Global:     "/srv/skills",
		Identities: "/srv/skills-identities",
		Export:     "/srv/skills-export",
	}
}

// The unscoped case is today's behaviour, and it has to stay byte-identical: the CLI listing
// what the deployment ships, and a deployment with no per-identity base configured.
func TestUnscopedRootsAreTheDeploymentsOwn(t *testing.T) {
	r, err := layout().For("")
	if err != nil {
		t.Fatalf("For(\"\"): %v", err)
	}
	if r.Identity != "" {
		t.Fatalf("identity root = %q, want none", r.Identity)
	}
	if r.Global != "/srv/skills" || r.Export != "/srv/skills-export" {
		t.Fatalf("roots = %#v", r)
	}
	if got := strings.Join(r.LoaderRoots(), ","); got != "/srv/skills" {
		t.Fatalf("loader roots = %q, want the global root alone", got)
	}
	if r.WritableRoot() != "/srv/skills" {
		t.Fatalf("writable root = %q, want the global one", r.WritableRoot())
	}
}

func TestScopedRootsDeriveBothPerIdentityPaths(t *testing.T) {
	r, err := layout().For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if r.Identity != filepath.Join("/srv/skills-identities", "alice") {
		t.Fatalf("identity root = %q", r.Identity)
	}
	if r.Export != filepath.Join(r.Identity, ".export") {
		t.Fatalf("export root = %q — an export shared by every identity puts every identity's skills in every box", r.Export)
	}
	if r.WritableRoot() != r.Identity {
		t.Fatalf("a scoped write must land in the identity's own root, got %q", r.WritableRoot())
	}
}

// D-214-3: the global root wins a name collision, so it must be LAST — the Loader merges in
// order with later-root-wins. Asserting the order is asserting the precedence.
func TestGlobalRootIsLastSoTheOperatorWinsACollision(t *testing.T) {
	r, err := layout().For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	roots := r.LoaderRoots()
	if len(roots) != 2 {
		t.Fatalf("roots = %#v, want the identity root then the global one", roots)
	}
	if roots[0] != r.Identity {
		t.Fatalf("roots[0] = %q, want the identity root first", roots[0])
	}
	if roots[len(roots)-1] != r.Global {
		t.Fatalf("roots[last] = %q, want the global root last so it wins the collision", roots[len(roots)-1])
	}
}

// The per-identity base is separate from the global root precisely so an identity can never
// name a directory inside the namespace the loader scans for skills.
func TestIdentityRootsNeverLandInsideTheGlobalRoot(t *testing.T) {
	r, err := layout().For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if strings.HasPrefix(r.Identity, r.Global+string(filepath.Separator)) {
		t.Fatalf("identity root %q sits inside the scanned global root %q", r.Identity, r.Global)
	}
}

func TestForRefusesAnIdentityThatEscapesEitherBase(t *testing.T) {
	for _, id := range []string{"../evil", "a/b", ".hidden", strings.Repeat("x", 65)} {
		if _, err := layout().For(id); err == nil {
			t.Fatalf("For(%q) = nil error, want a refusal", id)
		}
	}
}

// An unconfigured per-identity base is not an error: it is a deployment that has not turned
// per-identity skills on, and it must behave exactly like the unscoped case.
func TestAnUnconfiguredIdentityBaseLeavesEveryoneOnTheGlobalRoot(t *testing.T) {
	l := layout()
	l.Identities = "  "
	r, err := l.For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if r.Identity != "" || r.WritableRoot() != "/srv/skills" {
		t.Fatalf("roots = %#v, want the global root alone", r)
	}
	// With no identity base there is no per-identity library to export, so the export stays
	// the deployment's — the same single tree the pre-#214 box was filled from. Scoping it
	// here would name a directory nothing ever writes into and leave the box holding only
	// whatever the global source carried anyway.
	if r.Export != "/srv/skills-export" {
		t.Fatalf("export = %q, want the deployment export when per-identity skills are off", r.Export)
	}
}

// TestIdentityExportIsNotReachableFromTheGlobalExport is the leak this layout exists to make
// impossible, asserted as a path property rather than as a promise.
//
// MaterializeIn tars the DEPLOYMENT export into every box and tarDir walks the whole tree, so
// an identity export nested under the global export would ride into every other identity's
// box as /skills/<their-id>/<skill> — present, readable, and invisible to any check that
// lists only the top level of /skills.
func TestIdentityExportIsNotReachableFromTheGlobalExport(t *testing.T) {
	l := layout()
	alice, err := l.For("alice")
	if err != nil {
		t.Fatalf("For(alice): %v", err)
	}
	bob, err := l.For("bob")
	if err != nil {
		t.Fatalf("For(bob): %v", err)
	}
	for _, victim := range []Roots{alice, bob} {
		rel, relErr := filepath.Rel(l.Export, victim.Export)
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			t.Fatalf("identity export %q lives under the deployment export %q — every box would carry it", victim.Export, l.Export)
		}
	}
	if rel, relErr := filepath.Rel(alice.Export, bob.Export); relErr == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("bob's export %q lives under alice's %q", bob.Export, alice.Export)
	}
}

func TestZeroLayoutYieldsNothingRatherThanAJoinOfEmptyStrings(t *testing.T) {
	r, err := Layout{}.For("alice")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(r.LoaderRoots()) != 0 || r.WritableRoot() != "" || r.Export != "" {
		t.Fatalf("zero layout produced %#v", r)
	}
}
