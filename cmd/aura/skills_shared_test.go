package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/skillacl"
	"github.com/chetto1983/aura/internal/skills"
)

// skills_shared_test.go is the composition root's half of amendment #214 criterion 5: a
// standing grant reaches BOTH consumers — the reader's loader and the sources their box is
// filled from — and reaches them as one skill directory, never as the owner's tree.
//
// The two stores are faked. What is under test here is the wiring and the shape of the
// answer, not that Postgres can answer; the live grant is the db_integration tier's sentence
// and the box is the docker_integration tier's.

// stubGrants hands back canned resource ids, or an error. byReader, when set, answers each
// reader separately — which is what a test asserting that a grant reaches ONE person and not
// another needs; with it nil every reader gets ids, the simpler shape most callers want.
type stubGrants struct {
	ids      []string
	byReader map[string][]string
	err      error
}

func (s stubGrants) AccessibleResourceIDs(_ context.Context, readerID string, _ skillacl.ResourceType, _ skillacl.Perm) ([]string, error) {
	if s.byReader != nil {
		return s.byReader[readerID], s.err
	}
	return s.ids, s.err
}

// stubCatalog resolves those ids to catalog rows the way the RLS-scoped read would.
type stubCatalog struct {
	rows []skills.CatalogRow
	err  error
}

func (s stubCatalog) ListByIDs(context.Context, string, []string) ([]skills.CatalogRow, error) {
	return s.rows, s.err
}

// sharedFor builds a SharedReader over cfg's layout that reports exactly these rows as shared
// with EVERY reader.
func sharedFor(cfg *config.Config, rows ...skills.CatalogRow) *skills.SharedReader {
	return skills.NewSharedReader(stubGrants{ids: rowIDs(rows)}, stubCatalog{rows: rows}, skillLayout(cfg))
}

// sharedWith is sharedFor's scoped twin: only reader holds the grants, so a test can assert
// that somebody else's board and somebody else's box see nothing.
func sharedWith(cfg *config.Config, reader string, rows ...skills.CatalogRow) *skills.SharedReader {
	return skills.NewSharedReader(
		stubGrants{byReader: map[string][]string{reader: rowIDs(rows)}},
		stubCatalog{rows: rows},
		skillLayout(cfg),
	)
}

// rowIDs projects catalog rows onto their ids.
func rowIDs(rows []skills.CatalogRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// aliceExport resolves alice's export dir through the production layout.
func aliceExport(t *testing.T, cfg *config.Config, identity string) string {
	t.Helper()
	roots, err := skillLayout(cfg).For(identity)
	if err != nil {
		t.Fatalf("layout for %q: %v", identity, err)
	}
	return roots.Export
}

// TestSandboxMaterializeSourcesCarriesOnlyTheSharedSkill is the box half of criterion 5, and
// the containment property of amendment #215 stated for shares: the source is the shared
// SKILL's directory, so the unshared skill sitting beside it in the same export cannot be
// reached from it by any walk.
func TestSandboxMaterializeSourcesCarriesOnlyTheSharedSkill(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	export := aliceExport(t, cfg, rootsAlice)
	seedSkill(t, export, "alice-shared", "SHARED BODY", false)
	seedSkill(t, export, "alice-private", "PRIVATE BODY", false)

	shared := sharedFor(cfg, skills.CatalogRow{ID: "id-1", OwnerID: rootsAlice, Name: "alice-shared"})
	got, err := sandboxMaterializeSources(cfg, shared)(context.Background(), rootsBob)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("sources = %+v, want the share, bob's export and the house export", got)
	}
	want := usersandbox.MaterializeSource{
		HostDir: filepath.Join(export, "alice-shared"),
		Dest:    skills.InSandboxSkillsRoot + "/alice-shared",
		// Somebody else's tree, so a fault in it costs bob this skill and not his box
		// (amendment #217, D-217-4). The asymmetry itself is asserted in skills_roots_test.go.
		SkipOnFault: true,
	}
	if got[0] != want {
		t.Fatalf("shared source = %+v, want %+v", got[0], want)
	}
	// The share is listed FIRST so bob's own export and the house's both overwrite it on a
	// name collision — the same later-wins precedence the loader gives the model's context.
	if got[1].Dest != skills.InSandboxSkillsRoot || got[2].HostDir != cfg.SkillExportDir {
		t.Fatalf("sources are out of precedence order: %+v", got)
	}
	// Containment, as a property of the tree and not of the string: alice's OTHER skill must
	// not live under anything bob's box is filled from. A source that was the export TREE
	// would pass a name check and still carry it (amendment #215).
	for _, src := range got {
		rel, relErr := filepath.Rel(src.HostDir, filepath.Join(export, "alice-private"))
		if relErr == nil && !strings.HasPrefix(rel, "..") {
			t.Fatalf("alice's UNSHARED skill lives under a source of bob's box (%q) — tarDir would carry it in", src.HostDir)
		}
	}
}

// TestSandboxMaterializeSourcesDegradesWhenGrantsCannotBeRead pins the failure policy: a box
// is how its owner reaches every one of their own tools, so an unreadable ACL costs them the
// share, never the box — and costs them the share in the SAFE direction, since the mirror
// then removes what it can no longer justify.
func TestSandboxMaterializeSourcesDegradesWhenGrantsCannotBeRead(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	broken := skills.NewSharedReader(stubGrants{err: errors.New("database is unreachable")}, stubCatalog{}, skillLayout(cfg))

	got, err := sandboxMaterializeSources(cfg, broken)(context.Background(), rootsBob)
	if err != nil {
		t.Fatalf("a failed grant lookup denied the box entirely: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sources = %+v, want bob's own export and the house export", got)
	}
	for _, src := range got {
		if src.Dest != skills.InSandboxSkillsRoot {
			t.Fatalf("a shared source survived a failed lookup: %+v", src)
		}
	}
}

// TestSharedSkillIsReadableInTheListing is the listing half of criterion 5 at the composition
// root: the always-block's catalogue advertises a skill somebody shared with this reader, and
// the shared BODY never reaches the always-block itself.
func TestSharedSkillIsReadableInTheListing(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	seedSkill(t, cfg.SkillsDir, "house-rule", "HOUSE BODY", true)
	export := aliceExport(t, cfg, rootsAlice)
	seedSkill(t, export, "alice-shared", "SHARED BODY", true)
	seedSkill(t, export, "alice-private", "PRIVATE BODY", false)

	shared := sharedFor(cfg, skills.CatalogRow{ID: "id-1", OwnerID: rootsAlice, Name: "alice-shared"})
	block := alwaysBlockProvider(cfg, shared)(identityctx.WithIdentityID(context.Background(), rootsBob))

	if !strings.Contains(block, "alice-shared") {
		t.Fatalf("bob's catalogue does not list the skill shared with him:\n%s", block)
	}
	if strings.Contains(block, "alice-private") {
		t.Fatalf("bob's catalogue lists a skill alice never shared:\n%s", block)
	}
	// always:true belongs to its owner's turns. A grant makes a skill available; it does not
	// make it the grantee's standing instruction.
	if strings.Contains(block, "SHARED BODY") {
		t.Fatalf("a shared body was prepended to bob's turn:\n%s", block)
	}
	if !strings.Contains(block, "HOUSE BODY") {
		t.Fatalf("the house always-on skill stopped reaching bob:\n%s", block)
	}
}

// TestIdentityLoadersRebuildOnlyWhenTheGrantSetChanges pins the cache contract the per-turn
// grant lookup rests on: the same grants keep the same loader (and so the same snapshot),
// while a changed set replaces it.
func TestIdentityLoadersRebuildOnlyWhenTheGrantSetChanges(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	export := aliceExport(t, cfg, rootsAlice)
	seedSkill(t, export, "alice-shared", "SHARED BODY", false)
	row := skills.CatalogRow{ID: "id-1", OwnerID: rootsAlice, Name: "alice-shared"}

	loaders := newIdentityLoaders(cfg, sharedFor(cfg, row))
	ctx := context.Background()
	first := loaders.forIdentity(ctx, rootsBob)
	if _, ok := first.Get("alice-shared"); !ok {
		t.Fatal("the shared skill is not in bob's loader")
	}
	// invalidateAll expires the resolved grant set too, so this call really re-resolves — and
	// must still hand back the SAME loader, because the grants have not changed.
	loaders.invalidateAll()
	if again := loaders.forIdentity(ctx, rootsBob); again != first {
		t.Fatal("an unchanged grant set rebuilt the loader — every turn would throw the snapshot away")
	}

	// A revoked grant: the resolver now reports nothing, so the loader is replaced and the
	// body is gone from the listing.
	revoked := newIdentityLoaders(cfg, sharedFor(cfg))
	if _, ok := revoked.forIdentity(ctx, rootsBob).Get("alice-shared"); ok {
		t.Fatal("a revoked share is still readable")
	}
}

// TestIdentityLoadersRefuseAStaleGrantAnswer is the concurrency half of the per-turn grant
// lookup. The query runs OUTSIDE the map lock (deliberately — see forIdentity), so two reads
// that straddle a revoke return different answers and the one that finishes last would
// otherwise win the map. A share that was revoked would then be re-installed WITH A FRESH
// CLOCK and stay readable for another full TTL, again and again under load.
//
// install takes the instant its read STARTED, which is what makes this testable without
// racing goroutines: an answer older than what is stored, or older than the write that
// expired it, is dropped.
func TestIdentityLoadersRefuseAStaleGrantAnswer(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	export := aliceExport(t, cfg, rootsAlice)
	seedSkill(t, export, "alice-shared", "SHARED BODY", false)
	dir := filepath.Join(export, "alice-shared")

	t.Run("a slower read cannot undo a newer one", func(t *testing.T) {
		t.Parallel()
		loaders := newIdentityLoaders(cfg, nil)
		early := time.Now()
		// The read that started LATER lands first and reports the revoke.
		loaders.install(rootsBob, nil, early.Add(time.Millisecond))
		// The read that started EARLIER arrives afterwards still holding the grant.
		got := loaders.install(rootsBob, []string{dir}, early)
		if _, ok := got.Get("alice-shared"); ok {
			t.Fatal("an in-flight read re-installed a revoked share — it would stay readable for another TTL")
		}
	})

	// The MIRROR of the case above, and the one the first fix left open: the same two reads in
	// the other landing order. Here the read that started EARLIER lands first, so the entry's
	// clock is the moment IT was installed — a moment that is already later than when the newer
	// read started. Comparing the incoming read's START against that INSTALL time therefore
	// rejects the fresher answer, and the revoked share stays cached with a fresh clock for a
	// whole TTL — the exact outcome the guard exists to prevent, reached by swapping two lines
	// of timing. The watermark has to be start-vs-start.
	t.Run("a newer read is not rejected because an older one landed first", func(t *testing.T) {
		t.Parallel()
		loaders := newIdentityLoaders(cfg, nil)
		// Both reads STARTED before now; the grant-holding one started first and its query was
		// the slow one, so it installs before the read that already saw the revoke arrives.
		loaders.install(rootsBob, []string{dir}, time.Now().Add(-time.Hour))
		got := loaders.install(rootsBob, nil, time.Now().Add(-time.Minute))
		if _, ok := got.Get("alice-shared"); ok {
			t.Fatal("the newer read's revoke was dropped in favour of the older answer — the revoked share stays readable for another TTL")
		}
		if _, fresh := loaders.cached(rootsBob); !fresh {
			t.Fatal("the newer answer must be cached fresh, not left born-expired")
		}
	})

	t.Run("a read that predates a write cannot outlive it", func(t *testing.T) {
		t.Parallel()
		loaders := newIdentityLoaders(cfg, nil)
		loaders.install(rootsBob, nil, time.Now())
		asked := time.Now()
		time.Sleep(time.Millisecond)
		loaders.invalidateAll() // a skill write: every cached answer is expired

		got := loaders.install(rootsBob, []string{dir}, asked)
		if _, ok := got.Get("alice-shared"); ok {
			t.Fatal("an answer resolved before the write survived it — invalidateAll promises the next read sees the write")
		}
	})

	t.Run("on a cold cache the same answer is used but not cached", func(t *testing.T) {
		t.Parallel()
		// Nothing to fall back on, so the loader IS built from the stale answer — but its
		// clock must stay expired, or the next read would serve it for a whole TTL.
		loaders := newIdentityLoaders(cfg, nil)
		asked := time.Now()
		time.Sleep(time.Millisecond)
		loaders.invalidateAll()
		loaders.install(rootsBob, []string{dir}, asked)

		if _, fresh := loaders.cached(rootsBob); fresh {
			t.Fatal("an answer older than the write was cached as fresh")
		}
	})
}

// TestIdentityLoadersAreSafeUnderConcurrentTurns exercises the shape D-217-5 chose: the grant
// query runs OUTSIDE the map lock, so concurrent turns for the same identity really do read
// and install at the same time. Under -race this is what says the fields the cache mutates
// (the entry's clock, the invalidation epoch) are all reached under the mutex.
func TestIdentityLoadersAreSafeUnderConcurrentTurns(t *testing.T) {
	t.Parallel()
	cfg := rootsConfig(t)
	export := aliceExport(t, cfg, rootsAlice)
	seedSkill(t, export, "alice-shared", "SHARED BODY", false)
	loaders := newIdentityLoaders(cfg, sharedFor(cfg,
		skills.CatalogRow{ID: "id-1", OwnerID: rootsAlice, Name: "alice-shared"}))

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%4 == 3 {
				loaders.invalidateAll() // a skill write landing mid-turn
				return
			}
			if loaders.forIdentity(ctx, rootsBob) == nil {
				t.Error("forIdentity returned no loader")
			}
		}(i)
	}
	wg.Wait()
	if _, ok := loaders.forIdentity(ctx, rootsBob).Get("alice-shared"); !ok {
		t.Fatal("the standing share is gone after concurrent resolves")
	}
}

// TestCLISharedDirsDoesNotDialWithoutAnIdentity pins the one property that keeps the
// operator's read paths from acquiring a database dependency they never had: `aura skills
// list` with no --identity resolves to no shared dirs and never opens a pool. Nothing is
// shared with nobody, so there is nothing to ask.
func TestCLISharedDirsDoesNotDialWithoutAnIdentity(t *testing.T) {
	t.Parallel()
	// A DSN that cannot resolve: if this path dialled, the call would block or fail, and the
	// warn line would be printed. It returns immediately instead.
	cfg := rootsConfig(t)
	cfg.DB.URL = "postgres://nobody@127.0.0.1:1/aura"
	if got := cliSharedDirs(context.Background(), cfg, ""); got != nil {
		t.Fatalf("cliSharedDirs(unscoped) = %v, want nil with no dial", got)
	}
	if got := cliSharedDirs(context.Background(), cfg, "   "); got != nil {
		t.Fatalf("cliSharedDirs(blank identity) = %v, want nil with no dial", got)
	}
	if got := cliSharedDirs(context.Background(), nil, rootsBob); got != nil {
		t.Fatalf("cliSharedDirs(nil cfg) = %v, want nil", got)
	}
}
