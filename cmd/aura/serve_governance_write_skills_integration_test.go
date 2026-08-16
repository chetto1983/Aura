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
	"github.com/chetto1983/aura/internal/skills"
	"github.com/google/uuid"
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
// clean, isolated set of dirs.
func skillsBridgeConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.LoadDB()
	cfg.SkillsDir = filepath.Join(root, "active")
	cfg.SkillExportDir = filepath.Join(root, "export")
	cfg.SkillBodyCapBytes = 32768
	cfg.SkillInjectionBlocklist = []string{"<|im_start|>"}
	for _, d := range []string{cfg.SkillsDir, cfg.SkillExportDir, filepath.Join(cfg.SkillsDir, "pending"), filepath.Join(cfg.SkillsDir, "archived")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return cfg
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
func buildSkillsInstall(t *testing.T, cfg *config.Config, skillName, body string) (skillsWriteAdapter, *skills.Writer, *askuser.Store) {
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
	adapter := skillsWriteAdapter{installer: installer, writer: writer, activeDir: cfg.SkillsDir}
	return adapter, writer, pause
}

// TestSkillsInstallActivatesDirectly is the Claude-Code-parity no-skip-as-green backstop:
// Install stages + validates + ACTIVATES the skill in one step (status "active", loadable),
// mints NO approval pause in the cross-thread queue, and leaves nothing in pending/.
func TestSkillsInstallActivatesDirectly(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "br-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, writer, pause := buildSkillsInstall(t, cfg, name, "a clean install body")
	ctx := t.Context()

	info, err := adapter.Install(ctx, "local", "owner/"+name)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if info.Status != "active" {
		t.Fatalf("install status = %q, want active (direct activation, no ceremony)", info.Status)
	}
	if !writer.ActiveExists(name) {
		t.Fatal("skill must be ACTIVE immediately after install (no approval step)")
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

	// The pending staging dir was consumed by Activate (promoted to active).
	if _, serr := os.Stat(filepath.Join(cfg.SkillsDir, "pending", name, "SKILL.md")); serr == nil {
		t.Fatal("activated install must leave nothing in pending/")
	}
}

// TestSkillsInstallEmptySourceRejected proves an empty source is a client error before any
// transport runs (the install front-door guard).
func TestSkillsInstallEmptySourceRejected(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	adapter, _, _ := buildSkillsInstall(t, cfg, "unused", "body")
	if _, err := adapter.Install(t.Context(), "local", ""); err == nil {
		t.Fatal("empty source must be rejected")
	}
}
