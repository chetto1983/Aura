//go:build cot_eval

// skills_snippet_reuse_registry_cot_eval_test.go is the eval↔production registry-parity
// half of the 18-04 snippet-reuse gate (split from skills_cot_eval_test.go per the
// 600-LOC cap). It carries the SIBLING registry constructor that registers the PRODUCTION
// `skill` tool (live loader + writer over a temp skills root) on top of the seam-free set.
//
// The loader/writer adapters that bridge *skills.Loader / *skills.Writer onto the
// tools.skillLoader / tools.skillWriter consumer-declared seams now live in the SHARED
// internal/skilladapters package (IN-04), importable from BOTH cmd/aura (the production
// composition root) AND here — so this eval registry uses the SAME adapter the production
// `skill` tool uses, with no eval-test-local mirror to drift. The key-free
// TestRegistrySnippetReuse_HasSkillTool (classify_cot_eval_test.go) guards the parity so a
// regression fails CI's structural slot.
package eval

import (
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/skilladapters"
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
		Loader: skilladapters.NewLoader(loader, cfg.SkillManifestCapBytes, exportDir),
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
		skillTool.Writer = skilladapters.NewWriter(w)
	}
	reg.Register(skillTool)
	return reg
}
