# Phase 11: Skills - Research

**Researched:** 2026-06-05
**Domain:** Self-extension system — instruction skills (loader/validator/writer/installer) + executable snippets v1 on the local sandbox; Go 1.26, Postgres (sqlc/pgx/golang-migrate), skills.sh catalog, NFKC/Unicode validation, append-only audit
**Confidence:** HIGH

## Summary

Phase 11 is unusually well-specified upstream: `11-CONTEXT.md` carries 35+ locked decisions (D-01..D-38), a ratified env catalog (D-34), an audit-coherence matrix (D-29), and ten live spikes (003-010) that already settled every transport/install/mount/dep question the PRD left open. This research therefore is **not** an exploration of alternatives — the decisions are locked. Its job is to (a) **ground-truth the existing codebase seams** the plan must reuse verbatim, (b) **verify the two genuinely net-new external dependencies** (a YAML frontmatter parser + the `golang.org/x/text/unicode/norm` NFKC path), (c) **confirm the migration floor and integration points**, and (d) supply the Validation Architecture (Dimension 8) the Nyquist-enabled planner needs.

The strongest finding from reading the code: every seam the CONTEXT names is real and shaped exactly as claimed. `ActionRouter` (action.go) + `TaskTool` (task.go) are a copy-from template for the one `skill` tool (D-01); `toolSearchDescription(reg)` in search.go is the live precedent for the D-06 manifest-in-Description pattern (a `Spec().Description` computed from registry state at build time, turn-stable); `scoring.ComputeSkillTier` is shipped and unwired waiting for its Phase-11 consumer; the cron `Handler`/`HandlerMeta`/`NewDispatch` map is the host for D-16's `skill_ttl_sweep` TaskKind; the migration floor is **0009** (skills land at **0010+**); `golang.org/x/text v0.37.0` is already a vendored indirect dep (NFKC is free, no new module). The sandbox-agent runs `--no-token` today (D-38's wiring obligation is real) and the compose service has **no `/skills` mount yet** (D-17 adds it).

**Primary recommendation:** Plan the Wave-0 doc-only PRD-amendment commit (D-33) first, then build sub-slices in the D-32 order (7a→7b→7c→7d→7e). Adopt `github.com/goccy/go-yaml` v1.19.2 for frontmatter (verified, actively maintained, better errors than yaml.v3) and `golang.org/x/text/unicode/norm` for NFKC (promote indirect→direct). All execution stays on the shipped `sandbox_exec`/`sandboxagent.Client` HTTP seam — no new execution machinery.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Skill discovery (catalog browse) | API/Backend (`internal/skills` catalog client → skills.sh `/api/search`) | — | Read-only HTTP JSON; lives behind the `skill` tool, model-facing |
| Skill loading (FS scan, TTL cache, parse) | API/Backend (`internal/skills` loader) | Filesystem (`~/.aura/skills/`) | Multi-root FS scan + parse-only validation; pure read path |
| Skill validation (NFKC + blocklist + fuzz) | API/Backend (`internal/skills` validator) | — | Pure function over body bytes; enforced ONLY at write boundaries (D-28) |
| Skill mutation (create/update/delete/install) | API/Backend (writer + Postgres audit) | Database (`aura.skill_audit` 0010) | Atomic pending→active + append-only audit trigger + role separation |
| Skill install (clone + hash) | API/Backend (`internal/skills` installer) | Filesystem + git CLI | Native Go shallow clone (D-15), symlink-strip, canonical hash |
| Snippet execution | API/Backend (`sandbox_exec` tool → `sandboxagent.Client`) | Sandbox container (`/skills` ro mount) | By-path interpreter exec on the shipped HTTP seam — never new exec code |
| Prompt injection (manifest + always-block) | Frontend Server / loop composition (`prompt.go` + `conversations/context.go` messages[1]) | — | messages[0] static sentence (frozen); manifest in `skill` tool Description; always-bodies in messages[1] |
| TTL sweep | API/Backend (`internal/cron` TaskKind `skill_ttl_sweep`) | Database (scheduler tables) | Reuse Phase-10 scheduler persistence/HA/catch-up — no goroutine |
| Approval gate | API/Backend (ask_user resume + CLI) | Database (`paused_states`) | No model-facing approve (D-03); human/CLI only |

**Tier-correctness flags for the planner:** Validation is a *write-boundary* concern, never a loader concern (D-28) — do not place blocklist enforcement in the load path. Execution is *always* the sandbox tier via the existing tool — Phase 11 adds NO new execution code, only file materialization + a ro mount + a usage-stamping hook.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/goccy/go-yaml` | v1.19.2 `[VERIFIED: go proxy]` | SKILL.md frontmatter parsing (D-30) | Actively maintained (100+ releases), pure-Go (no CGO), strict-mode + far better error messages than yaml.v3; handles double-quoted scalars with escaped quotes (the real skills.sh corpus shape, spike 006) `[CITED: spike 006 README]` |
| `golang.org/x/text/unicode/norm` | v0.37.0 (already indirect) `[VERIFIED: go.mod]` | NFKC normalization for the validator (D-27/D-28) | Stdlib-adjacent canonical Unicode normalization; `norm.NFKC.String(s)` is the TR15 path; ALREADY a vendored indirect dep — promote to direct, zero new supply-chain surface |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/jackc/pgx/v5` | v5.9.2 (in tree) | Audit store (`aura.skill_audit`) | Copy the identity/askuser canonical store pattern (D-A4-01) |
| `os/exec` (stdlib) | — | `git clone --depth 1 --single-branch -c core.autocrlf=false` (D-15) | LookPath fixed-argv, never shell-string; mirrors the dockerCLI pattern in `internal/sandbox` |
| `crypto/sha256` + `sort` (stdlib) | — | Aura's canonical hash (byte-sorted (relPath,bytes) sha256, D-15) | Install TOFU pin; do NOT interop with upstream `skills-lock.json` computedHash |
| `net/http` (stdlib) | — | skills.sh `/api/search?q=` catalog client (D-11) | Lax JSON decode (NEVER `DisallowUnknownFields`); rank by `installs` |
| `testing` + `go test -fuzz` (stdlib) | Go 1.26 | `FuzzSkillValidator` (SC#3, 10K mutations) | Native fuzzing; no third-party fuzz lib |
| `pgregory.net/rapid` | v1.3.0 (in tree) | Property-based tests where indicated | Already adopted for swarm/budget |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `goccy/go-yaml` v1.19.2 | `gopkg.in/yaml.v3` v3.0.1 | yaml.v3 is the de-facto default but in maintenance-only mode with known frontmatter quirks (no strict line/col errors, duplicate-key handling). goccy is actively developed, gives line/column errors (better operator gate messages), and is what Codex's Go skills tooling uses. CONTEXT calls picobot's hand-roll "the anti-reference" — either real lib satisfies "real YAML lib"; goccy recommended. `[ASSUMED — final pick is planner discretion]` |
| `goccy/go-yaml` | `sigs.k8s.io/yaml` v1.6.0 | sigs.k8s.io/yaml routes YAML→JSON→struct (good for JSON-schema reuse) but pulls a heavier dep tree (k8s apimachinery lineage); overkill for frontmatter. |
| native `git` CLI clone | go-git library | A pure-Go go-git removes the git binary dep but adds a large module + its own CRLF/symlink semantics to re-audit; spike 004b proved the CLI clone bit-identical and 3× faster than npx. Stay with the shipped-binary CLI pattern. |

**Installation:**
```bash
go get github.com/goccy/go-yaml@v1.19.2
# norm is already present transitively; promote it:
go get golang.org/x/text@v0.37.0   # makes the existing indirect dep direct
```

**Version verification (run during planning):**
```bash
go list -m -versions github.com/goccy/go-yaml   # confirms v1.19.2 latest
go list -m golang.org/x/text                     # confirms v0.37.0 already in tree
```

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/goccy/go-yaml` | go proxy | mature (v1.0.0 → v1.19.2, years of releases) | high (broad Go ecosystem) | github.com/goccy/go-yaml | `[OK]` (full module path) | Approved |
| `golang.org/x/text` | go proxy | golang.org official | very high | go.googlesource.com/text | n/a (official x/) | Approved (already vendored) |
| `gopkg.in/yaml.v3` | go proxy | mature | very high | github.com/go-yaml/yaml | `[OK]` | Approved (alternative) |
| `sigs.k8s.io/yaml` | go proxy | mature | high | github.com/kubernetes-sigs/yaml | not run | Approved (alternative) |

**Packages removed due to slopcheck [SLOP] verdict:** none. NOTE: slopcheck initially flagged the bare `goccy/go-yaml` form as `[SLOP]` ("does not exist on go") — this was a **false positive caused by the bare owner/repo path**; re-running with the full module path `github.com/goccy/go-yaml` returned `[OK]` and `go get` resolved v1.19.2. For Go modules ALWAYS pass slopcheck the full module path.
**Packages flagged as suspicious [SUS]:** none.

> All three YAML candidates and x/text were verified present on the Go module proxy via `go list -m -versions`. No `npm`/`PyPI` packages are installed by this phase's Go code (the `npx skills` host dep is **dropped** per D-15 — installer is native Go clone). The xlsx North-Star Python deps (`openpyxl/defusedxml/lxml/validators`) are *resolved inside the sandbox container at run time* (D-36 dep-strategy choice), not added to Aura's Go module.

## Architecture Patterns

### System Architecture Diagram

```
                          ┌─────────────────────────────────────────────┐
   operator / model       │  ONE `skill` tool (non-deferred, D-05)        │
   ──────────────────────▶│  ActionRouter (copy of task.go pattern)       │
   "make me an xlsx"      │  actions: list info use create update delete  │
                          │           install catalog restore archive     │
                          └───────┬───────────────┬───────────────┬───────┘
                                  │               │               │
                  ┌───────────────▼──┐   ┌────────▼────────┐  ┌───▼──────────────┐
                  │ loader (7a)      │   │ validator (7b)  │  │ writer (7c)      │
                  │ multi-root FS    │   │ NFKC + blocklist│  │ pending→active   │
                  │ scan, TTL 1s,    │   │ + 10K fuzz      │  │ atomic + audit   │
                  │ YAML parse-only  │   │ (WRITE boundary │  │ INSERT (0010)    │
                  └──────┬───────────┘   │  ONLY, D-28)    │  └───┬──────────┬───┘
                         │               └─────────────────┘      │          │
       ┌─────────────────┼──────────────────────────┐             │          │
       ▼                 ▼                           ▼             ▼          ▼
  messages[1]      skill Description           catalog (7b)   ask_user    aura.skill_audit
  always-block     manifest (D-06,             skills.sh      resume       append-only
  (D-07, shared    turn-stable, BM25            /api/search   OR `aura      BEFORE U/D/TRUNCATE
  w/ Phase-14)     overflow D-09)               (JSON, lax)   skills        + role sep (aura_app)
                                                              approve` CLI  (D-29 matrix)
                                                    │              (D-03)
                                              installer (7d)
                                              native git clone --depth 1
                                              + symlink-strip + canonical hash
                                                    │
                                              ┌─────▼──────────────────────────┐
                                              │ activation materializes files  │
                                              │ to host export dir (D-17)      │
                                              └─────┬──────────────────────────┘
                                                    │  ro bind mount
                                                    ▼
   snippet exec (7e) ──────────▶  sandbox_exec tool ──▶ sandboxagent.Client (HTTP :2468)
   action=use returns path        (python3 /skills/x.py)  ──▶ aura-sandbox-agent container
                                                              /skills mounted READ-ONLY
   TTL sweep ──────────▶ cron `skill_ttl_sweep` TaskKind (D-16) ──▶ de-materialize + archive
```

### Recommended Project Structure
```
internal/skills/                  # GREENFIELD — zero skill code in repo today
├── loader.go                     # multi-root FS scan + TTL 1s cache + parse-only (D-28, ≤600 LOC)
├── frontmatter.go                # goccy/go-yaml parse + CRLF normalize + tolerate optional fields (D-30)
├── validator.go                  # NFKC + literal blocklist + SanitizeName chokepoint (D-27/D-28)
├── validator_fuzz_test.go        # FuzzSkillValidator (SC#3, 10K mutations)
├── catalog.go                    # skills.sh /api/search client, lax decode (D-11)
├── installer.go                  # native git clone + symlink-strip + canonical hash (D-15)
├── writer.go                     # pending→active atomic + audit INSERT (D-29)
├── audit_store.go                # copy identity/askuser canonical store pattern
├── manifest.go                   # skill Description generation (D-06) + BM25 overflow (D-09)
├── materialize.go                # activation→host export dir; de-materialize on archive/delete (D-17)
└── snippet.go                    # 7e save/use; usage sidecar JSON (D-19)
internal/agent/tools/skill.go     # ONE skill tool (ActionRouter), copy of task.go (D-01)
internal/cron/handlers/skill_ttl.go  # skill_ttl_sweep handler (D-16)
internal/db/migrations/0010_skill_audit.{up,down}.sql   # (+ optional 0011_snippet_runs)
internal/db/queries/skill_audit.sql                     # sqlc
```

### Pattern 1: One tool, ActionRouter dispatch (D-01)
**What:** A single `skill` Tool whose `Execute` parses only the `action` discriminator and dispatches through `NewActionRouter(map[string]ActionFunc{...})`.
**When to use:** This is the locked shape; copy `task.go` verbatim.
**Example:**
```go
// Source: internal/agent/tools/task.go (shipped Phase 10) — copy-from template
func (t *SkillTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var head struct{ Action string `json:"action"` }
	if err := json.Unmarshal(raw, &head); err != nil { return ToolResult{}, fmt.Errorf("skill args: %w", err) }
	if head.Action == "" { return ToolResult{}, fmt.Errorf("skill: action is required") }
	return t.actionRouter().Dispatch(ctx, head.Action, raw)
}
```
**Schema discipline (D-10, OpenAI-wire / DeepSeek):** top-level `"required": ["action"]` ONLY; per-action fields documented in property `description` strings; the `action` property carries a string-level `enum` (wire-safe) but there is NO root `oneOf`/`anyOf`/`enum` (a root enum 400s DeepSeek). This is enforced by `task.go`'s `taskParamsSchema` — mirror it.

### Pattern 2: Manifest-in-Description, turn-stable (D-06)
**What:** The `skill` tool's `Spec().Description` is COMPUTED from registry/loader state at build time (not a constant), exactly like `tool_search`.
**When to use:** D-06 available-skills manifest.
**Example:**
```go
// Source: internal/agent/tools/search.go — toolSearchDescription(reg) precedent
func (t *SkillTool) Spec() Spec {
	return Spec{
		Name:        "skill",
		Summary:     "List, inspect, use, create, install, or manage skills.",
		Description: t.renderManifestDescription(), // name+trigger per skill, cap AURA_SKILL_MANIFEST_CAP_BYTES (~8k), then "N more — search with action=list {query}"
		Parameters:  json.RawMessage(skillParamsSchema),
		Deferred:    false, // D-05 parity
	}
}
```
**Cache contract:** The Description is turn-stable (changes only on a skill add/remove, busting the tools-prefix cache ONCE on the next turn — rare, accepted, D-06). `messages[0]` (prompt.go `SystemPrompt`) gets exactly ONE frozen mechanism sentence and is otherwise untouched (CAP-04 byte-stable invariant). Manifest ordering must be **alphabetical/byte-stable** (manifest.go `Render()` sort precedent — any reshuffle cache-busts).

### Pattern 3: Append-only audit + role separation (D-29, Pitfall #6)
**What:** `aura.skill_audit` is INSERT-only for `aura_app`; a BEFORE UPDATE/DELETE row trigger + a BEFORE TRUNCATE statement trigger raise an exception; DDL is reserved to `aura_migrate`.
**When to use:** Migration 0010.
**Example:**
```sql
-- Source: internal/db/migrations/0009_scheduler.up.sql (role-grant precedent)
-- aura_app gets INSERT/SELECT only on the audit table — NO UPDATE/DELETE/TRUNCATE.
GRANT SELECT, INSERT ON aura.skill_audit TO aura_app;
GRANT ALL           ON aura.skill_audit TO aura_migrate;
-- Pitfall #6 belt-and-suspenders: a TRUNCATE bypasses row triggers, so a SEPARATE
-- BEFORE TRUNCATE STATEMENT trigger is required in addition to BEFORE UPDATE/DELETE.
CREATE TRIGGER skill_audit_no_truncate BEFORE TRUNCATE ON aura.skill_audit
  FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_audit_mutation();
```
The audit-coherence CHECK (D-29 matrix) governs `approval_source`/`paused_state_token`/`gate_recommended`/`gate_taken` consistency — the planner writes the exact CHECK SQL from the D-29 table.

### Pattern 4: TTL sweep as a cron TaskKind (D-16)
**What:** A `skill_ttl_sweep` handler implementing `cron.Handler` (Meta + Run), registered in the composition root's kind→handler map, seeded daily like the backup tasks.
**Example:**
```go
// Source: internal/cron/dispatch.go (Handler interface) + cmd/aura/serve.go:109-110 (backup seed)
type SkillTTLSweepHandler struct{ /* loader, store */ }
func (h SkillTTLSweepHandler) Meta() cron.HandlerMeta {
	return cron.HandlerMeta{Kind: "skill_ttl_sweep", MaxDuration: 2*time.Minute, ReschedulesOnRecovery: false}
}
func (h SkillTTLSweepHandler) Run(ctx context.Context, job cron.Job) (string, error) { /* scan usage sidecars, archive > AURA_SKILL_SNIPPET_TTL_DAYS, de-materialize */ }
```
NOTE: the 0009 `scheduler_tasks.kind` CHECK constraint currently allows only `reminder|agent_job|backup_postgres|backup_neo4j`. **Adding `skill_ttl_sweep` requires an ALTER of that CHECK in 0010** (and the `task.go` tool's `kind` enum stays unchanged — this TaskKind is system-seeded, not model-schedulable). The planner must include the CHECK widening.

### Anti-Patterns to Avoid
- **Full skill body in the system prompt (messages[0]):** picobot's `context.go` eager full-body injection is the documented anti-pattern (cache-bust every turn). Always-bodies go in **messages[1]** (D-07); the manifest goes in the **tool Description**; messages[0] gets one frozen sentence only.
- **Blocklist enforcement in the loader:** disk is operator-trusted (CC parity, D-28). Enforce NFKC+blocklist ONLY at write boundaries (create/update/install). The loader validates parse + structure + name regex + size and skip-logs invalid.
- **A `ttl_sweeper.go` goroutine:** D-16 amends this away — use the scheduler TaskKind (free persistence/HA/catch-up).
- **A model-facing `approve` action:** D-03 — the model cannot approve its own mutations; activation is human (ask_user resume) or CLI only.
- **Interop with upstream `skills-lock.json` computedHash:** spike 004b proved it locale/platform-sensitive. Compute Aura's OWN canonical hash.
- **Relying on the executable bit for snippet exec:** Docker Desktop masks 777 (spike 005). ALWAYS invoke `python3 /skills/x.py` (interpreter + path), never `./x.py`.
- **Root-level JSON-schema `oneOf`/`enum`:** 400s DeepSeek (task.go comment). Per-action requirements in descriptions only.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| YAML frontmatter parse | A regex/hand-roll scanner (picobot's approach) | `goccy/go-yaml` | Double-quoted scalars with escaped quotes + multiline + anchors are in the real corpus (spike 006); hand-roll silently mis-parses |
| Unicode NFKC normalization | Manual codepoint folding | `golang.org/x/text/unicode/norm` | TR15 canonical/compatibility decomposition is a large table; `norm.NFKC.String` is the only correct path; already vendored |
| Single-tool dispatch | N separate `skill_*` tool files (the pre-rewrite 347-LOC `registry/skill.go`) | `ActionRouter` (action.go) | One manifest entry, one wire-safe schema; D-01 explicitly kills the multi-file shape |
| BM25 over the skill corpus | A new ranker | `bm25.go` (`newBM25Index`/`rank`) | Shipped, stdlib-only, sub-ms over N≤~50; D-09 overflow search |
| Risk tiering | A new gate | `scoring.ComputeSkillTier` | Shipped + unit-tested; Phase 11 is its designated consumer (create/update/install→Risky, delete→Destructive) |
| TTL background job | A goroutine + ticker | cron `skill_ttl_sweep` TaskKind | Persistence/HA/missed-catch-up/audit free from Phase 10 |
| Approval channel | New pause machinery | ask_user resume + `paused_states` | Shipped FIFO multi-pause; D-03/D-26 reuse `proxied_*` columns |
| Audit store | New store boilerplate | Copy identity/askuser canonical pattern + `db.WithTx` | D-A4-01 lineage; SQLSTATE classification, sentinel errors, pgtype boundary |
| Sandbox execution | Any new exec path | `sandbox_exec` → `sandboxagent.Client` | The ONLY execution seam (D-04/D-17); HTTP :2468; never re-enter code into context |
| Snippet output cleanup | New retention machinery | Phase-8 ConversationCleaner workspace purge cascade | D-18 ratified — zero new machinery |

**Key insight:** Phase 11 is almost entirely *wiring shipped primitives together* + two pure modules (validator, frontmatter parser). The only genuinely new external deps are the YAML lib and the (already-vendored) norm package. Resist building anything the CONTEXT's "Reusable Assets" list already names.

## Runtime State Inventory

> Phase 11 is greenfield (`internal/skills/` has zero code) but it DOES create runtime state in three persistent stores beyond the repo. Inventory below.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.skill_audit` rows (migration 0010, append-only); optional `aura.snippet_runs` (0011) per-run forensics (D-19); the `scheduler_tasks` seed row for `skill_ttl_sweep` | Migration writes the schema + role grants + triggers; cmd seeds the daily sweep task (mirror backup seeding) |
| Live service config | The `aura-sandbox-agent` compose service gains a **ro `/skills` bind mount** (D-17) — TODAY it has only the `aura-sandbox-agent` named volume at `/workspace`, NO `/skills`. This is the stack's first bind mount (spike 005 proved it works on Docker Desktop) | Edit `compose.yaml` `aura-sandbox-agent.volumes` to add `- ${AURA_SKILL_EXPORT_DIR}:/skills:ro`; also wire `--token` + `Authorization: Bearer` (D-38) — the service runs `--no-token` today |
| OS-registered state | None — no OS scheduler/launchd registration; the TTL sweep is a DB row, not an OS cron | None — verified: cron is Postgres-backed (`scheduler_tasks`), not OS-level |
| Secrets/env vars | New: `AURA_SANDBOX_AGENT_TOKEN` (D-38, gen at first boot, mirror `AURA_SETUP_TOKEN`). D-34 env catalog (8 `AURA_SKILL_*` vars). NO third-party API keys (skills.sh `/api/search` is unauthenticated) | Config additions only; token gen + healthcheck must carry the bearer (spike 008) |
| Build artifacts / installed packages | `~/.aura/skills/` (active/pending/archived skill dirs) materialized to `AURA_SKILL_EXPORT_DIR` on activation; the **xlsx North-Star Python deps live in the SANDBOX IMAGE or per-skill venv** (D-36 choice), NOT in Aura's Go module | No Go-module change for xlsx deps; the dep-strategy decision (bake / on-demand uv / hybrid) is a planner choice that may touch `docker/sandbox-agent/Dockerfile` |

**The canonical question — after every repo file ships, what runtime state still carries old shape?** Nothing stale (greenfield). But three NEW persistent surfaces are created: the audit table (0010), the `/skills` mount + sandbox token (compose), and the seeded sweep task (DB). All three must be in the plan, not just the Go code.

## Common Pitfalls

### Pitfall 1: TRUNCATE bypasses row triggers (Pitfall #6, the named one)
**What goes wrong:** A `BEFORE UPDATE OR DELETE` row trigger does NOT fire on `TRUNCATE` — an attacker (or a buggy migration) could wipe the audit log.
**Why it happens:** Postgres `TRUNCATE` is a statement-level DDL-ish operation, not row DML.
**How to avoid:** TWO triggers — `BEFORE UPDATE OR DELETE FOR EACH ROW` AND a separate `BEFORE TRUNCATE FOR EACH STATEMENT` — plus `aura_app` having no TRUNCATE grant (belt-and-suspenders, D-29). The SC#2 acceptance literally tests `aura skills audit purge` as `aura_app` → permission denied.
**Warning signs:** A test that only checks UPDATE/DELETE rejection but not TRUNCATE.

### Pitfall 2: NFKC collapse must be checked, not just the raw bytes (SC#3)
**What goes wrong:** A blocklisted token can be encoded with fullwidth/compatibility codepoints that look different byte-wise but NFKC-collapse to the blocklist literal — bypassing a naive byte-match.
**Why it happens:** Unicode has many compatibility forms (e.g. fullwidth Latin) that normalize to ASCII under NFKC.
**How to avoid:** Normalize with `norm.NFKC.String(body)` FIRST, then run the literal blocklist on the normalized form. The fuzz target (`FuzzSkillValidator`, 10K mutations) must assert every NFKC-collapse-to-blocklist input is rejected (SC#3 is exactly this).
**Warning signs:** Fuzz corpus passes but a hand-crafted fullwidth variant slips through.

### Pitfall 3: messages[1] silently evicted in long conversations (D-07 flagged constraint)
**What goes wrong:** The L2.5 evictor in `conversations/context.go` drops oldest user/assistant pairs; if it isn't taught to protect the messages[1] always-skills block, a long conversation silently loses haiku/always-on mode.
**Why it happens:** The always-block is a user-role message; the evictor treats user messages as droppable.
**How to avoid:** The L2.5 evictor (`reduceForBudget`, preserves seq=1 system L0) must ALSO protect the messages[1] block like L0. This is the D-07 flagged constraint landing in `context.go`.
**Warning signs:** A 20+ turn conversation where an `always:true` skill stops steering behavior.

### Pitfall 4: Symlinks resolve inside the container (spike 005)
**What goes wrong:** A malicious/third-party skill ships a symlink; materialized to the host export dir, it resolves IN-CONTAINER to escape `/skills`.
**Why it happens:** Bind-mounted symlinks are followed by the container's filesystem.
**How to avoid:** Writer/installer MUST strip symlinks at materialization — reuse the Phase-4 `ScanOrphans` Lstat-no-follow pattern (do not follow, unlink/skip the symlink).
**Warning signs:** A skill dir with a symlink that points outside its tree survives materialization.

### Pitfall 5: skills.sh `/api/search` schema drift (spike 003)
**What goes wrong:** A strict decoder (`DisallowUnknownFields`) breaks when skills.sh adds a field (`isDuplicate`, `count` already observed).
**Why it happens:** It is the CLI's internal endpoint, no public API contract (vercel-labs/skills#426).
**How to avoid:** Lax decode (default `json.Decode`); guard empty queries (server returns 400); rank by `installs`; isolate the transport so the future public API is a drop-in swap.
**Warning signs:** Catalog list works today, breaks after a skills.sh deploy.

### Pitfall 6: golang-migrate forbids CONCURRENTLY in a tx (0007/0009 precedent)
**What goes wrong:** A `CREATE INDEX CONCURRENTLY` inside a migration fails because golang-migrate wraps each migration in an implicit transaction.
**How to avoid:** Plain `CREATE INDEX` on the fresh empty 0010 table (it's empty, no lock concern), OR isolate a CONCURRENTLY statement as the sole statement in its own migration (0006 precedent). Fold `pg_trgm`-style EXTENSION needs carefully (0005 precedent).

### Pitfall 7: sandbox-agent runs `--no-token` today (D-38)
**What goes wrong:** Model-authored snippet code executes against an unauthenticated `:2468` — anyone on loopback can exec.
**How to avoid:** Wire `--token` (compose command), send `Authorization: Bearer ${AURA_SANDBOX_AGENT_TOKEN}` from `sandboxagent.Client` (spike 008, ~5 LOC), and the healthcheck must carry the bearer too (`/v1/health` becomes 401 without it). gen the token at first boot.
**Warning signs:** Healthcheck flips to unhealthy after adding `--token` (forgot the bearer on the healthcheck curl).

## Code Examples

### NFKC + blocklist validator (write boundary)
```go
// Source: golang.org/x/text/unicode/norm (CITED: pkg.go.dev/golang.org/x/text/unicode/norm)
import "golang.org/x/text/unicode/norm"

func violatesBlocklist(body string, blocklist []string) (string, bool) {
	folded := norm.NFKC.String(body) // TR15 compatibility composition FIRST
	lower := strings.ToLower(folded)
	for _, bad := range blocklist {
		if strings.Contains(lower, strings.ToLower(bad)) {
			return bad, true
		}
	}
	return "", false
}
```

### Native shallow clone (installer, D-15)
```go
// Source: spike 004b README + internal/sandbox dockerCLI LookPath pattern
gitBin, err := exec.LookPath("git")           // never a shell string
cmd := exec.CommandContext(ctx, gitBin,
	"clone", "--depth", "1", "--single-branch",
	"-c", "core.autocrlf=false",               // spike 004b: locale/CRLF determinism
	repoURL, scratchDir)
// then: copy skill subdir, strip symlinks (Lstat-no-follow), compute canonical hash:
//   sort entries by relPath; for each: write relPath bytes + content bytes into sha256
```

### Catalog client, lax decode (D-11)
```go
// Source: spike 003 README — GET https://www.skills.sh/api/search?q=
type catalogItem struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
	// NO DisallowUnknownFields — isDuplicate/count drift in and out
}
// rank by Installs desc; empty q → server 400, guard before the call
```

## State of the Art

| Old Approach (PRD as written) | Current Approach (CONTEXT/spikes) | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `sandbox.Runner.Execute(lang,code,...,SessionID)` | `tools.SandboxExec` → `sandboxagent.Client` HTTP :2468 | 2026-06-03 pivot (amendment #44) | Snippets exec by-path, not by injected code |
| Migrations `0007_skill_audit`/`0012_snippet_runs` | floor is `0009`; skills at `0010+` | Phase 8/10 shipped 0007-0009 | Renumber in Wave-0 amendment |
| Dotted tool names `skill.list`/`skill.create` | ONE `skill` tool, `action` enum | D-01 (OpenAI-wire `^[a-zA-Z0-9_-]+$`) | DeepSeek rejects dotted names |
| "Skills enter the system prompt" | messages[1] always-block + manifest-in-Description | D-06/D-07 (CAP-04 byte-stable) | messages[0] stays frozen |
| HTML scrape catalog (`catalogItemRE`) | skills.sh `/api/search` JSON, lax | D-11 (spike 003) | No regex scrape; amendment #14 flipped to browse-default-ON |
| `npx skills add --ignore-scripts` | native Go `git clone` | D-15 (spike 004b) | Drops node/npx host dep; `--ignore-scripts` was a no-op anyway (spike 004) |
| `ttl_sweeper.go` goroutine | cron `skill_ttl_sweep` TaskKind | D-16 | Free HA/persistence |
| `skill_audit` ALTER (last_used_at/use_count) | sidecar JSON per skill + `snippet_runs` forensics | D-19 | Audit columns dropped |

**Deprecated/outdated:**
- ROADMAP SC#5 wording (`catalog list` shows "disabled") — re-specced by D-12 (browse is default-ON; `aura skills disable-catalog` is the escape hatch). The Wave-0 amendment updates SC#5.
- ROADMAP/PRD SC#1 `npx skills add --ignore-scripts` — amended to native clone (D-14/D-15).
- gVisor `runsc` overlay + seccomp re-tightening — this is a **Phase-8 sandbox-wide regression** (D-38 scope note), tracked against Phase 8, NOT owned by Phase 11. Phase 11 depends only on the portable hardening floor (token + seccomp + no-new-privileges + read-only rootfs + egress allowlist). Do not let it bloat the Phase-11 plan.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `goccy/go-yaml` is the recommended frontmatter parser over yaml.v3 | Standard Stack | LOW — both satisfy "real YAML lib"; final pick is planner discretion; yaml.v3 already verified as fallback |
| A2 | Adding `skill_ttl_sweep` requires ALTERing the 0009 `scheduler_tasks.kind` CHECK constraint | Pattern 4 | MEDIUM — if the planner forgets the CHECK widening, seeding the sweep task fails the constraint at insert |
| A3 | The xlsx North-Star Python deps resolve inside the sandbox (image bake OR on-demand uv), not Aura's Go module | Package Audit / Runtime Inventory | LOW — confirmed by spikes 006/007 + CONTEXT D-36; the specific dep-strategy model is an explicit planner choice |
| A4 | One migration (0010) suffices; 0011 for snippet_runs is optional | Project Structure | LOW — CONTEXT D-32 calls the split planner discretion |

**Note:** All four are flagged as planner-discretion in CONTEXT — none are hidden assumptions. The CONTEXT itself is overwhelmingly `[VERIFIED via spike]` or `[locked decision]`; this research adds the codebase ground-truth and library verification on top.

## Open Questions (RESOLVED)

1. **goccy/go-yaml has "no source repository linked" in slopcheck.**
   - What we know: It exists on the Go proxy (v1.19.2 resolved via `go get`), is widely used, repo is github.com/goccy/go-yaml.
   - What's unclear: slopcheck's `[OK]` came with a "harder to verify" note (its Go resolver doesn't link the repo).
   - Recommendation: Acceptable — the repo is real and well-known; pin the exact version in go.mod. If the project prefers maximum conservatism, `gopkg.in/yaml.v3` (also `[OK]`) is the fallback.

2. **Dep-strategy model for the xlsx North-Star (D-36 trichotomy).**
   - What we know: bake / on-demand uv / hybrid all viable; on-demand makes `deps:` frontmatter LOAD-BEARING (supersedes D-20 docs-only for that field).
   - What's unclear: which model the planner ships.
   - Recommendation: hybrid (bake the common heavy set, uv the long tail) per CONTEXT's "recommended lean"; ensure `openpyxl/defusedxml/lxml/validators` resolve whichever model ships.

3. **Egress posture for `needs_network` snippets + on-demand deps (D-37).**
   - What we know: enforceable only on native-Linux non-masquerading bridge; advisory on Docker Desktop (vpnkit NAT).
   - Recommendation: route `needs_network:true` and on-demand-dep installs through the allowlisted host forward-proxy at the RISKY tier; flag the Phase-8 proxy-restoration dependency. The egress boundary decision is the planner's, shared with Phase 8.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | All Go code | ✓ | go 1.26.4 (go.mod) | — |
| `git` CLI | installer native clone (D-15) | ✓ (assumed on dev/CI) | — | go-git library (heavier, re-audit CRLF/symlinks) |
| Docker Desktop + sandbox-agent | snippet exec (7e), `/skills` mount | ✓ (`make sandbox-up`) | sandbox-agent :2468 | — (snippet exec returns sandbox_unavailable if down) |
| skills.sh `/api/search` | catalog (7b) | ✓ (probed live 2026-06-05, spike 003) | CLI backend v1.5.10 | npx `skills find` (fallback only, D-11) |
| Postgres (aura_app/aura_migrate roles) | audit 0010 | ✓ (shipped INFRA-01) | PG 17 | — |
| `golang.org/x/text` | NFKC | ✓ (vendored indirect) | v0.37.0 | — |
| `goccy/go-yaml` | frontmatter | ✓ (go proxy, verified) | v1.19.2 | gopkg.in/yaml.v3 v3.0.1 |
| `uv` (in sandbox image) | on-demand deps (D-36 model 2/3) | ✓ if baked (spike 007) | static binary | build-time bake (D-36 model 1) |
| gVisor `runsc` | prod isolation tier (D-38) | ✗ on Docker Desktop | — | portable floor (token+seccomp+no-new-priv+ro-rootfs+egress) — Phase 11 depends on floor ONLY; gVisor is Phase-8/CI/prod |

**Missing dependencies with no fallback:** none blocking Phase 11 (gVisor is explicitly Phase-8-scoped; Phase 11 needs only the portable floor).
**Missing dependencies with fallback:** gVisor (portable floor); skills.sh API (npx find).

## Validation Architecture

> Nyquist validation is ENABLED (no `workflow.nyquist_validation: false` in config). This section is REQUIRED.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go test -fuzz` + `pgregory.net/rapid` (property) + `go.uber.org/goleak` (leak) |
| Config file | none — Go convention (`*_test.go`); build tags `db_integration`, `sandbox_integration`, `cot_eval` |
| Quick run command | `go test ./internal/skills/ ./internal/agent/tools/` (unit, < 30s) |
| Full suite command | WSL: `make quality-full` (vet+build+lint+race+coverage+integration+mutation); per-tier `go test -tags 'db_integration sandbox_integration' -race ./...` with composed DSNs + `make sandbox-up` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CAP-07 | `aura skills list` shows name+summary+tier | unit + integration | `go test -run TestSkillsList ./internal/skills/` | ❌ Wave 0 |
| CAP-07 | `aura skills install <repo>` → native clone + `skill_audit` INSERT (SC#1) | integration | `go test -tags db_integration -run TestInstallAuditRow ./internal/skills/` | ❌ Wave 0 |
| CAP-07 | `aura skills audit purge` as aura_app → permission denied; TRUNCATE trigger (SC#2, Pitfall #6) | integration | `go test -tags db_integration -run TestAuditImmutable ./internal/skills/` | ❌ Wave 0 |
| CAP-07 | Validator rejects every NFKC-collapse-to-blocklist input (SC#3) | fuzz | `go test -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/` | ❌ Wave 0 |
| CAP-07 | `skill` tool schema validates (no root oneOf/enum; required=[action] only) | unit | `go test -run TestSkillToolSchema ./internal/agent/tools/` | ❌ Wave 0 |
| CAP-07 | Manifest-in-Description turn-stable; messages[0] byte-identical (CAP-04) | unit + CI | `scripts/cache_invariant_audit.sh` (existing cross-slice job) | ✓ (extend) |
| CAP-08 | Snippet save → exec by-path in sandbox 2b; output captured (SC#4) | integration | `go test -tags 'sandbox_integration db_integration' -run TestSnippetExec ./internal/skills/` | ❌ Wave 0 |
| CAP-08 | TTL archive after `AURA_SKILL_SNIPPET_TTL_DAYS` via cron TaskKind | integration | `go test -tags db_integration -run TestSkillTTLSweep ./internal/cron/...` | ❌ Wave 0 |
| CAP-07 | catalog list default-ON; `disable-catalog` escape hatch (SC#5, D-12 re-spec) | unit | `go test -run TestCatalogDefaultOn ./internal/skills/` | ❌ Wave 0 |
| CAP-07/08 | xlsx North-Star E2E (catalog→ask_user→install→sandbox_exec→.xlsx) | live cot_eval (operator, NOT CI) | `go test -tags cot_eval -run TestSkillsE2E ./internal/eval/` | ❌ Wave 0 |
| CAP-07 | Symlink stripped at materialization (Pitfall 4) | unit + integration | `go test -run TestMaterializeStripsSymlink ./internal/skills/` | ❌ Wave 0 |
| CAP-07 | No goroutine leak (loader TTL cache, sweep) | unit | `goleak.VerifyTestMain` in package TestMain | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/skills/ ./internal/agent/tools/`
- **Per wave merge:** full tagged tiers in WSL — `go test -tags 'db_integration sandbox_integration' -race ./...` with stack up + composed DSNs
- **Phase gate:** `make quality-full` green (coverage floor **≥85%** across full tag matrix per CLAUDE.md, overrides PRD 75/60) + mutation spot-check ≥70% on validator.go + writer.go + the live cot_eval xlsx E2E (operator-run, ground-truth artifact assertions per D-35)

### Wave 0 Gaps
- [ ] `internal/skills/validator_fuzz_test.go` — FuzzSkillValidator (SC#3), 10K NFKC/Unicode mutations of blocklist patterns
- [ ] `internal/skills/loader_test.go`, `validator_test.go`, `frontmatter_test.go`, `installer_test.go`, `writer_test.go`, `catalog_test.go`, `materialize_test.go` — unit tiers
- [ ] `internal/skills/audit_integration_test.go` (build tag `db_integration`) — SC#1 INSERT + SC#2 immutability/TRUNCATE/role-denied
- [ ] `internal/skills/snippet_integration_test.go` (build tags `sandbox_integration db_integration`) — SC#4 by-path exec + output capture
- [ ] `internal/cron/handlers/skill_ttl_test.go` — sweep handler + seeded-task integration
- [ ] `internal/agent/tools/skill_test.go` — schema discipline + ActionRouter dispatch + `reg.Validate()` holds
- [ ] `internal/skills/main_test.go` — `goleak.VerifyTestMain`
- [ ] `internal/eval/...` cot_eval scenario — xlsx North-Star E2E (extend existing `cot_eval` harness, D-35)
- [ ] Extend `scripts/cache_invariant_audit.sh` to cover the messages[1] always-block + manifest-in-Description turn-stability
- [ ] Migration round-trip test: `TestMigration0010_SchemaRoundTrip` (triggers + role grants + CHECK)
- [ ] Framework install: `go get github.com/goccy/go-yaml@v1.19.2` + promote `golang.org/x/text` to direct

## Security Domain

> `security_enforcement` not disabled in config — section included.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Self-extension scoped to skills only; substrate source is NOT model-editable (REQUIREMENTS out-of-scope row); execution contained in sandbox |
| V2 Authentication | yes | sandbox-agent bearer token `AURA_SANDBOX_AGENT_TOKEN` (D-38, spike 008) — wire it; today `--no-token` |
| V4 Access Control | yes | Postgres role separation `aura_app` (INSERT/SELECT audit only) vs `aura_migrate` (DDL); model cannot self-approve (D-03) |
| V5 Input Validation | yes | NFKC + literal blocklist + SanitizeName chokepoint at WRITE boundaries; `goccy/go-yaml` strict parse; fuzz (SC#3) |
| V6 Cryptography | yes | `crypto/sha256` canonical install hash (TOFU pin) — stdlib, never hand-roll |
| V10 Malicious Code | yes | Third-party skill install gate surfaces red flags (`metadata.*.install[]`, tool wildcards, bundled-exec count); `always` stripped on install (D-10/D-13); symlink-strip (Pitfall 4); bundled scripts run ONLY via sandbox_exec |
| V12 Files & Resources | yes | ro `/skills` mount; materialize only active skills; de-materialize on archive/delete; Lstat-no-follow symlink strip |
| V13 API/Web Service | yes | skills.sh catalog client is read-only JSON, lax decode, transport-isolated for future public-API swap |

### Known Threat Patterns for Skills + Sandbox

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Prompt injection via skill body (NFKC-obfuscated blocklist) | Tampering | NFKC-normalize then literal-blocklist at write boundary; FuzzSkillValidator (SC#3) |
| Audit log tampering / wipe | Repudiation | Append-only BEFORE UPDATE/DELETE row trigger + BEFORE TRUNCATE statement trigger + aura_app no-DELETE/TRUNCATE grant (Pitfall #6) |
| Self-approval of malicious mutation | Elevation of Privilege | No model-facing approve (D-03); activation only via human ask_user resume or operator CLI |
| Symlink escape from `/skills` | Tampering / Info Disclosure | Strip symlinks at materialization (Lstat-no-follow); ro mount |
| Supply-chain (slopsquat / malicious third-party skill) | Tampering | Native clone (no npm postinstall surface, D-14); canonical hash TOFU pin; install gate surfaces red flags; `always` stripped |
| Unauthenticated sandbox exec on loopback | Elevation of Privilege | Bearer token (D-38, spike 008) — wiring obligation |
| Arbitrary egress from model code | Info Disclosure | Egress allowlist via host forward-proxy at RISKY tier (D-37); `needs_network:false` → no route (prod) |
| Headless self-extension (cron/swarm) auto-activation | Elevation of Privilege | Headless mutation lands in pending + IMMEDIATE Notifier alert + audit `gate_taken=false`; can never self-activate (D-26) |

## Project Constraints (from CLAUDE.md)

- **PRD-first (absolute):** Wave-0 doc-only PRD-amendment commit (D-33) BEFORE any code — the PRD §Slice 7 is stale in 4 confirmed places; amend first.
- **NEVER SUPPOSE / READ BEFORE EDIT:** This research read every named seam (action.go, task.go, sandboxagent/client.go, sandbox_exec.go, scoring.go, spec.go, manifest.go, bm25.go, search.go, prompt.go, dispatch.go, 0009 migration) — the plan must too.
- **NO GOD CLASS (≤600 LOC) + refactor-on-touch:** `internal/skills/` split into ≤600-LOC concern files (`<name>_<concern>.go`); the pre-rewrite 347-LOC `registry/skill.go` is the anti-pattern D-01 kills.
- **Post-edit validation (Gate 2):** `go vet ./... && go build ./... && go test -race ./internal/<pkg>/` after every Go file edit.
- **Coverage floor 85%** across the FULL tag matrix (overrides PRD 75/60); report combined unit+integration+smoke.
- **No-skip-as-green in CI:** integration tiers `t.Fatal` under `$CI` when env unset; CI exports composed DSNs (`AURA_DB_URL`/`AURA_DB_MIGRATE_URL`) + `make sandbox-up` + `AURA_SANDBOX_AGENT_TOKEN`.
- **Deferred-tool pattern:** `skill` is NON-deferred (D-05) but its Description must stay turn-stable; manifest alphabetical (cache-load-bearing).
- **Env convention `AURA_<DOMAIN>_<UNIT>`:** the 8 D-34 `AURA_SKILL_*` vars + `AURA_SANDBOX_AGENT_TOKEN` follow it; skills.sh URL is `AURA_SKILL_CATALOG_URL`.
- **One slice = one commit** (D-32 sub-slice lettering): Wave-0 amendment → 7a → 7b → 7c → 7d → 7e, each atomic + smoke green.
- **All prompts English-only:** the messages[0] mechanism sentence + messages[1] block header + authority frame literal in English; user output language via directive.

## Sources

### Primary (HIGH confidence)
- Codebase ground-truth (read this session): `internal/agent/tools/{action,task,spec,manifest,bm25,search,sandbox_exec}.go`, `internal/sandboxagent/client.go`, `internal/scoring/scoring.go`, `internal/agent/prompt.go`, `internal/cron/dispatch.go`, `internal/db/migrations/0009_scheduler.up.sql`, `compose.yaml` (aura-sandbox-agent service), `go.mod`
- `.planning/phases/11-skills/11-CONTEXT.md` — 35+ locked decisions D-01..D-38, env catalog D-34, audit matrix D-29
- `.planning/spikes/MANIFEST.md` + spikes 003-010 READMEs — live-validated transport/install/mount/dep/hardening verdicts
- `.planning/REQUIREMENTS.md` CAP-07 + CAP-08; `.planning/STATE.md` (migration floor, prior-phase decisions)
- `go list -m -versions` for `goccy/go-yaml` (v1.19.2), `gopkg.in/yaml.v3` (v3.0.1), `sigs.k8s.io/yaml` (v1.6.0), `golang.org/x/text` (v0.37.0 in tree) — go proxy
- slopcheck v0.6.1: `github.com/goccy/go-yaml` → `[OK]`, `gopkg.in/yaml.v3` → `[OK]`

### Secondary (MEDIUM confidence)
- `pkg.go.dev/golang.org/x/text/unicode/norm` — NFKC API (`norm.NFKC.String`)
- agentskills.io specification + skills.sh `/api/search` (probed live in spike 003, 2026-06-05)
- Project skills: `.claude/skills/{golang-testing,golang-error-handling,golang-database,golang-security,golang-lint,golang-concurrency,golang-context,golang-structs-interfaces,neo4j-*}/SKILL.md`

### Tertiary (LOW confidence)
- None requiring validation — the CONTEXT's claims are spike-verified; this research's only new recommendation (goccy/go-yaml) is registry-verified and has a verified fallback.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — both new deps verified on the Go proxy + slopcheck; norm already vendored
- Architecture: HIGH — every seam read in source; the patterns are copy-from-shipped-code
- Pitfalls: HIGH — drawn from spike evidence + the named CONTEXT pitfalls + read migration/trigger precedents
- Validation: HIGH — maps directly to the 5 ROADMAP success criteria + CLAUDE.md gates

**Research date:** 2026-06-05
**Valid until:** 2026-07-05 (stable; re-verify goccy/go-yaml latest + skills.sh `/api/search` shape if planning slips a month — the API is an unversioned internal endpoint)
```