//go:build cot_eval

// skills_snippet_reuse_registry_cot_eval_test.go is the eval↔production registry-parity
// half of the 18-04 snippet-reuse gate (split from skills_cot_eval_test.go per the
// 600-LOC cap). It carries the SIBLING registry constructor that registers the PRODUCTION
// `skill` tool (live loader + writer over a temp skills root) on top of the seam-free set,
// plus the eval-test-only loader/writer adapters that bridge *skills.Loader / *skills.Writer
// onto the tools.skillLoader / tools.skillWriter consumer-declared seams.
//
// WHY the adapters are local (Option B per the 18-04 plan): the production composition
// root (cmd/aura.newSkillTool + skillLoaderAdapter/skillWriterAdapter) lives in package
// main and is NOT importable from internal/eval. These ~30 LOC mirror serve_adapters.go
// exactly (actor "model", cli ApprovalSource for restore/archive, host-path Snippet
// resolution). They are NEVER a production surface. Extracting them into a shared
// internal/skilladapters package importable from BOTH cmd/aura and internal/eval is the
// deep-refactor-on-touch move, but it would rewire the cmd/aura composition root in a
// measurement-only plan AND a parallel session was concurrently editing cmd/aura at
// execution time — so the extraction is deferred to a STATE.md follow-up for when
// serve_adapters.go is next touched. The key-free TestRegistrySnippetReuse_HasSkillTool
// (classify_cot_eval_test.go) guards the parity so a regression fails CI's structural slot.
package eval

import (
	"context"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

// buildSnippetReuseRegistry is the SNIPPET-REUSE eval registry (18-04 Task 1, Pitfall 3 /
// #52-#53 rule 4). It is a SIBLING of buildSeamFreeSkillsRegistry — the find-skills xlsx
// gate (TestSkillsE2E / TestRegistry_SeamFree) is deliberately left untouched — that adds
// the PRODUCTION `skill` tool so a snippet-reuse scenario can resolve action=use /
// action=save_snippet. The live *skills.Loader + *skills.Writer are adapted onto the
// tools.skillLoader / tools.skillWriter seams via the eval-test-local adapters below. The
// loader/writer are rooted at the supplied skillsRoot (a t.TempDir-rooted active/pending/
// archived tree) + exportDir (the host materialization target the D-01 host by-path use
// frame points at); pool may be nil for a loader-only structural assertion (the no-key
// TestRegistrySnippetReuse_HasSkillTool path), and is the live pool for the steady-state
// gate (the write actions persist their audit through it).
func buildSnippetReuseRegistry(cfg *config.Config, workspace, skillsRoot, exportDir string, pool *pgxpool.Pool) *tools.Registry {
	reg := buildSeamFreeSkillsRegistry(cfg, workspace)
	loader := skills.NewLoader(skills.Config{
		Roots:        []string{skillsRoot},
		BodyCapBytes: cfg.SkillBodyCapBytes,
		Blocklist:    cfg.SkillInjectionBlocklist,
	})
	skillTool := &tools.SkillTool{
		Loader: &evalSkillLoaderAdapter{loader: loader, manCap: cfg.SkillManifestCapBytes, exportDir: exportDir},
	}
	if pool != nil {
		w := skills.NewWriter(skills.WriterConfig{
			Pool:         pool,
			PendingDir:   filepath.Join(skillsRoot, "pending"),
			ActiveDir:    skillsRoot,
			ExportDir:    exportDir,
			ArchiveDir:   filepath.Join(skillsRoot, "archived"),
			Blocklist:    cfg.SkillInjectionBlocklist,
			BodyCapBytes: cfg.SkillBodyCapBytes,
		})
		skillTool.Writer = &evalSkillWriterAdapter{w: w}
	}
	reg.Register(skillTool)
	return reg
}

// evalSkillLoaderAdapter is an eval-test-only adapter — NEVER a production surface. It
// mirrors cmd/aura/serve_adapters.go's skillLoaderAdapter over a live *skills.Loader.
type evalSkillLoaderAdapter struct {
	loader    *skills.Loader
	manCap    int
	exportDir string
}

func (a *evalSkillLoaderAdapter) List() []tools.SkillMeta {
	loaded := a.loader.List()
	out := make([]tools.SkillMeta, 0, len(loaded))
	for _, s := range loaded {
		out = append(out, tools.SkillMeta{Name: s.Name, Description: s.Description})
	}
	return out
}

func (a *evalSkillLoaderAdapter) Body(name string) (string, bool) {
	s, ok := a.loader.Get(name)
	if !ok {
		return "", false
	}
	return s.Body, true
}

func (a *evalSkillLoaderAdapter) ManifestDescription() string {
	return skills.RenderManifest(a.loader.List(), a.manCap)
}

func (a *evalSkillLoaderAdapter) Snippet(name string) (instructions, hostPath, interpreter string, ok bool) {
	s, found := a.loader.Get(name)
	if !found || s.Type != skills.TypeSnippet {
		return "", "", "", false
	}
	path, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
	if perr != nil {
		return "", "", "", false
	}
	return s.Body, path, interp, true
}

// evalSkillWriterAdapter is an eval-test-only adapter mirroring serve_adapters.go's
// skillWriterAdapter (actor "model"; cli ApprovalSource for restore/archive).
type evalSkillWriterAdapter struct {
	w *skills.Writer
}

func (a *evalSkillWriterAdapter) WriteMutation(ctx context.Context, action, name, description, body string, always bool) (string, error) {
	return a.w.WriteMutationByName(ctx, action, name, description, body, always, skills.AuditActor{ActorID: "model"})
}

func (a *evalSkillWriterAdapter) SaveSnippet(ctx context.Context, name, language, code, description string, needsNetwork, needsWorkspace bool) (string, error) {
	fm := skills.Frontmatter{
		Name:           name,
		Description:    description,
		NeedsNetwork:   needsNetwork,
		NeedsWorkspace: needsWorkspace,
	}
	res, err := a.w.SaveSnippet(ctx, name, language, code, fm, skills.AuditActor{ActorID: "model"})
	if err != nil {
		return "", err
	}
	return res.Status, nil
}

func (a *evalSkillWriterAdapter) Restore(ctx context.Context, name string) (string, error) {
	if err := a.w.Restore(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "model"}); err != nil {
		return "", err
	}
	return skills.StatusActive, nil
}

func (a *evalSkillWriterAdapter) ArchiveSnippet(ctx context.Context, name string) (string, error) {
	if err := a.w.Archive(ctx, name, skills.ApprovalCLI, skills.AuditActor{ActorID: "model"}); err != nil {
		return "", err
	}
	return "archived", nil
}
