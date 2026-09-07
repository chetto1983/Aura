//go:build db_integration

// Integration test for the Phase-29 cockpit skills install (Claude-Code parity, operator
// directive 2026-06-21): a cockpit install ACTIVATES directly — no approval pause, no staging
// ceremony, no "stage for approval" two-step. A real skillsWriteAdapter.Install (with a fake
// npx runner, so no network) stages + validates + then activates the skill against the live PG
// audit store; the skill is ACTIVE immediately and NO approval pause is minted in the
// cross-thread queue.
//
// It requires the migrations applied through 0010 (see
// internal/skills/audit_store_integration_test.go header for the env + invocation:
// POSTGRES_PASSWORD + AURA_DB_URL + AURA_DB_MIGRATE_URL on the live stack). No-skip-as-green:
// the env composition fails loudly under $CI when unset.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// skillsBridgeEnvOrSkip mirrors the integration-tier discipline: skip locally, fail-loud
// under $CI so a missing DSN never reports a falsely-green job.
func skillsBridgeEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// skillsBridgeConfig points a config at temp skill dirs so the adapter's Writer operates on a
// clean, isolated set of dirs. SkillsIdentityDir is set too, and it is not decoration: without
// a per-identity base every actor collapses back onto the deployment library, and the test
// would prove the pre-#214 behaviour while claiming to prove the scoped one.
func skillsBridgeConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.LoadDB()
	cfg.SkillsDir = filepath.Join(root, "active")
	cfg.SkillsIdentityDir = filepath.Join(root, "identities")
	cfg.SkillExportDir = filepath.Join(root, "export")
	cfg.SkillBodyCapBytes = 32768
	cfg.SkillInjectionBlocklist = []string{"<|im_start|>"}
	for _, d := range []string{cfg.SkillsDir, cfg.SkillsIdentityDir, cfg.SkillExportDir, filepath.Join(cfg.SkillsDir, skills.StageArchived)} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return cfg
}

// bridgeIdentity seeds one real aura.identities row and returns its uuid. The catalog row a
// scoped write cuts references it, so an actor that names no identity is not a scoped actor at
// all — it is a filesystem-only one, and this test is about the pair.
func bridgeIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "skills-bridge-"+id); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

// fakeNpxAdd stages a SKILL.md tree under <dir>/.claude/skills/<name>/ exactly as
// `npx skills add` would, so the transport runs end to end WITHOUT a network call.
func fakeNpxAdd(name, body string) skills.CommandRunner {
	return func(_ context.Context, dir, _ string, _ ...string) (string, error) {
		skillDir := filepath.Join(dir, ".claude", "skills", name)
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			return "", err
		}
		md := "---\nname: " + name + "\ndescription: install live fixture\ntype: instruction\n---\n" + body
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o600); err != nil {
			return "", err
		}
		return "Installed 1 skill\n", nil
	}
}

// buildSkillsInstall builds the live pool, a Writer over cfg's dirs, and the adapter with a
// fake-npx Installer — the direct-activate install chain under test. It also returns the
// askuser.Store so the test can assert NO approval pause was minted.
func buildSkillsInstall(t *testing.T, cfg *config.Config, skillName, body string) (skillsWriteAdapter, skillsBoardAdapter, *askuser.Store, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := skillsBridgeEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, skillsBridgeEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := skillsBridgeEnvOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	pause := askuser.New(pool)
	writer := newSkillWriter(cfg, pool)
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       writer,
		Run:          fakeNpxAdd(skillName, body),
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
	// The write adapter and the board adapter share ONE loader cache, exactly as the daemon
	// wires them (serve_agui.go). That sharing is the subject of the assertion below: the
	// write expires the snapshot the read is about to take, so "the board shows it" is a fact
	// about the console and not about how long the test happened to sleep.
	loaders := newIdentityLoaders(cfg, newSharedSkillReader(cfg, pool))
	adapter := skillsWriteAdapter{
		installer:    installer,
		writer:       writer,
		layout:       skillLayout(cfg),
		blocklist:    cfg.SkillInjectionBlocklist,
		bodyCapBytes: cfg.SkillBodyCapBytes,
		invalidate:   loaders.invalidateAll,
	}
	board := skillsBoardAdapter{loaders: loaders, layout: skillLayout(cfg), audit: skills.NewAuditStore(pool)}
	return adapter, board, pause, pool
}

// TestSkillsInstallActivatesDirectly is the Claude-Code-parity no-skip-as-green backstop:
// Install stages + validates + ACTIVATES the skill in one step (status "active", loadable),
// mints NO approval pause in the cross-thread queue, and leaves nothing in pending/.
//
// Since amendment #214 it also carries the invariant amendment #216 stated and could not then
// satisfy: A CONSOLE'S WRITE LANDS WHERE ITS READ LOOKS. The install is made AS an identity, and
// what is asserted is not only that the tree exists but that the board of THAT person lists it
// and the board of another person does not. That pair is the whole point — either half alone is
// the half-and-half configuration #216 measured as worse than either whole choice.
func TestSkillsInstallActivatesDirectly(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "br-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, board, pause, pool := buildSkillsInstall(t, cfg, name, "a clean install body")
	ctx := t.Context()
	actor := bridgeIdentity(t, pool)
	other := bridgeIdentity(t, pool)

	info, err := adapter.Install(ctx, actor, "owner/"+name)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if info.Status != "active" {
		t.Fatalf("install status = %q, want active (direct activation, no ceremony)", info.Status)
	}

	// WHERE it landed: the actor's own root, not the deployment's.
	scoped, err := adapter.forActor(actor)
	if err != nil {
		t.Fatalf("writer for actor: %v", err)
	}
	if !scoped.ActiveExists(name) {
		t.Fatal("skill must be ACTIVE in the ACTOR's root immediately after install (no approval step)")
	}
	wantRoot := filepath.Join(cfg.SkillsIdentityDir, actor)
	if scoped.ActiveDir() != wantRoot {
		t.Fatalf("actor active dir = %q, want %q", scoped.ActiveDir(), wantRoot)
	}
	if info.Destination != filepath.Join(wantRoot, name) {
		t.Fatalf("reported destination = %q, want the actor's own root", info.Destination)
	}

	// WHERE the read looks: the same person's board sees it, and nobody else's does.
	if !boardLists(board, ctx, actor, name) {
		t.Fatal("the actor's own board must list the skill their console just installed (#216 invariant)")
	}
	if boardLists(board, ctx, other, name) {
		t.Fatal("another identity's board must NOT list a skill installed into the actor's root")
	}
	if _, ok := board.SkillBody(identityctx.WithIdentityID(ctx, other), name); ok {
		t.Fatal("another identity must not resolve the body either — listing and body cannot disagree")
	}

	// No approval pause minted — the cross-thread queue carries nothing about this install
	// (the unique uuid8 name makes the scan robust against concurrent tests sharing the PG).
	pendings, err := pause.ListPendingAll(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingAll: %v", err)
	}
	for i := range pendings {
		if strings.Contains(string(pendings[i].ResumeContext), name) || strings.Contains(pendings[i].Question, name) {
			t.Fatalf("install must NOT mint an approval pause, found one referencing %q", name)
		}
	}

	// The staging dir was consumed by the rename into active. The name mirrors the skills
	// package's unexported stagingDirName; asserting it EMPTY is the honest form — the
	// assertion this replaced stat'd a "pending/" directory no writer has created since
	// amendment #97 removed the approval step, so it could only ever pass.
	//
	// A MISSING staging dir fails here rather than passing quietly, and that is the whole
	// difference from the assertion it replaces: writeActive MkdirAll's <active>/.staging and
	// only renames the tree OUT of it, so after an install the directory exists and is empty.
	// Tolerating its absence would rebuild the same vacuum in a new place — an assertion that
	// cannot fail because the thing it looks at is never there.
	entries, rerr := os.ReadDir(filepath.Join(scoped.ActiveDir(), skillsStagingDirName))
	if rerr != nil {
		t.Fatalf("the install must leave the staging dir behind (empty), reading it: %v", rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("activated install must leave the staging dir empty, found %d entries", len(entries))
	}
}

// skillsStagingDirName mirrors the skills package's unexported stagingDirName. It is spelled
// here because the assertion above is about the directory the writer really leaves on disk,
// and a constant is where that coupling is visible instead of buried in a string literal.
const skillsStagingDirName = ".staging"

// boardLists reports whether identity's board carries name.
func boardLists(board skillsBoardAdapter, ctx context.Context, identity, name string) bool {
	for _, sk := range board.ActiveSkills(identityctx.WithIdentityID(ctx, identity)) {
		if sk.Name == name {
			return true
		}
	}
	return false
}

// TestSkillsBoardArchiveIsScopedWithTheBoard closes the half amendment #216 named and #217
// left open: ArchivedSkills reads the archive of the SAME root the caller's writer archives
// into. A global archive beside a scoped active list is the configuration #216 calls worse
// than either whole choice, so it is asserted directly rather than inferred.
func TestSkillsBoardArchiveIsScopedWithTheBoard(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "ar-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, board, _, pool := buildSkillsInstall(t, cfg, name, "archived fixture body")
	ctx := t.Context()
	actor := bridgeIdentity(t, pool)
	other := bridgeIdentity(t, pool)

	if _, err := adapter.Mutate(ctx, actor, "create", name, "archive fixture", "BODY", false); err != nil {
		t.Fatalf("Mutate create: %v", err)
	}
	if err := adapter.Archive(ctx, actor, name); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	staged, err := board.ArchivedSkills(identityctx.WithIdentityID(ctx, actor))
	if err != nil {
		t.Fatalf("ArchivedSkills(actor): %v", err)
	}
	if !stagedHas(staged, name) {
		t.Fatalf("the actor's archive must hold %q after their own Archive, got %+v", name, staged)
	}
	otherStaged, err := board.ArchivedSkills(identityctx.WithIdentityID(ctx, other))
	if err != nil {
		t.Fatalf("ArchivedSkills(other): %v", err)
	}
	if stagedHas(otherStaged, name) {
		t.Fatal("another identity's archive must not hold a skill this actor archived")
	}
}

// TestSkillsArchiveReachesTheHouseForAnOperator is the live proof of the 2026-09-07
// regression: a skill sitting in the HOUSE root — which is where every skill installed before
// the per-identity roots still lives — must be archivable by an operator holding
// governance.write. Before this, Archive resolved to the actor's own root, found nothing, and
// answered with a sentinel naming a library the operator was not looking at.
func TestSkillsArchiveReachesTheHouseForAnOperator(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "hs-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, _, _, pool := buildSkillsInstall(t, cfg, name, "house fixture body")
	ctx := t.Context()
	operator := bridgeIdentity(t, pool)
	adapter.capabilities = stubCapabilities{operator: true}

	// The skill is house policy: it lives in the deployment root and in nobody's own root.
	seedSkill(t, cfg.SkillsDir, name, "HOUSE BODY", false)

	if err := adapter.Archive(ctx, operator, name); err != nil {
		t.Fatalf("an operator must be able to archive house policy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillsDir, name, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("the house skill is still active after Archive (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillsDir, skills.StageArchived, name, "SKILL.md")); err != nil {
		t.Fatalf("the house skill did not land in the deployment archive: %v", err)
	}
}

// TestSkillsArchiveKeepsATenantOutOfTheHouse is the fence, proven live: without the capability
// the same call must fail rather than reach the deployment's library.
func TestSkillsArchiveKeepsATenantOutOfTheHouse(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "ht-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, _, _, pool := buildSkillsInstall(t, cfg, name, "house fixture body")
	ctx := t.Context()
	tenant := bridgeIdentity(t, pool)
	adapter.capabilities = stubCapabilities{}

	seedSkill(t, cfg.SkillsDir, name, "HOUSE BODY", false)

	if err := adapter.Archive(ctx, tenant, name); err == nil {
		t.Fatal("a tenant without governance.write must not archive house policy")
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillsDir, name, "SKILL.md")); err != nil {
		t.Fatalf("the refused archive must leave the house skill in place: %v", err)
	}
}

// TestSkillsProvidersCarryACapabilitySource pins the WIRING, which is the half a unit test
// cannot see and the half that decides whether any of this reaches the running daemon: both
// the write provider and the board must be built with a capability source, or the house verbs
// stay dead in production while every test above passes.
func TestSkillsProvidersCarryACapabilitySource(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "wr-" + uuid.Must(uuid.NewV7()).String()[:8]
	_, _, _, pool := buildSkillsInstall(t, cfg, name, "wiring fixture body")

	provider := buildSkillsWriteProvider(cfg, pool, nil)
	adapter, ok := provider.(skillsWriteAdapter)
	if !ok {
		t.Fatalf("write provider is %T, want skillsWriteAdapter", provider)
	}
	if adapter.capabilities == nil {
		t.Fatal("the write provider was built without a capability source: the house verbs cannot work in production")
	}

	boards := buildGovernanceProviders(cfg, pool, nil, nil, newIdentityLoaders(cfg, nil))
	board, ok := boards.Skills.(skillsBoardAdapter)
	if !ok {
		t.Fatalf("skills board is %T, want skillsBoardAdapter", boards.Skills)
	}
	if board.capabilities == nil {
		t.Fatal("the skills board was built without a capability source: it can never mark a house row actionable")
	}
}

// stagedHas reports whether the stage listing carries name.
func stagedHas(staged []skills.StageSkill, name string) bool {
	for _, sk := range staged {
		if sk.Name == name {
			return true
		}
	}
	return false
}

// TestSkillsInstallEmptySourceRejected proves an empty source is a client error before any
// transport runs (the install front-door guard).
func TestSkillsInstallEmptySourceRejected(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	adapter, _, _, _ := buildSkillsInstall(t, cfg, "unused", "body")
	if _, err := adapter.Install(t.Context(), "local", ""); err == nil {
		t.Fatal("empty source must be rejected")
	}
}
