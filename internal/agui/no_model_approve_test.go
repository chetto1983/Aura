package agui

// no_model_approve_test.go is the SPEC-Prohibition-#3 ("no model-facing approve /
// pending non-injectable") held-out backstop. It proves BOTH halves INDEPENDENTLY
// (two top-level test functions, each fails on its own):
//
//	(a) TestNoModelFacingActivatePath — a CONCRETE type-resolved static check: it loads
//	    the model-facing tool packages with golang.org/x/tools/go/packages (full
//	    TypesInfo) and walks every CallExpr, resolving each selector to its types.Object
//	    via the type-checker (NOT a string grep, so a same-named local method cannot fool
//	    it). It asserts COUNT==0 call sites whose callee is (*skills.Writer).Activate OR
//	    (*skills.ResumeHandler).Resume. The legitimate callers — internal/skills/resume.go,
//	    the ApprovalAuto path in internal/skills/writer.go, the CLI, the operator HTTP
//	    resume handler — are OUTSIDE the scanned model-tool package set, so they do not
//	    count. A model tool reaching Activate/Resume would be an elevation of privilege
//	    (T-29-05-03): a model could self-approve a pending skill into the active loader.
//
//	(b) TestActiveLoaderExcludesPending — the pending-non-injectable held-out: stage a
//	    skill under pending/ and assert the active Loader's List()/Get() (the ONLY path
//	    that mounts a body into LLM context) never returns it. A pending body crossing into
//	    context would let an un-approved skill be injected (T-29-05-03 / prompt injection).

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/skills"
	"golang.org/x/tools/go/packages"
)

// modelToolPackages is the model-facing tool package set the no-model-approve scan
// loads. internal/agent/tools is the model-visible tool registry (skill.go,
// skill_write.go, skill_read.go, registry.go, manifest.go, action.go, …). If a future
// model-tool registry package is added, append it here so the held-out keeps covering
// the full model surface.
var modelToolPackages = []string{
	"github.com/chetto1983/aura/internal/agent/tools",
}

// forbiddenSkillCallees names the two skills methods no model-tool path may ever reach.
// They are the ONLY promote-pending→active / approve methods (writer_activate.go:24 +
// resume.go:48). A model-tool edge into either is the SPEC-Prohibition-#3 violation.
var forbiddenSkillCallees = map[string]bool{
	"Activate": true, // (*skills.Writer).Activate — pending → active
	"Resume":   true, // (*skills.ResumeHandler).Resume — calls Writer.Activate on accept
}

// TestNoModelFacingActivatePath (part a) loads the model-facing tool packages with full
// type information and asserts ZERO type-resolved call sites into (*skills.Writer).Activate
// or (*skills.ResumeHandler).Resume. It resolves each call's callee through types.Info
// (Uses/Selections), so it cannot be fooled by a same-named local method or a string match.
// It passes independently of part (b).
func TestNoModelFacingActivatePath(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, modelToolPackages...)
	if err != nil {
		t.Fatalf("packages.Load(%v): %v", modelToolPackages, err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("packages.Load returned no packages for %v", modelToolPackages)
	}

	var loadErrs int
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		loadErrs += len(p.Errors)
	})
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("package %s load error (the scan is only sound on a clean type-check): %v", p.PkgPath, e)
		}
		if p.TypesInfo == nil {
			t.Fatalf("package %s loaded without TypesInfo — the type-resolved scan cannot run", p.PkgPath)
		}
	}
	if loadErrs > 0 {
		t.Fatalf("model-tool packages did not type-check cleanly (%d errors) — refusing to report a false COUNT==0", loadErrs)
	}

	var hits []string
	var scannedCalls int
	for _, p := range pkgs {
		info := p.TypesInfo
		fset := p.Fset
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				scannedCalls++
				obj := resolveSelectorObject(info, sel)
				if obj == nil {
					return true
				}
				fn, ok := obj.(*types.Func)
				if !ok {
					return true
				}
				if !forbiddenSkillCallees[fn.Name()] {
					return true
				}
				// The selector name matched a forbidden name; confirm via the type-checker
				// that the receiver is genuinely a skills.Writer / skills.ResumeHandler —
				// a same-named method on an unrelated type is NOT a violation.
				if recv := receiverTypeName(fn); recv == "skills.Writer" || recv == "skills.ResumeHandler" {
					pos := fset.Position(call.Pos())
					hits = append(hits, fn.FullName()+" @ "+pos.String())
				}
				return true
			})
		}
	}

	if scannedCalls == 0 {
		t.Fatalf("scanned 0 call expressions in %v — the scan is not exercising the model-tool surface", modelToolPackages)
	}
	if len(hits) != 0 {
		t.Fatalf("SPEC Prohibition #3 VIOLATED: a model-facing tool path reaches a skill-activation method (want COUNT==0, got %d):\n%v",
			len(hits), hits)
	}
	t.Logf("no-model-approve: scanned %d call sites across %d model-tool package(s); COUNT==0 edges into Writer.Activate/ResumeHandler.Resume",
		scannedCalls, len(pkgs))
}

// resolveSelectorObject resolves a selector expression to its types.Object using the
// type-checker's recorded Uses/Selections — the authoritative resolution, never a string
// match. Selections covers method-value/field selections (x.M()); Uses covers
// package-qualified identifiers (pkg.Func()).
func resolveSelectorObject(info *types.Info, sel *ast.SelectorExpr) types.Object {
	if info.Selections != nil {
		if s, ok := info.Selections[sel]; ok {
			return s.Obj()
		}
	}
	if info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			return obj
		}
	}
	return nil
}

// receiverTypeName returns "pkg.Type" for a method's receiver (pointer or value),
// or "" if fn is not a method. It is used to confirm a forbidden-named callee actually
// belongs to the skills.Writer/ResumeHandler types and not a same-named method elsewhere.
func receiverTypeName(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok {
		return ""
	}
	obj := named.Obj()
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Name() + "." + obj.Name()
}

// TestActiveLoaderExcludesPending (part b) proves a pending skill body is never returned
// by the active Loader (the ONLY path that mounts a body into LLM context). It stages a
// skill under pending/ and an unrelated active skill under active/, points a Loader at
// active/ ONLY (the production wiring), and asserts List()/Get() return the active skill
// and never the pending one. It passes independently of part (a).
func TestActiveLoaderExcludesPending(t *testing.T) {
	root := t.TempDir()
	activeDir := filepath.Join(root, "active")
	pendingDir := filepath.Join(root, "pending")

	stageSkill(t, activeDir, "active-skill", "an approved active skill")
	stageSkill(t, pendingDir, "pending-skill", "an UN-approved pending skill — MUST NOT load")

	// The Loader is constructed with the active root ONLY, mirroring production: the
	// pending/ dir is deliberately not a root, so a pending body cannot be scanned into
	// context. (ListStage is the display-only reader for the board; it never mounts.)
	loader := skills.NewLoader(skills.Config{Roots: []string{activeDir}})

	got := loader.List()
	for _, s := range got {
		if s.Name == "pending-skill" {
			t.Fatalf("active Loader.List() returned the pending skill %q — a pending body is injectable into context (Prohibition #3 part b VIOLATED)", s.Name)
		}
	}
	if len(got) != 1 || got[0].Name != "active-skill" {
		t.Fatalf("active Loader.List() = %v, want exactly [active-skill]", names(got))
	}

	if _, ok := loader.Get("pending-skill"); ok {
		t.Fatal("active Loader.Get(\"pending-skill\") found a pending skill — it must be invisible to the loader")
	}
	if _, ok := loader.Get("active-skill"); !ok {
		t.Fatal("active Loader.Get(\"active-skill\") missing the active skill")
	}
}

// stageSkill writes a minimal valid <root>/<name>/SKILL.md (name + description
// frontmatter + a one-line body) so the Loader's structural validation accepts it.
func stageSkill(t *testing.T, root, name, desc string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nUse this skill when " + desc + ".\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md for %s: %v", name, err)
	}
}

func names(ss []skills.Skill) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}
