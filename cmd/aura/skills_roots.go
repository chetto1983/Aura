package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skillacl"
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
	cfg    *config.Config
	shared *skills.SharedReader
	mu     sync.Mutex
	byID   map[string]*cachedLoader
	// invalidated is when the last write expired every cached answer. A grant set resolved
	// before that instant is stale however late it arrives, which is the only thing that
	// makes invalidateAll's promise ("visible to the next read") true while a query for the
	// same identity is already in flight.
	invalidated time.Time
}

// cachedLoader is one identity's loader plus the shared-skill set it was built for. The set
// is remembered so a re-resolve that finds the same grants keeps the SAME loader — rebuilding
// it would throw the snapshot away, which is the cost this cache exists to avoid.
//
// The two clocks answer different questions and must not be collapsed into one. `resolved` is
// when the answer was INSTALLED, and it is what the TTL counts from. `asked` is when the read
// that produced the answer STARTED, and it is the staleness watermark: an answer is stale
// relative to another by when the two reads BEGAN, never by when they happened to finish.
// Comparing an incoming read's start against the stored INSTALL time conflates them, and it
// rejects the fresher of two racing answers whenever the older read lands first — see install.
type cachedLoader struct {
	loader   *skills.Loader
	shared   []string
	asked    time.Time
	resolved time.Time
}

// sharedRecheckTTL is how long a resolved grant set is trusted before the next read asks the
// database again. It matches the Loader's own snapshot TTL on purpose: a share and a skill
// edit become visible on the same cadence, so an operator who grants and then looks does not
// have to know which of the two clocks they are waiting on. Grants are written by a different
// process (`aura skills share`), so nothing but this expiry can notice one.
const sharedRecheckTTL = time.Second

func newIdentityLoaders(cfg *config.Config, shared *skills.SharedReader) *identityLoaders {
	return &identityLoaders{cfg: cfg, shared: shared, byID: map[string]*cachedLoader{}}
}

// invalidateAll expires every cached snapshot AND every resolved grant set, so a completed
// write is visible to the next read in the same turn — for every reader, not only the writer:
// a write can change what a grantee sees (deleting a skill takes its grants with it), and the
// writer does not know who has a warm loader.
func (l *identityLoaders) invalidateAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.invalidated = time.Now()
	for _, entry := range l.byID {
		entry.loader.Invalidate()
		entry.resolved = time.Time{}
	}
}

// forIdentity returns the loader scoped to identity ("" = the deployment library alone).
//
// The grant lookup runs WITHOUT the mutex held. It is a database round-trip, and holding the
// map lock across it would make one identity's slow query the whole deployment's stall.
//
// Two callers racing therefore pay a redundant query — and they do NOT necessarily read the
// same grants: a revoke landing between the two reads gives the later one a smaller answer,
// and whichever query returned last would otherwise win the map. So the moment each read
// STARTED is carried into install, and an answer older than the one already stored is
// dropped. Without that, a revoked share could be re-installed by an in-flight query and
// stay readable for another whole TTL, repeatedly, under exactly the concurrency a busy
// deployment has.
func (l *identityLoaders) forIdentity(ctx context.Context, identity string) *skills.Loader {
	identity = strings.TrimSpace(identity)
	if loader, fresh := l.cached(identity); fresh {
		return loader
	}
	asked := time.Now()
	return l.install(identity, l.sharedDirs(ctx, identity), asked)
}

// cached returns the identity's loader and whether its grant set is still within the TTL.
func (l *identityLoaders) cached(identity string) (*skills.Loader, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.byID[identity]
	if !ok {
		return nil, false
	}
	return entry.loader, time.Since(entry.resolved) < sharedRecheckTTL
}

// install records a grant set resolved by a read that started at asked. An unchanged set only
// refreshes the clock: the loader, and the snapshot inside it, survive. An answer that is
// older than the stored one is discarded rather than installed — see forIdentity.
func (l *identityLoaders) install(identity string, shared []string, asked time.Time) *skills.Loader {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.byID[identity]; ok {
		// Older than what is already stored, or older than the write that expired it: drop
		// it. The entry keeps its expired timestamp, so the next read resolves again at once
		// rather than serving this answer for a whole TTL.
		//
		// The comparison is start-against-START. Against the stored INSTALL time it would
		// reject the fresher of two racing answers in one of the two landing orders: a read
		// that began earlier, and whose query was the slow one, installs at a moment already
		// later than when the newer read began, so the newer read's revoke would be discarded
		// and the stale grant kept with a fresh clock — the very thing this guard exists to
		// stop, reached by swapping which query finished first.
		if entry.asked.After(asked) || l.invalidated.After(asked) {
			return entry.loader
		}
		if slices.Equal(entry.shared, shared) {
			entry.asked, entry.resolved = asked, time.Now()
			return entry.loader
		}
	}
	// The same staleness question on a cold cache, where there is no entry to fall back on:
	// a loader must be returned, so the answer is USED but not CACHED — the zero clock makes
	// the next read resolve again instead of serving it for a whole TTL.
	resolved := time.Now()
	if l.invalidated.After(asked) {
		resolved = time.Time{}
	}
	loader := skills.NewLoader(skills.Config{
		Roots:        skillLoaderRoots(l.cfg, identity),
		SharedDirs:   shared,
		BodyCapBytes: l.cfg.SkillBodyCapBytes,
		Blocklist:    l.cfg.SkillInjectionBlocklist,
	})
	l.byID[identity] = &cachedLoader{loader: loader, shared: shared, asked: asked, resolved: resolved}
	return loader
}

// sharedDirs resolves the skill directories other identities have shared with this reader.
//
// A lookup that FAILS degrades to none, loudly. That direction is the security-relevant one:
// carrying the previous answer forward would keep a REVOKED skill readable for as long as the
// database stayed unreachable, and a share is an addition to a library that works without it,
// never a dependency of the turn.
func (l *identityLoaders) sharedDirs(ctx context.Context, identity string) []string {
	if l.shared == nil || identity == "" {
		return nil
	}
	shared, err := l.shared.For(ctx, identity)
	if err != nil {
		slog.Error("skills: could not resolve what is shared with this identity — reading their own library only",
			"identity_id", identity, "err", err)
		return nil
	}
	return skills.SharedDirs(shared)
}

// cliSharedDirs answers "what is shared with this identity" for the operator's read-only CLI
// paths, which are otherwise pool-free.
//
// It opens its own pool and closes it, and it is BEST EFFORT: a database that cannot be
// reached costs the operator the shared rows and a line on stderr saying so, never the
// command. `aura skills list` with no --identity keeps its exact pre-#214 behaviour — it does
// not dial at all — because nothing is shared with nobody and a listing of the house library
// must not acquire a database dependency it never had.
func cliSharedDirs(ctx context.Context, cfg *config.Config, identity string) []string {
	if cfg == nil || strings.TrimSpace(identity) == "" {
		return nil
	}
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: cannot read what is shared with this identity, listing their own roots only:", err)
		return nil
	}
	defer pool.Close()
	shared, err := newSharedSkillReader(cfg, pool).For(ctx, identity)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warn: cannot read what is shared with this identity, listing their own roots only:", err)
		return nil
	}
	return skills.SharedDirs(shared)
}

// newSharedSkillReader builds the grants→bodies join the per-identity loaders and the box
// resolver both read (amendment #214 criterion 5). It returns nil — "nothing is shared with
// anybody" — for every composition that has no pool, which is the honest answer where there
// is no ACL to consult.
func newSharedSkillReader(cfg *config.Config, pool *pgxpool.Pool) *skills.SharedReader {
	if cfg == nil || pool == nil {
		return nil
	}
	acl, err := skillacl.NewStore(pool)
	if err != nil {
		slog.Error("skills: resource ACL unavailable — no shared skill will be readable", "err", err)
		return nil
	}
	return skills.NewSharedReader(acl, skills.NewCatalogStore(pool), skillLayout(cfg))
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
func alwaysBlockProvider(cfg *config.Config, shared *skills.SharedReader) func(context.Context) string {
	if cfg == nil || cfg.SkillsDir == "" {
		return func(context.Context) string { return "" }
	}
	loaders := newIdentityLoaders(cfg, shared)
	// The catalogue renders through the SAME adapter the skill tool's action=list uses,
	// so there is one manifest renderer (cap + BM25 overflow tail included), not two that
	// can drift — and it resolves the identity off the same context this provider reads.
	catalogue := skilladapters.NewIdentityLoader(loaders.forIdentity, loaders.invalidateAll, cfg.SkillManifestCapBytes)
	return func(ctx context.Context) string {
		var b strings.Builder
		if block, present := skills.RenderAlwaysBlock(loaders.forIdentity(ctx, identityctx.IdentityID(ctx)).List()); present {
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
