package mcpenv

import "testing"

// TestNodeDeclaredRefusesANameOutsideNodeModules proves a package name that climbs out of the
// environment's node_modules is refused before any file is read: the package.json it would
// reach is not this install's.
func TestNodeDeclaredRefusesANameOutsideNodeModules(t *testing.T) {
	p := &Preparer{}
	for _, pkg := range []string{"../../outside", "../sibling", "/etc"} {
		if _, err := p.nodeDeclared(t.TempDir(), pkg); err == nil {
			t.Errorf("nodeDeclared(%q) read a manifest outside node_modules", pkg)
		}
	}
}
