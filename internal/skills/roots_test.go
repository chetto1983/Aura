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
	if r.Export != filepath.Join("/srv/skills-export", "alice") {
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
	// The export still scopes: the box boundary does not depend on the source layout.
	if r.Export != filepath.Join("/srv/skills-export", "alice") {
		t.Fatalf("export = %q, want it scoped even with no identity source root", r.Export)
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
