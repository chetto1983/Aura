//go:build db_integration

// Integration test for the Phase-29 D-13 install→approval bridge (the Option-A widening
// of T-04-19). It requires the migrations applied through 0010 (see
// internal/skills/audit_store_integration_test.go header for the env + invocation:
// POSTGRES_PASSWORD + AURA_DB_URL + AURA_DB_MIGRATE_URL on the live stack). No-skip-as-green:
// the env composition fails loudly under $CI when unset.
//
// This is the no-skip-as-green backstop for the cockpit install pause: a real
// skillsWriteAdapter.Install (with a fake npx runner, so no network) mints an
// operator-origin ask_user pause via askuser.Store.Insert; the pause surfaces in
// askuser.Store.ListPendingAll carrying the skill_approval ResumeContext; a real
// Runner.SubmitAnswers(accept) routes through the EXISTING newSkillResumeHook →
// ResumeHandler.Resume → Writer.Activate so the staged skill becomes ACTIVE; a decline
// instead discards the pending (never active). The mint→queue→approve→Activate round-trip
// runs end to end against the live PG.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
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

// skillsBridgeConfig points a config at temp skill dirs so the adapter's Writer + the resume
// hook's Writer (both built via newSkillWriter from this cfg) operate on ONE set of dirs.
func skillsBridgeConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.LoadDB()
	cfg.SkillsDir = filepath.Join(root, "active")
	cfg.SkillExportDir = filepath.Join(root, "export")
	cfg.SkillBodyCapBytes = 32768
	cfg.SkillInjectionBlocklist = []string{"<|im_start|>"}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "test-model"
	}
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
		md := "---\nname: " + name + "\ndescription: bridge live fixture\ntype: instruction\n---\n" + body
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o600); err != nil {
			return "", err
		}
		return "Installed 1 skill\n", nil
	}
}

// buildSkillsBridge builds the live pool, stores, a Writer over cfg's dirs, the adapter with a
// fake-npx Installer, and a real Runner with the skill resume hook wired — the full Option-A
// chain under test.
func buildSkillsBridge(t *testing.T, cfg *config.Config, skillName, body string) (skillsWriteAdapter, *skills.Writer, *runner.Runner, *askuser.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := skillsBridgeEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := skillsBridgeEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
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
	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir()})
	idStore := identity.New(pool)

	writer := newSkillWriter(cfg, pool)
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       writer,
		Run:          fakeNpxAdd(skillName, body),
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
	adapter := skillsWriteAdapter{
		installer:       installer,
		writer:          writer,
		pause:           pause,
		conv:            conv,
		localIdentityID: resolveLocalIdentityID(ctx, idStore),
		model:           cfg.LLM.Model,
	}

	// The resume path (SubmitAnswers → injectAnswer → resumeHook) never drives an LLM turn,
	// so a nil Client is sufficient — runner.New's classifier/embedder wiring is nil-tolerant.
	run := runner.New(runner.Deps{
		Conv:       conv,
		Pause:      pause,
		Identity:   idStore,
		LLM:        llm.Config{Model: cfg.LLM.Model},
		ResumeHook: newSkillResumeHook(cfg, pool),
	})
	return adapter, writer, run, pause
}

// TestSkillsInstallApprovalBridgeAccept is the D-13 Option-A no-skip-as-green backstop for the
// ACCEPT path: Install mints the operator-origin pause; it surfaces in ListPendingAll with the
// skill_approval ResumeContext; the staged skill is pending (NOT active); SubmitAnswers(accept)
// activates it via the existing resume bridge.
func TestSkillsInstallApprovalBridgeAccept(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "br-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, writer, run, pause := buildSkillsBridge(t, cfg, name, "a clean bridged install body")
	ctx := t.Context()

	info, err := adapter.Install(ctx, adapter.localIdentityID, "owner/"+name)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if info.Status != "pending_approval" {
		t.Fatalf("install status = %q, want pending_approval (never self-activated)", info.Status)
	}
	if info.ApprovalToken == "" {
		t.Fatal("install must carry the operator-origin approval token")
	}
	// The staged skill is pending — NOT yet active.
	if writer.ActiveExists(name) {
		t.Fatal("staged skill must NOT be active before the operator approves")
	}

	// The operator-origin pause surfaces in the cross-thread queue with the skill_approval
	// ResumeContext (the SAME projection /api/approvals reads).
	pendings, err := pause.ListPendingAll(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingAll: %v", err)
	}
	var found *askuser.Pending
	for i := range pendings {
		if pendings[i].Token == info.ApprovalToken {
			found = &pendings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("install pause %q absent from /api/approvals queue", info.ApprovalToken)
	}
	if found.Kind != tools.KindApproval {
		t.Errorf("pause kind = %q, want %q", found.Kind, tools.KindApproval)
	}
	var rc struct {
		Type      string `json:"type"`
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal(found.ResumeContext, &rc); err != nil {
		t.Fatalf("decode resume context: %v", err)
	}
	if rc.Type != "skill_approval" || rc.SkillName != name {
		t.Fatalf("resume context = %+v, want skill_approval/%s", rc, name)
	}

	// The operator approves via the SAME source-agnostic Runner path the /api/approvals resolve
	// uses → the resume hook activates the staged skill.
	if _, err := run.SubmitAnswers(ctx, map[string]runner.ResponseInput{
		info.ApprovalToken: {Action: askuser.ActionAccept, Content: "approve"},
	}); err != nil {
		t.Fatalf("SubmitAnswers(accept): %v", err)
	}
	if !writer.ActiveExists(name) {
		t.Fatal("skill must be ACTIVE after the operator approves (the resume bridge activated it)")
	}
}

// TestSkillsInstallApprovalBridgeDecline is the DECLINE backstop: SubmitAnswers(decline)
// discards the pending — the skill is never activated.
func TestSkillsInstallApprovalBridgeDecline(t *testing.T) {
	cfg := skillsBridgeConfig(t)
	name := "br-" + uuid.Must(uuid.NewV7()).String()[:8]
	adapter, writer, run, _ := buildSkillsBridge(t, cfg, name, "a declined install body")
	ctx := t.Context()

	info, err := adapter.Install(ctx, adapter.localIdentityID, "owner/"+name)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := run.SubmitAnswers(ctx, map[string]runner.ResponseInput{
		info.ApprovalToken: {Action: askuser.ActionDecline, Content: ""},
	}); err != nil {
		t.Fatalf("SubmitAnswers(decline): %v", err)
	}
	if writer.ActiveExists(name) {
		t.Fatal("a declined install must NOT be activated")
	}
	// The pending dir was discarded by the resume bridge (DiscardPending).
	if _, serr := os.Stat(filepath.Join(cfg.SkillsDir, "pending", name, "SKILL.md")); serr == nil {
		t.Fatal("declined install must remove the pending staged skill")
	}
}
