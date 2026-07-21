package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/idroot"
)

// localSkillIdentity is the CLI / no-principal identity whose skills stay in the base
// dirs (backward-compatible single-user layout, D-21/D-25).
const localSkillIdentity = "local"

// SkillRootBase carries the base (single-user / default) storage dirs the per-identity
// resolver roots under. SkillsDir is $AURA_SKILLS_DIR (the shared read-only built-in
// root and, for local, the user root); PyScriptsDir is ~/.aura/pyscripts. An empty
// PyScriptsDir falls back to the home-derived default.
type SkillRootBase struct {
	SkillsDir    string
	PyScriptsDir string
}

// IdentitySkillRoots is the per-identity skills + pyscripts storage rooting result
// (D-21). Built-ins stay shared read-only under SharedSkillsDir; a user's own skills
// live under UserSkillsDir and snippets under PyScriptsDir. LoaderRoots is the scan
// order (shared first, per-identity user last so a user override shadows a built-in).
type IdentitySkillRoots struct {
	Identity        string
	SharedSkillsDir string
	UserSkillsDir   string
	PyScriptsDir    string
	LoaderRoots     []string
}

// isLocalSkillIdentity reports whether identity is the local / no-principal case that
// resolves the base dirs directly (backward compatible single-user layout).
func isLocalSkillIdentity(identity string) bool {
	s := strings.TrimSpace(identity)
	return s == "" || s == localSkillIdentity
}

// defaultPyScriptsDir is the home-derived base for per-identity snippet storage
// (~/.aura/pyscripts), mirroring config.auraHomeDir with a tmp fallback when the home
// dir is unavailable.
func defaultPyScriptsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura", "pyscripts")
	}
	return filepath.Join(os.TempDir(), "aura", "pyscripts")
}

// NewSkillToolForIdentity resolves the per-identity skills + pyscripts storage roots
// from ctx (identityctx), reusing the profile traversal-safe containment guard. Built-ins
// stay shared read-only under SharedSkillsDir (never copied per identity); a user's own
// skills live under $AURA_SKILLS_DIR/{id} and snippets under ~/.aura/pyscripts/{id}. The
// local / no-principal identity resolves the base dirs unchanged (backward compatible).
// This is STORAGE rooting only — snippet execution isolation is Phase 37.
func NewSkillToolForIdentity(ctx context.Context, base SkillRootBase) (IdentitySkillRoots, error) {
	pyBase := strings.TrimSpace(base.PyScriptsDir)
	if pyBase == "" {
		pyBase = defaultPyScriptsDir()
	}
	identity := identityctx.IdentityID(ctx)

	if isLocalSkillIdentity(identity) {
		return IdentitySkillRoots{
			Identity:        localSkillIdentity,
			SharedSkillsDir: base.SkillsDir,
			UserSkillsDir:   base.SkillsDir,
			PyScriptsDir:    pyBase,
			LoaderRoots:     []string{base.SkillsDir},
		}, nil
	}

	userDir, err := idroot.RootIdentityDir(base.SkillsDir, identity)
	if err != nil {
		return IdentitySkillRoots{}, err
	}
	pyDir, err := idroot.RootIdentityDir(pyBase, identity)
	if err != nil {
		return IdentitySkillRoots{}, err
	}
	return IdentitySkillRoots{
		Identity:        identity,
		SharedSkillsDir: base.SkillsDir,
		UserSkillsDir:   userDir,
		PyScriptsDir:    pyDir,
		LoaderRoots:     []string{base.SkillsDir, userDir},
	}, nil
}
