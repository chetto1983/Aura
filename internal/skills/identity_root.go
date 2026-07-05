package skills

import (
	"context"
	"errors"
)

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

// errIdentityRootStub is the RED-phase stub sentinel, removed once the resolver lands.
var errIdentityRootStub = errors.New("not implemented")

// NewSkillToolForIdentity resolves the per-identity skills + pyscripts storage roots
// from ctx (identityctx). Storage rooting only — snippet execution isolation is Phase 37.
func NewSkillToolForIdentity(ctx context.Context, base SkillRootBase) (IdentitySkillRoots, error) {
	return IdentitySkillRoots{}, errIdentityRootStub
}
