package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skilladapters"
	"github.com/chetto1983/aura/internal/skills"
)

// skills_roots.go is where the composition root decides WHOSE skills a reader sees
// (amendment #214). It holds the config→skills.Layout bridge, the loader scan roots, and the
// per-identity always-block provider — the three places that used to answer "the deployment's,
// obviously" and now have to answer with a name.
//
// It was split out of serve_adapters.go on touch: that file was at 580 LOC and these are one
// concern, not scattered wiring.

// skillLayout projects the three configured skill bases onto the layout skills.Layout.For
// resolves an identity's roots from. It is the ONLY place cfg's skill dirs become a layout,
// so no caller joins a per-identity skills path by hand.
func skillLayout(cfg *config.Config) skills.Layout {
	if cfg == nil {
		return skills.Layout{}
	}
	return skills.Layout{
		Global:     cfg.SkillsDir,
		Identities: cfg.SkillsIdentityDir,
		Export:     cfg.SkillExportDir,
	}
}

// skillLoaderRoots is the single source of truth for the loader scan roots
// (amendment #51 / D-40 + #50, amendment #214).
//
// For an unscoped caller there is exactly one: the active SkillsDir, where every global write
// path lands. For an identity there are two — their own root FIRST and the global root LAST —
// which reads backwards and is not a slip: the Loader merges with LATER-ROOT-WINS, so global
// last means the operator's skill WINS a name collision (D-214-3). A skill the operator ships
// is house policy; a person quietly shadowing it is how the policy stops applying without
// anyone noticing. The ordering itself lives in skills.Roots.LoaderRoots, asserted by test
// there.
//
// It used to scan <export>/.agents/skills first, the landing zone for an in-sandbox
// `npx skills add` back when /skills was a read-write bind mount. D-10 replaced that mount
// with a docker-cp'd copy MaterializeIn clears at every create and resume, and shell_exec
// now routes a skills-install in the box to the host pipeline, so nothing could reach that
// directory from either side. Measured absent on the running deployment (amendment #208),
// it is no longer scanned; nothing recreates it, so a future landing zone is a deliberate
// re-declaration rather than an inherited one.
func skillLoaderRoots(cfg *config.Config, identity string) []string {
	roots, err := skillLayout(cfg).For(identity)
	if err != nil {
		// A crafted identity that cannot name a directory falls back to the deployment's own
		// skills — never to another identity's, and never to a silent empty set that would
		// leave the agent with no house skills at all and no line saying why.
		slog.Warn("skills: identity cannot name a root, falling back to the deployment library",
			"identity_id", identity, "err", err)
		return []string{cfg.SkillsDir}
	}
	return roots.LoaderRoots()
}

// identityLoaders hands out one *skills.Loader per identity, built once and reused.
//
// A Loader is a mutex-guarded snapshot with a one-second TTL and no goroutine, so keeping one
// per identity costs a map entry and buys the re-scan cadence the always-block depends on:
// rebuilding a Loader per turn would throw the snapshot away every time and re-walk both
// roots on every message. Entries are never evicted — the key set is the identity set, which
// is bounded by the deployment's people, not by traffic.
type identityLoaders struct {
	cfg  *config.Config
	mu   sync.Mutex
	byID map[string]*skills.Loader
}

func newIdentityLoaders(cfg *config.Config) *identityLoaders {
	return &identityLoaders{cfg: cfg, byID: map[string]*skills.Loader{}}
}

// invalidateAll expires every cached snapshot, so a completed write is visible to the next
// read in the same turn — for every reader, not only the writer: a write can change what a
// grantee sees, and the writer does not know who has a warm loader.
func (l *identityLoaders) invalidateAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, loader := range l.byID {
		loader.Invalidate()
	}
}

// forIdentity returns the loader scoped to identity ("" = the deployment library alone).
func (l *identityLoaders) forIdentity(identity string) *skills.Loader {
	identity = strings.TrimSpace(identity)
	l.mu.Lock()
	defer l.mu.Unlock()
	if loader, ok := l.byID[identity]; ok {
		return loader
	}
	loader := skills.NewLoader(skills.Config{
		Roots:        skillLoaderRoots(l.cfg, identity),
		BodyCapBytes: l.cfg.SkillBodyCapBytes,
		Blocklist:    l.cfg.SkillInjectionBlocklist,
	})
	l.byID[identity] = loader
	return loader
}

// alwaysBlockProvider returns a per-turn renderer of the messages[1] always-block
// (D-07): it renders the always:true bodies via skills.RenderAlwaysBlock on each call, so a
// skill add/remove changes messages[1] live (the loader's short TTL re-scans). A nil cfg /
// empty skills dir yields a provider that always renders empty (no always-block turn). The
// loaders are goroutine-free (lazy TTL re-scan), so this provider adds no background goroutine.
//
// The block is rendered for the TURN'S IDENTITY (amendment #214): a skill is executable
// instruction the model follows, so an always-on skill of one person must not be prepended to
// another person's turn. A context with no identity renders the deployment's own always-on
// skills exactly as before.
func alwaysBlockProvider(cfg *config.Config) func(context.Context) string {
	if cfg == nil || cfg.SkillsDir == "" {
		return func(context.Context) string { return "" }
	}
	loaders := newIdentityLoaders(cfg)
	// The catalogue renders through the SAME adapter the skill tool's action=list uses,
	// so there is one manifest renderer (cap + BM25 overflow tail included), not two that
	// can drift — and it resolves the identity off the same context this provider reads.
	catalogue := skilladapters.NewIdentityLoader(loaders.forIdentity, loaders.invalidateAll, cfg.SkillManifestCapBytes)
	return func(ctx context.Context) string {
		var b strings.Builder
		if block, present := skills.RenderAlwaysBlock(loaders.forIdentity(identityctx.IdentityID(ctx)).List()); present {
			b.WriteString(block)
		}
		// The catalogue of installed skills lives HERE, not in the skill tool's
		// Description. Both are per-turn live state, but messages[1] is the seam built
		// for that (rebuilt each turn, never touching messages[0]), whereas the tool
		// Description sits inside the `tools` array — so every skill add/remove rewrote
		// the tools payload and invalidated the provider's prefix cache. It also made
		// `skill` the heaviest entry in the manifest at ~1773 tokens against ~400 for the
		// constant text that replaced it. hermes-agent renders its index into the system
		// prompt for the same reason; Claude Code keeps the listing beside the tool, not
		// inside it.
		if manifest := strings.TrimSpace(catalogue.ManifestDescription(ctx)); manifest != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(skillCatalogueHeader)
			b.WriteString(manifest)
		}
		return b.String()
	}
}

// skillCatalogueHeader is a frozen English literal (D-06): the block must stay
// byte-stable between skill changes or it defeats the prefix cache it was moved here
// to protect.
const skillCatalogueHeader = "Installed skills (call the skill tool with action=use <name> to apply one):\n\n"
