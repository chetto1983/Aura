//go:build cot_eval

// skills_adapters_cot_eval.go holds the eval-package bridges from the live
// internal/skills primitives onto the unexported tools.skill* seams the `skill` tool
// dispatches against. They mirror cmd/aura/serve_adapters.go EXACTLY (the composition
// root's adapters live in the main package the eval tier cannot import), so the
// TestSkillsE2E harness exercises the same Loader/Writer/Installer/CatalogClient the
// production boot path wires — not a fake. The model is the actor on every write/install
// path (allowBlocklisted=false is enforced inside internal/skills).
package eval

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/skills"
)

// evalSkillLoader bridges *skills.Loader onto tools.skillLoader (the read/manifest seam).
type evalSkillLoader struct {
	loader *skills.Loader
	manCap int
}

func (a *evalSkillLoader) List() []tools.SkillMeta {
	loaded := a.loader.List()
	out := make([]tools.SkillMeta, 0, len(loaded))
	for _, s := range loaded {
		out = append(out, tools.SkillMeta{Name: s.Name, Description: s.Description})
	}
	return out
}

func (a *evalSkillLoader) Body(name string) (string, bool) {
	s, ok := a.loader.Get(name)
	if !ok {
		return "", false
	}
	return s.Body, true
}

func (a *evalSkillLoader) ManifestDescription() string {
	return skills.RenderManifest(a.loader.List(), a.manCap)
}

func (a *evalSkillLoader) Snippet(name string) (instructions, sandboxPath, interpreter string, ok bool) {
	s, found := a.loader.Get(name)
	if !found || s.Type != skills.TypeSnippet {
		return "", "", "", false
	}
	path, interp, err := skills.SnippetInvocation(name, s.Language)
	if err != nil {
		return "", "", "", false
	}
	return s.Body, path, interp, true
}

// evalSkillWriter bridges *skills.Writer onto tools.skillWriter (create/update/delete).
type evalSkillWriter struct {
	w *skills.Writer
}

func (a *evalSkillWriter) WriteMutation(ctx context.Context, action, name, description, body string, always bool) (string, error) {
	return a.w.WriteMutationByName(ctx, action, name, description, body, always, skills.AuditActor{ActorID: "model"})
}

// evalSkillCatalog bridges *skills.CatalogClient onto tools.skillCatalog (action=catalog).
type evalSkillCatalog struct {
	client *skills.CatalogClient
}

func (a *evalSkillCatalog) Search(ctx context.Context, query string) ([]tools.CatalogResult, error) {
	items, err := a.client.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]tools.CatalogResult, 0, len(items))
	for _, it := range items {
		name := it.Name
		if name == "" {
			name = it.SkillID
		}
		out = append(out, tools.CatalogResult{Name: name, Source: it.Source, Installs: it.Installs, SkillID: it.SkillID})
	}
	return out, nil
}

func (a *evalSkillCatalog) Disabled() bool { return a.client.Disabled() }

// evalSkillInstaller bridges *skills.Installer onto tools.skillInstaller (action=install).
type evalSkillInstaller struct {
	inst *skills.Installer
}

func (a *evalSkillInstaller) Install(ctx context.Context, repoURL, skillName string) (tools.InstallSummary, error) {
	res, err := a.inst.Install(ctx, repoURL, skills.InstallOpts{Actor: skills.AuditActor{ActorID: "model"}, SkillName: skillName})
	if err != nil {
		return tools.InstallSummary{}, err
	}
	flags := make([]string, 0, len(res.RedFlags))
	for _, f := range res.RedFlags {
		flags = append(flags, f.Detail)
	}
	return tools.InstallSummary{
		Name:           res.Name,
		ContentHash:    res.ContentHash,
		AlwaysStripped: res.AlwaysStripped,
		RedFlags:       flags,
		Tier:           string(res.Tier),
	}, nil
}
