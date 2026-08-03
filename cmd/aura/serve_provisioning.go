package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/idroot"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// serve_provisioning.go promotes the shipped live saga TEST adapters
// (internal/agui/provisioning_saga_resumable_test.go) to production composition-root
// wiring, closing VERIF-3/HI-01: before this file the OnboardingDeps.ObjectStore/
// Filesystem/Journal were left nil so a real admin-create provisioned NO per-identity
// Garage bucket + NO per-identity dirs, and the D-27 de-provisioning Deprovisioner was
// never constructed nor its purge sweep registered/seeded. The four adapters here are the
// narrow ports agui declares (onboarding_provision_resources.go / saga_journal.go /
// deprovision.go); buildProvisioningPorts + buildDeprovisioner + seedIdentityPurgeSweep are
// the constructor seams serve_onboarding.go / serve_dispatch.go / serve.go call.
//
// Backward compatible: buildProvisioningPorts returns NIL adapters when the pool OR the
// Garage admin config is absent (a pre-cutover / interview-only deploy provisions nothing),
// and every agui port nil-skips its leg.

// localSeededIdentityID is the migration-0004 seeded `local` operator identity UUID. The
// object-store resolver maps it (and the "local" name / empty principal) to the SHARED
// bucket (D-11 operator-unchanged); every other identity resolves to its OWN provisioned
// bucket. Mirrors internal/agui/auth.go's localIdentityID (unexported there).
const localSeededIdentityID = "00000000-0000-0000-0000-000000000001"

// identityPurgeSweepMinutes is the cadence of the seeded grace-window purge sweep. The
// per-sweep work is a small indexed scan + a handful of idempotent deletes, so 15 minutes
// is a tight-enough grace-window resolution without churn (the 5-minute handler budget from
// handlers.NewIdentityPurgeHandler bounds a single sweep).
const identityPurgeSweepMinutes = 15

// memoryEmbedBackfillSweepMinutes is the cadence of the seeded memory embedding backfill.
// It is cron.MinScheduleEveryMinutes, the floor of the `every` grammar, because the window
// it governs is how long a freshly-written fact stays invisible to semantic search: the
// lexical leg answers immediately, the dense one only once the vector exists. The sweep is
// cheap when there is nothing to do — one credential probe and one indexed SELECT per
// identity — so the floor costs little and buys the shortest gap the scheduler allows.
const memoryEmbedBackfillSweepMinutes = cron.MinScheduleEveryMinutes

var (
	_ agui.ObjectStoreProvisioner = (*objectStoreProvisionAdapter)(nil)
	_ agui.FilesystemProvisioner  = filesystemProvisionAdapter{}
	_ agui.SagaJournal            = sagaJournalAdapter{}
	_ agui.IdentityDeactivator    = identityDeactivatorAdapter{}
	_ agui.IdentityDeleter        = auraLegAdapter{}
)

// objectStoreProvisionAdapter, its objectStoreCredentialResolver/objectStoreMinter seams, and
// EnsureForIdentity/ProvisionObjectStore/DeprovisionObjectStore live in
// serve_provisioning_objectstore.go (refactor-on-touch split, Amendment #88 Task 3, to keep
// this file under the 600-LOC cap).

// filesystemProvisionAdapter satisfies agui.FilesystemProvisioner over the REAL per-user
// config bases (~/.aura/mcp, $AURA_SKILLS_DIR, ~/.aura/pyscripts, ~/.aura/agents), each
// per-identity path rooted through the profile traversal guard so a crafted identity cannot
// escape its provisioning dir. ProvisionIdentityDirs = MkdirAll(0o700) + write-if-absent
// empty Agent.md; DeprovisionIdentityDirs = RemoveAll each root.
type filesystemProvisionAdapter struct {
	roots map[string]string // sub ("mcp"|"skills"|"pyscripts"|"agents") -> base dir
}

func newFilesystemProvisionAdapter(cfg *config.Config) filesystemProvisionAdapter {
	return filesystemProvisionAdapter{roots: identityDirRoots(cfg)}
}

// identityDirRoots resolves the four per-identity storage bases from cfg + the mcp/skills
// package conventions (NOT a temp dir): the mcp base honors mcp.ManagedConfigPath (and thus
// AURA_MCP_CONFIG), skills is $AURA_SKILLS_DIR, agents is $AURA_PROFILE_DIR, pyscripts is
// the home-derived ~/.aura/pyscripts (mirroring internal/skills defaultPyScriptsDir).
func identityDirRoots(cfg *config.Config) map[string]string {
	mcpBase := auraSubdir("mcp")
	if p, err := mcp.ManagedConfigPath(); err == nil {
		mcpBase = filepath.Dir(p)
	}
	return map[string]string{
		"mcp":       mcpBase,
		"skills":    cfg.SkillsDir,
		"pyscripts": auraSubdir("pyscripts"),
		"agents":    cfg.ProfileDir,
	}
}

// auraSubdir returns ~/.aura/<name> with a tmp fallback when the home dir is unavailable
// (matches config.auraHomeDir + internal/skills.defaultPyScriptsDir semantics).
func auraSubdir(name string) string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura", name)
	}
	return filepath.Join(os.TempDir(), "aura", name)
}

func (a filesystemProvisionAdapter) subRoots(id string) (map[string]string, error) {
	out := make(map[string]string, len(a.roots))
	for sub, base := range a.roots {
		dir, err := idroot.RootIdentityDir(base, id)
		if err != nil {
			return nil, err
		}
		out[sub] = dir
	}
	return out, nil
}

func (a filesystemProvisionAdapter) ProvisionIdentityDirs(_ context.Context, id string) error {
	roots, err := a.subRoots(id)
	if err != nil {
		return err
	}
	for _, dir := range roots {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("provision identity dir %q: %w", dir, err)
		}
	}
	agentMD := filepath.Join(roots["agents"], "Agent.md")
	if _, err := os.Stat(agentMD); os.IsNotExist(err) {
		if err := os.WriteFile(agentMD, []byte{}, 0o600); err != nil {
			return fmt.Errorf("write empty Agent.md: %w", err)
		}
	}
	return nil
}

func (a filesystemProvisionAdapter) DeprovisionIdentityDirs(_ context.Context, id string) error {
	roots, err := a.subRoots(id)
	if err != nil {
		return err
	}
	for _, dir := range roots {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("deprovision identity dir %q: %w", dir, err)
		}
	}
	return nil
}

// sagaJournalAdapter satisfies agui.SagaJournal over aura.provisioning_saga (migration
// 0028, MUTABLE) — promoted verbatim from liveJournal.
type sagaJournalAdapter struct {
	pool *pgxpool.Pool
}

func (j sagaJournalAdapter) Record(ctx context.Context, sagaID, identityID, kind, step, status string) error {
	_, err := j.pool.Exec(ctx,
		`INSERT INTO aura.provisioning_saga (saga_id, identity_id, kind, step, status, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, now())
		 ON CONFLICT (saga_id, step) DO UPDATE SET status = EXCLUDED.status, updated_at = now()`,
		sagaID, identityID, kind, step, status)
	return err
}

func (j sagaJournalAdapter) CompletedSteps(ctx context.Context, sagaID string) (map[string]bool, error) {
	rows, err := j.pool.Query(ctx,
		`SELECT step FROM aura.provisioning_saga WHERE saga_id = $1::uuid AND status = 'done'`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var step string
		if err := rows.Scan(&step); err != nil {
			return nil, err
		}
		out[step] = true
	}
	return out, rows.Err()
}

// identityDeactivatorAdapter satisfies agui.IdentityDeactivator over the aura pool (raw-pgx,
// the PgAuditStore composition-root precedent). It owns the 0029 soft-delete columns +
// resolves a target's Authula user id via the 0019 identity_auth_links join. GetIdentityBy*
// is deliberately NOT used (those must NOT gain a deactivated_at filter — the deprovision
// saga + admin roster need to see deactivated rows; the deny is at the auth boundary, Task 3).
type identityDeactivatorAdapter struct {
	pool *pgxpool.Pool
}

func (a identityDeactivatorAdapter) MarkDeactivated(ctx context.Context, identityID string, purgeAfter time.Time) error {
	if _, err := a.pool.Exec(ctx,
		`UPDATE aura.identities SET deactivated_at = now(), purge_after = $2 WHERE id = $1::uuid`,
		identityID, purgeAfter); err != nil {
		return fmt.Errorf("mark identity %q deactivated: %w", identityID, err)
	}
	return nil
}

// purgeableQuery selects deactivated identities past their grace window, LEFT JOINing the
// Authula link (an identity may have no link). i.id is cast to text so a string scan is
// portable without registering a uuid codec.
const purgeableQuery = `
	SELECT i.id::text, i.name, l.authula_user_id
	FROM aura.identities i
	LEFT JOIN aura.identity_auth_links l ON l.identity_id = i.id
	WHERE i.deactivated_at IS NOT NULL AND i.purge_after IS NOT NULL AND i.purge_after <= $1`

func (a identityDeactivatorAdapter) ListPurgeable(ctx context.Context, now time.Time) ([]agui.DeprovisionTarget, error) {
	rows, err := a.pool.Query(ctx, purgeableQuery, now)
	if err != nil {
		return nil, fmt.Errorf("list purgeable identities: %w", err)
	}
	defer rows.Close()
	var out []agui.DeprovisionTarget
	for rows.Next() {
		var id, name string
		var authula *string
		if err := rows.Scan(&id, &name, &authula); err != nil {
			return nil, fmt.Errorf("scan purgeable identity: %w", err)
		}
		t := agui.DeprovisionTarget{IdentityID: id, IdentityName: name}
		if authula != nil {
			t.AuthulaUserID = *authula
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// resolveTargetQuery is the same projection keyed on a single identity id.
const resolveTargetQuery = `
	SELECT i.id::text, i.name, l.authula_user_id
	FROM aura.identities i
	LEFT JOIN aura.identity_auth_links l ON l.identity_id = i.id
	WHERE i.id = $1::uuid`

func (a identityDeactivatorAdapter) ResolveTarget(ctx context.Context, identityID string) (agui.DeprovisionTarget, error) {
	var id, name string
	var authula *string
	if err := a.pool.QueryRow(ctx, resolveTargetQuery, identityID).Scan(&id, &name, &authula); err != nil {
		return agui.DeprovisionTarget{}, fmt.Errorf("resolve deprovision target %q: %w", identityID, err)
	}
	t := agui.DeprovisionTarget{IdentityID: id, IdentityName: name}
	if authula != nil {
		t.AuthulaUserID = *authula
	}
	return t, nil
}

// buildProvisioningPorts assembles the three onboarding resource-leg ports. It returns NIL
// adapters when the pool OR the Garage admin config (endpoint+token) is absent, so an
// interview-only / pre-cutover deploy provisions nothing (each agui port nil-skips its leg).
// A malformed AURA_AUTHULA_SECRET (the object-store KEK) also disables provisioning rather
// than booting with a weak key.
func buildProvisioningPorts(chat *chatEnv) (agui.ObjectStoreProvisioner, agui.FilesystemProvisioner, agui.SagaJournal) {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return nil, nil, nil
	}
	cfg := chat.cfg
	endpoint := strings.TrimSpace(cfg.GarageAdminEndpoint)
	token := strings.TrimSpace(cfg.GarageAdminToken)
	if endpoint == "" || token == "" {
		return nil, nil, nil // pre-cutover / interview-only deploy provisions nothing
	}
	client, err := garageadmin.New(endpoint, token)
	if err != nil {
		slog.Warn("aura serve: garage admin client unavailable — provisioning disabled", "err", err)
		return nil, nil, nil
	}
	shared := objectstore.Credentials{Bucket: cfg.ObjectStoreBucket, AccessKey: cfg.ObjectStoreAccessKey, SecretKey: cfg.ObjectStoreSecretKey}
	store, err := objectstore.NewIdentityStore(chat.pool, cfg.AuthulaSecret, shared, localSeededIdentityID)
	if err != nil {
		slog.Warn("aura serve: object-store identity resolver unavailable — provisioning disabled", "err", err)
		return nil, nil, nil
	}
	objProv := newObjectStoreProvisionAdapter(client, store)
	fsProv := newFilesystemProvisionAdapter(cfg)
	jrnl := sagaJournalAdapter{pool: chat.pool}
	return objProv, fsProv, jrnl
}

// buildDeprovisioner assembles the D-27 de-provisioning saga over the chat-reachable ports.
// A nil pool yields an empty Deprovisioner whose PurgeExpired is a safe no-op (nil
// Deactivator). The owned Postgres conversation plane and the ArcadeDB
// memory plane are both mandatory: deprovision refuses identity deletion unless each adapter
// acknowledges a verified purge.
// AuthulaDelete/Sessions/Jobs remain separate lifecycle work because those providers are
// assembled after this seam. The identity FK cascade still drops grants, auth links,
// object-store ownership, and the whole document catalog (aura.documents.identity_id is
// ON DELETE CASCADE, migration 0025) — which is why no document leg is wired here.
//
// One landmine in that cascade: aura.audit_logs.actor_identity_id is ON DELETE RESTRICT
// (0025:183). It is dormant only because nothing outside tests writes that table; the first
// writer added turns every de-provision into a failed identity_row step, AFTER the memory
// database has already been dropped.
func buildDeprovisioner(chat *chatEnv) *agui.Deprovisioner {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return agui.NewDeprovisioner(agui.DeprovisionDeps{})
	}
	objProv, fsProv, jrnl := buildProvisioningPorts(chat)
	convStore := chat.conv
	if convStore == nil {
		convStore = conversations.New(chat.pool, conversations.Config{
			RunDir:       chat.cfg.RunDir,
			TurnCapBytes: chat.cfg.ConversationTurnCapBytes,
		})
	}
	return agui.NewDeprovisioner(agui.DeprovisionDeps{
		Journal:        jrnl,
		Deactivator:    identityDeactivatorAdapter{pool: chat.pool},
		Conversations:  conversationPurgeAdapter{store: convStore},
		Memory:         buildArcadeMemoryPurger(chat.cfg),
		ObjectStore:    objProv,
		Filesystem:     fsProv,
		IdentityDelete: auraLegAdapter{pool: chat.pool},
	})
}

// seedEveryCronSweep idempotently seeds a fixed-interval system-seeded sweep: it scans the
// active tasks for an existing entry of kind and only inserts one if absent. Shared by the
// three `every` sweeps (identity_purge, sandbox_reap, memory_embed_backfill) for the same
// reason seedDailyCronSweep is shared by the daily ones — three byte-identical copies modulo
// four literals is what golangci-lint's dupl exists to catch (CLAUDE.md REUSABLE CODE). A
// seed failure is non-fatal (logged by the caller); the daemon still runs.
func seedEveryCronSweep(ctx context.Context, store *cron.Store, kind cron.TaskKind, everyMinutes int, label string) error {
	tasks, err := store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}
	for _, t := range tasks {
		if t.Kind == kind {
			return nil // already seeded — idempotent
		}
	}
	spec, err := cron.ParseSchedule(string(cron.KindEvery), "", everyMinutes, time.Time{}, "Europe/Rome")
	if err != nil {
		return fmt.Errorf("parse %s schedule: %w", label, err)
	}
	next, err := cron.NextRunAt(spec, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("compute next run: %w", err)
	}
	if _, err := store.CreateTask(ctx, cron.CreateTaskParams{
		Kind:      kind,
		Spec:      spec,
		NextRunAt: next,
	}); err != nil {
		return fmt.Errorf("create %s task: %w", string(kind), err)
	}
	slog.Info("aura serve: seeded "+label, "schedule", fmt.Sprintf("every %dm", everyMinutes))
	return nil
}

// seedIdentityPurgeSweep idempotently seeds the grace-window purge sweep (D-27). The INSERT
// succeeds against the 0033-widened scheduler_tasks.kind CHECK.
func seedIdentityPurgeSweep(ctx context.Context, store *cron.Store) error {
	return seedEveryCronSweep(ctx, store, cron.KindIdentityPurge, identityPurgeSweepMinutes, "identity purge sweep")
}

// seedSandboxReapSweep idempotently seeds the D-08 idle-suspend reaper sweep (plan 37-05). The
// INSERT succeeds against the 0034-widened scheduler_tasks.kind CHECK. The cadence is DERIVED
// from the idle-TTL config knob (idleTTLSec, the D-08 tunable) — not a new hardcoded const —
// so a shorter TTL sweeps proportionally more often.
func seedSandboxReapSweep(ctx context.Context, store *cron.Store, idleTTLSec int) error {
	return seedEveryCronSweep(ctx, store, cron.KindSandboxReap, sandboxReapSweepMinutes(idleTTLSec), "sandbox reap sweep")
}

// seedMemoryEmbedBackfillSweep idempotently seeds the memory embedding backfill. The INSERT
// succeeds against the 0091-widened scheduler_tasks.kind CHECK.
//
// This is the sweep that closes the hole: writes store a fact without its vector whenever the
// embedding sidecar is absent or slow, and until this existed nothing ever came back for them,
// so the dense leg of retrieval answered on a corpus with holes in it.
func seedMemoryEmbedBackfillSweep(ctx context.Context, store *cron.Store) error {
	return seedEveryCronSweep(ctx, store, cron.KindMemoryEmbedBackfill,
		memoryEmbedBackfillSweepMinutes, "memory embed backfill sweep")
}

// sandboxReapSweepMinutes derives the reap-sweep cadence (in minutes) from the idle-TTL seconds
// knob (D-08): roughly one sweep per TTL window, floored at 1 minute so a sub-minute TTL still
// yields a valid every-schedule. A box therefore suspends within about one TTL of going idle
// (auto-resumed transparently on its next tool call), and the cadence tracks the operator's knob
// rather than a second hardcoded const.
func sandboxReapSweepMinutes(idleTTLSec int) int {
	m := idleTTLSec / 60
	if m < 1 {
		return 1
	}
	return m
}

// seedDailyCronSweep idempotently seeds a daily-cron system-seeded sweep task: it scans the
// active tasks for an existing entry of kind and only inserts one if absent. Shared by
// seedSkillTTLSweep (D-16) and seedShareExpirySweep (D-15/OQ3) — golangci-lint's dupl caught the
// two as byte-identical modulo four literals, so this is the CLAUDE.md REUSABLE CODE fold rather
// than two copies. seedIdentityPurgeSweep/seedSandboxReapSweep stay separate: they seed a
// KindEvery cadence, not a fixed daily cron. A seed failure is non-fatal (logged by the caller);
// the daemon still runs; the operator can re-seed by restarting.
func seedDailyCronSweep(ctx context.Context, store *cron.Store, kind cron.TaskKind, cronExpr, label string) error {
	tasks, err := store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}
	for _, t := range tasks {
		if t.Kind == kind {
			return nil // already seeded — idempotent
		}
	}
	spec, err := cron.ParseSchedule(string(cron.KindCron), cronExpr, 0, time.Time{}, "Europe/Rome")
	if err != nil {
		return fmt.Errorf("parse daily schedule: %w", err)
	}
	next, err := cron.NextRunAt(spec, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("compute next run: %w", err)
	}
	if _, err := store.CreateTask(ctx, cron.CreateTaskParams{
		Kind:      kind,
		Spec:      spec,
		NextRunAt: next,
	}); err != nil {
		return fmt.Errorf("create %s task: %w", string(kind), err)
	}
	slog.Info("aura serve: seeded daily "+label, "schedule", cronExpr+" Europe/Rome")
	return nil
}

// seedSkillTTLSweep idempotently seeds the daily snippet TTL-sweep task (D-16) — a daily 03:00
// cron (a quiet hour), TZ Europe/Rome. The INSERT succeeds against the 0010-widened
// scheduler_tasks.kind CHECK (A2 landmine closed). Co-located here with its sibling seed helpers
// (seedIdentityPurgeSweep/seedSandboxReapSweep) so serve.go stays under the 600-LOC cap after the
// 37C web-voice wiring landed (refactor-on-touch).
func seedSkillTTLSweep(ctx context.Context, store *cron.Store) error {
	return seedDailyCronSweep(ctx, store, cron.KindSkillTTLSweep, "0 3 * * *", "skill TTL sweep")
}

// seedShareExpirySweep idempotently seeds the daily share-link expiry GC sweep (D-15/OQ3) — a
// daily 04:00 cron (a quiet hour adjacent to skill_ttl_sweep's 03:00), TZ Europe/Rome. This sweep
// is GARBAGE COLLECTION only — the security gate is the lazy fail-closed liveness predicate
// already in ResolveByToken/ResolveLiveByID (plan 37F-07), which denies an expired-but-unswept
// link even if this sweep never runs — so a daily cadence is generous, not urgent. The INSERT
// succeeds against the 0040-widened scheduler_tasks.kind CHECK.
func seedShareExpirySweep(ctx context.Context, store *cron.Store) error {
	return seedDailyCronSweep(ctx, store, cron.KindShareExpirySweep, "0 4 * * *", "share expiry sweep")
}

// seedRetentionSweep installs the single scheduler owner for the shared engine.
func seedRetentionSweep(ctx context.Context, store *cron.Store) error {
	return seedDailyCronSweep(ctx, store, cron.KindRetentionSweep, "30 2 * * *", "retention sweep")
}
