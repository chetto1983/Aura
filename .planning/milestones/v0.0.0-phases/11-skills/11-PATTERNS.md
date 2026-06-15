# Phase 11: Skills - Pattern Map

**Mapped:** 2026-06-05
**Files analyzed:** 18 new + 3 modified
**Analogs found:** 18 / 18 (every new file has a shipped in-repo analog — Phase 11 is overwhelmingly wiring shipped primitives)

> Greenfield package `internal/skills/` has zero code today, but every concern it
> builds maps to a SHIPPED analog. Excerpts below are extracted live from source
> (verified 2026-06-05), not paraphrased from RESEARCH. File:line citations are
> against the current `tabula-rasa` tree.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agent/tools/skill.go` | tool (ActionRouter) | request-response | `internal/agent/tools/task.go` | exact |
| `internal/skills/manifest.go` (skill Description gen + BM25 overflow) | utility | transform | `internal/agent/tools/search.go` `toolSearchDescription` + `bm25.go` | exact |
| `internal/skills/loader.go` (multi-root FS scan + TTL cache + parse-only) | service | file-I/O / read | `internal/conversations/orphan_scan.go` (FS scan + Lstat) | role-match |
| `internal/skills/frontmatter.go` (goccy/go-yaml parse + CRLF) | utility | transform | (new dep — no in-repo YAML parse; spike 004b/006 reference) | partial |
| `internal/skills/validator.go` (NFKC + blocklist + SanitizeName) | utility | transform | `internal/identity/store.go` `validateGrantInput`/`capNameRe` chokepoint | role-match |
| `internal/skills/validator_fuzz_test.go` (FuzzSkillValidator) | test (fuzz) | transform | (Go stdlib `go test -fuzz`; no in-repo fuzz analog) | partial |
| `internal/skills/catalog.go` (skills.sh `/api/search` client, lax decode) | service (HTTP client) | request-response | `internal/sandboxagent/client.go` (HTTP JSON client) | role-match |
| `internal/skills/installer.go` (native git clone + symlink-strip + hash) | service | file-I/O / batch | `internal/cron/handlers/backup.go` (LookPath fixed-argv exec) + spike 004b | role-match |
| `internal/skills/writer.go` (pending→active atomic + audit INSERT) | service | CRUD | `internal/identity/store.go` + `internal/askuser/store.go` (canonical store) | role-match |
| `internal/skills/audit_store.go` (append-only audit store) | model/store | CRUD (INSERT-only) | `internal/askuser/store.go` (canonical store + db.WithTx) | exact |
| `internal/skills/materialize.go` (activation→export dir; de-materialize) | service | file-I/O | `internal/conversations/orphan_scan.go` (Lstat symlink-strip) | role-match |
| `internal/skills/snippet.go` (7e save/use; usage sidecar JSON) | service | file-I/O / CRUD | `internal/cron/handlers/backup.go` (`backupDir`, atomic FS) | role-match |
| `internal/cron/handlers/skill_ttl.go` (`skill_ttl_sweep` Handler) | handler | event-driven / batch | `internal/cron/handlers/backup.go` (`BackupHandler` Meta+Run + sweep) | exact |
| `internal/db/migrations/0010_skill_audit.{up,down}.sql` | migration | DDL | `internal/db/migrations/0009_scheduler.up.sql` (role grants + CHECK) | exact |
| `internal/db/queries/skill_audit.sql` (sqlc) | query | CRUD | existing `internal/db/queries/*.sql` (sqlc convention) | role-match |
| `internal/skills/main_test.go` (goleak) | test | — | existing `main_test.go` goleak TestMain (cron/handlers) | exact |
| `internal/eval/...` cot_eval xlsx scenario | test (E2E) | request-response | existing `internal/eval` cot_eval harness | role-match |
| `internal/agent/prompt.go` (ONE mechanism sentence — MODIFIED) | config | — | itself (byte-stable `SystemPrompt` const) | exact |
| `internal/conversations/context.go` (messages[1] protect — MODIFIED) | service | transform | itself (`dropOldestPairs` seq=1 preservation) | exact |
| `cmd/aura/main.go` `buildBaseRegistry` (register `skill` — MODIFIED) | config (wiring) | — | itself (existing `reg.Register(...)` lines) | exact |
| `compose.yaml` (`/skills` ro mount + `--token` — MODIFIED) | config | — | `compose.yaml` `aura-sandbox-agent` service | exact |

## Pattern Assignments

### `internal/agent/tools/skill.go` (tool, request-response) — D-01/D-05/D-10

**Analog:** `internal/agent/tools/task.go` (copy-from template, doc comment in `action.go:11-17` names Slice 7 as the explicit reuse target).

**One-tool Execute + ActionRouter dispatch** (`task.go:136-160`):
```go
func (t *TaskTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var head struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return ToolResult{}, fmt.Errorf("task args: %w", err)
	}
	if head.Action == "" {
		return ToolResult{}, fmt.Errorf("task: action is required")
	}
	return t.actionRouter().Dispatch(ctx, head.Action, raw)
}

func (t *TaskTool) actionRouter() *ActionRouter {
	if t.router == nil {
		t.router = NewActionRouter(map[string]ActionFunc{
			"schedule": t.actionSchedule,
			"list":     t.actionList,
			// ...
		})
	}
	return t.router
}
```
Skill map: `list|info|use|create|update|delete|install|catalog|restore|archive` (NO `approve` — D-03; NO `run` — D-04). The `≤600 LOC` discipline (CLAUDE.md NO GOD CLASS) means the handler bodies split across `skill_read.go`/`skill_write.go`/`skill_install.go` concern files — Claude's discretion per CONTEXT.

**OpenAI-wire-safe schema discipline (D-10)** — `task.go:96-117` is the verbatim template; the load-bearing rule:
```go
// taskParamsSchema is the OpenAI-wire-safe JSON schema (D-10). The root object's
// only required field is `action`; per-action requirements are spelled out in the
// field descriptions. There is intentionally NO root-level oneOf/anyOf/enum — a
// root enum 400s OpenAI-compat providers (DeepSeek). The `action` property does
// carry an enum (that is a property-level enum on a string, which is wire-safe).
```
Mirror this exactly: top-level `"required": ["action"]` ONLY; per-action fields documented in their property `description`; `action` property carries a string-level `enum`.

**ActionRouter mechanics** (`action.go:30-58`) — copied verbatim, no skill-specific types added (it is kept generic for this reuse):
```go
func NewActionRouter(handlers map[string]ActionFunc) *ActionRouter {
	cp := make(map[string]ActionFunc, len(handlers))
	for name, fn := range handlers {
		cp[name] = fn
	}
	return &ActionRouter{handlers: cp}
}

func (r *ActionRouter) Dispatch(ctx context.Context, action string, args json.RawMessage) (ToolResult, error) {
	fn, ok := r.handlers[action]
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown action %q: valid actions are %s", action, strings.Join(r.Actions(), ", "))
	}
	return fn(ctx, args)
}
```

**Non-deferred Spec + risk-gated mutation** — `task.go:121-131` (Spec shape) + `task.go:187-220` (the `GateRecommended` → `pending_approval` flow that skill create/update/install copies):
```go
tier := scoring.ComputeTaskTier(scoring.TaskArgs{ ... })
status := "active"
if scoring.GateRecommended(tier) {
	status = "pending_approval"
}
// ... persist with Status: status ...
if status == "pending_approval" {
	fmt.Fprintf(&b, "\nAwaiting approval before it fires — approve with ...")
	if scoring.RequiresImmediateAlert(tier, t.alertThreshold()) {
		b.WriteString("\nThis task meets the immediate-alert threshold.")
	}
}
```
Skill version calls `scoring.ComputeSkillTier(action, body)` instead of `ComputeTaskTier` (see Shared Patterns → Risk tiering).

---

### `internal/skills/manifest.go` (utility, transform) — D-06/D-09

**Analog:** `internal/agent/tools/search.go` `toolSearchDescription(reg)` (the live registry-derived turn-stable Description precedent) + `internal/agent/tools/bm25.go` (D-09 overflow ranker).

**Registry-derived, byte-stable Description** (`search.go:75-125`) — the exact cache contract the skill manifest must honor:
```go
// toolSearchDescription builds the registry-derived tool_search Description (D-09):
// a fixed lead-in plus a SORTED, deduped list ... Because the
// Registry is immutable for an agent run, the output is byte-stable across calls and
// across turns — tool_search is non-deferred, so this Description ships in every
// manifest and any variance would bust the OpenRouter implicit cache (T-08.1-07).
func toolSearchDescription(reg *Registry) string {
	return toolSearchLeadIn + sourceOrientation(reg)
}
```
`sourceOrientation` sorts source + tool names before joining (`search.go:113-124`) — the skill manifest must `sort.Strings` skills ALPHABETICALLY so the block is byte-stable (a reshuffle cache-busts). The skill `Spec().Description` is COMPUTED from loader state at build time, exactly like this — NOT a constant. It busts the tools-prefix cache ONCE on a skill add/remove (D-06, accepted).

**BM25 overflow `list` ranker** (`bm25.go:96-145`) — reuse verbatim for the D-09 overflow path. `newBM25Index([]Spec)` + `rank(query)` already exist, stdlib-only, sub-ms over N≤~50; build the skill corpus as `searchDocument`-style name+description strings and rank:
```go
func (idx *bm25Index) rank(query string) []scoredDoc {
	qterms := tokenize(query)
	out := make([]scoredDoc, 0, len(idx.tf))
	for i := range idx.tf {
		if s := idx.scoreDoc(i, qterms); s > 0 {
			out = append(out, scoredDoc{doc: i, score: s})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		return out[a].doc < out[b].doc
	})
	return out
}
```
Manifest renders skills up to `AURA_SKILL_MANIFEST_CAP_BYTES` (~8k), then ends with `"N more — search with skill action=list {query}"` (D-09). `bm25.go` is in the `tools` package — the skill `list` action (in `tools/skill.go`) can call it directly; the corpus-build helper lives in `internal/skills/manifest.go` and feeds spec-shaped docs.

---

### `internal/skills/loader.go` (service, file-I/O read) — D-28

**Analog:** `internal/conversations/orphan_scan.go` (multi-entry FS scan with Lstat-no-follow symlink guard).

**Anti-pattern guard (D-28 / RESEARCH Anti-Patterns):** the loader validates parse + structure + name regex + size and skip-logs invalid — it NEVER runs the blocklist (disk is operator-trusted, CC parity). Blocklist enforcement is a WRITE-boundary concern only.

The loader's TTL cache (1s) needs a goroutine-free design or a `goleak.VerifyTestMain` (see `main_test.go` analog). Symlink handling at scan reuses the `orphan_scan.go:74-88` Lstat guard (see Shared Patterns → Symlink strip).

---

### `internal/skills/validator.go` (utility, transform) — D-27/D-28

**Analog:** `internal/identity/store.go` `validateGrantInput`/`capNameRe` (the single-chokepoint validate-before-act pattern + the compiled-once name regex).

**SanitizeName single chokepoint + compiled-once regex** (`store.go:32-33`, `store.go:160-168`):
```go
var capNameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func (s *Store) validateGrantInput(identityID, capability string) (pgtype.UUID, error) {
	if capability == Wildcard {
		return pgtype.UUID{}, ErrWildcardManaged
	}
	if !capNameRe.MatchString(capability) {
		return pgtype.UUID{}, fmt.Errorf("%w: %q must match %s", ErrInvalidCapability, capability, capNameRe.String())
	}
	return parseUUID(identityID)
}
```
Skill name regex per D-30: `name` ≤64, `[a-z0-9-]`, must match dir name — a compiled-once `skillNameRe` mirroring `capNameRe`. Sentinel errors (`ErrInvalidName`, `ErrBlocklisted`) mirror the `Err*` declarations at `store.go:39-43`.

**NFKC + blocklist (the new logic, RESEARCH Code Example, Pitfall 2):**
```go
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
NFKC-normalize FIRST, then literal blocklist — a fullwidth/compatibility variant must NOT slip past (SC#3 fuzz target). `golang.org/x/text` is already vendored (indirect → promote to direct).

---

### `internal/skills/catalog.go` (service, HTTP client) — D-11

**Analog:** `internal/sandboxagent/client.go` (the stdlib `net/http` JSON client — construction, timeout, status-class check, decode).

**HTTP client construction + status-class guard + decode** (`client.go:51-92`):
```go
func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" { base = DefaultBaseURL }
	timeout := cfg.TimeoutSec
	if timeout <= 0 { timeout = DefaultTimeoutSec }
	return &Client{baseURL: base, http: &http.Client{Timeout: time.Duration(timeout) * time.Second}}
}
// ... in Run():
if resp.StatusCode < 200 || resp.StatusCode > 299 {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return out, fmt.Errorf("sandbox-agent run HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
}
if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { ... }
```
Catalog deviation (D-11, Pitfall 5): LAX decode — NEVER `DisallowUnknownFields` (`isDuplicate`/`count` drift in/out); guard empty query before the call (server returns 400); rank by `Installs` desc. Isolate the transport so the future public API (vercel-labs/skills#426) is a drop-in swap. `AURA_SKILL_CATALOG_URL` config field mirrors `Config.BaseURL`. Decode struct per RESEARCH Code Example:
```go
type catalogItem struct {
	ID string `json:"id"`; SkillID string `json:"skillId"`
	Name string `json:"name"`; Installs int `json:"installs"`; Source string `json:"source"`
}
```

---

### `internal/skills/installer.go` (service, file-I/O / batch) — D-15

**Analog:** `internal/cron/handlers/backup.go` (LookPath-gated fixed-argv `exec.CommandContext`) + spike `004b-install-native-clone/main.go` (the proven clone+copy+symlink-strip+hash).

**LookPath fixed-argv exec** (`backup.go:133-142`, `backup.go:100-103`):
```go
docker, err := exec.LookPath("docker")    // never a shell string
// G204: docker + the argv are operator-config constants ... NEVER model output
cmd := exec.CommandContext(runCtx, docker, args...) //nolint:gosec
```
Installer uses `exec.LookPath("git")` + fixed argv `clone --depth 1 --single-branch -c core.autocrlf=false`. The `repoURL` is the ONLY interpolated value (validate it before the call).

**Proven clone + symlink-strip copy + canonical hash** (spike `004b/main.go:64-110`):
```go
cmd := exec.CommandContext(ctx, gitPath, "clone", "--depth", "1", "--single-branch", repo, cloneDir)
cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0") // never hang on auth prompts
// ... copy the skill dir, REJECTING symlinks (spike 005 finding):
if d.Type()&fs.ModeSymlink != 0 {
	symlinksRejected++
	return nil // strip — never copy through
}
// then Aura's OWN canonical hash: sort entries by relPath; sha256(relPath bytes + content bytes)
```
Do NOT interop with upstream `skills-lock.json` `computedHash` (locale/platform-sensitive, spike 004b). Compute Aura's own byte-sorted `(relPath,bytes)` sha256 as the TOFU pin.

---

### `internal/skills/audit_store.go` + `internal/skills/writer.go` (store, CRUD) — D-29

**Analog:** `internal/askuser/store.go` (the canonical Store pattern, doc comment `store.go:1-14` names the lineage explicitly) — itself a copy of `internal/identity/store.go` (D-A4-01).

**Canonical Store shape** (`identity/store.go:45-57`, the pattern askuser copies):
```go
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }
```
Non-tx reads/INSERTs use `s.q`; atomic multi-statement writes wrap `db.WithTx` (askuser uses it for `MarkResumedBatch`/`AutoResolveForConversation`). The audit store is INSERT-only for `aura_app` (D-29).

**SQLSTATE classification, never message-match** (`identity/store.go:180-185`, RESEARCH Pitfall 2 / memory `project_reset_fixed_status_pgx_bug`):
```go
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
```
The writer (pending→active atomic) follows the `task.go:187-220` gate flow (status=`pending_approval` when `GateRecommended`) but persists to FS (`pending/`→`active/`) + an audit INSERT in one `db.WithTx`. The audit row carries the D-29 coherence-matrix columns (`approval_source`/`paused_state_token`/`gate_recommended`/`gate_taken`/`content_hash`/`blocklist_override`).

---

### `internal/cron/handlers/skill_ttl.go` (handler, event-driven) — D-16

**Analog:** `internal/cron/handlers/backup.go` `BackupHandler` (Meta + Run + sweep) — exact structural match, including the retention-sweep idiom.

**Handler interface shape** (`dispatch.go:49-52` is the contract; `backup.go:63-69` the impl template):
```go
type Handler interface {
	Meta() HandlerMeta
	Run(ctx context.Context, job Job) (summary string, err error)
}
// BackupHandler.Meta:
func (h BackupHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: kind, MaxDuration: backupMaxDuration, ReschedulesOnRecovery: true}
}
```
`SkillTTLSweepHandler.Meta()` → `HandlerMeta{Kind: "skill_ttl_sweep", MaxDuration: 2*time.Minute, ReschedulesOnRecovery: false}` (RESEARCH Pattern 4). `Run` scans usage sidecars, archives skills past `AURA_SKILL_SNIPPET_TTL_DAYS`, de-materializes — mirroring `backup.go:198-220` `sweepRetention` (best-effort, log-and-skip, return count).

**Handler lives in `internal/cron/handlers/`** (imports `internal/agent/tools`), adapted onto the cron-local `Handler` at the composition root (`cmd/aura/serve.go:106-112`):
```go
real := map[cron.TaskKind]handlers.Handler{
	cron.KindReminder:       handlers.ReminderHandler{},
	cron.KindAgentJob:       handlers.AgentJobHandler{Deps: agentDeps},
	cron.KindBackupPostgres: handlers.BackupHandler{Variant: handlers.BackupPostgres},
	cron.KindBackupNeo4j:    handlers.BackupHandler{Variant: handlers.BackupNeo4j},
}
```
Add `KindSkillTTLSweep` to `internal/cron/store.go:27-35` (next to `KindReminder` etc.) and register `handlers.SkillTTLSweepHandler{...}` here. **The TaskKind is system-seeded, NOT model-schedulable** — the `task.go` tool's `kind` enum stays unchanged (RESEARCH Pattern 4 note).

---

### `internal/db/migrations/0010_skill_audit.up.sql` (migration, DDL) — D-29

**Analog:** `internal/db/migrations/0009_scheduler.up.sql` (role grants + CHECK + audit-forever-no-DELETE + golang-migrate CONCURRENTLY caveat).

**Role separation + audit grants** (`0009:57-63`, the precedent; skill_audit is INSERT/SELECT-only):
```sql
-- aura_app gets DML only — DDL is reserved to aura_migrate.
GRANT SELECT, INSERT, UPDATE         ON aura.agent_job_runs   TO aura_app;  -- NO DELETE
GRANT ALL                            ON aura.agent_job_runs   TO aura_migrate;
```
For `skill_audit`: `GRANT SELECT, INSERT ON aura.skill_audit TO aura_app;` (NO UPDATE/DELETE/TRUNCATE — append-only, D-29) + `GRANT ALL ... TO aura_migrate;`.

**Plain (non-CONCURRENT) index on fresh table** (`0009:48-52`, RESEARCH Pitfall 6):
```sql
-- Plain (non-CONCURRENT) build on a fresh empty table inside the implicit
-- migration tx — golang-migrate forbids CONCURRENTLY in a tx block (0007 precedent).
CREATE INDEX scheduler_tasks_due_idx ON aura.scheduler_tasks (next_run_at)
    WHERE status = 'active';
```

**NEW in 0010 (no in-repo trigger analog — write from D-29 + Pitfall #6):**
- BEFORE UPDATE OR DELETE FOR EACH ROW trigger raising an exception (append-only)
- a SEPARATE BEFORE TRUNCATE FOR EACH STATEMENT trigger (TRUNCATE bypasses row triggers — Pitfall 1)
- the D-29 audit-coherence CHECK over `(approval_source, paused_state_token, gate_recommended, gate_taken)`
- **ALTER the 0009 `scheduler_tasks.kind` CHECK** to add `'skill_ttl_sweep'` (RESEARCH A2 — currently `IN ('reminder','agent_job','backup_postgres','backup_neo4j')` at `0009:16`; seeding the sweep task fails the constraint without this widening).

---

### `internal/agent/prompt.go` (config, MODIFIED) — D-06 static mechanism sentence

**Analog:** itself — the byte-stable `SystemPrompt` const (`prompt.go:14-20`).

```go
// SystemPrompt is Aura's first real system prompt (D-09): a minimal, tool-aware,
// BYTE-STABLE prefix. It is a package constant — never templated ... It explains
// the tool MECHANISM ... WITHOUT enumerating individual tool names — enumeration
// would cache-bust the prefix every time the tool set changes ...
const SystemPrompt = `You are Aura, a domain-neutral agentic substrate ...`
```
The ONLY allowed edit (D-06): add ONE static mechanism-not-enumeration sentence ("skills exist; the skill tool lists/applies them"), then freeze forever. Mechanism not enumeration — same discipline as the existing tool_search sentence. English-only (memory `feedback_all_prompts_in_english_only`).

---

### `internal/conversations/context.go` (service, MODIFIED) — D-07 flagged constraint

**Analog:** itself — `dropOldestPairs` already preserves the seq=1 system L0 turn (`context.go:181-201`).

```go
func dropOldestPairs(enc *tiktoken.Tiktoken, turns []Turn, hardCap int) ([]Turn, int) {
	start := 0
	if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
		start = 1   // the system L0 turn is never dropped
	}
	system := turns[:start]
	body := append([]Turn(nil), turns[start:]...)
	// ... drops oldest user/assistant pairs from body until under hardCap ...
}
```
**The D-07 constraint lands here (Pitfall 3):** the messages[1] always-skills block is a user-role message; the evictor currently treats user messages as droppable. Teach `dropOldestPairs` (and `applyL1` at `context.go:146-165`, which already skips seq=1) to ALSO protect the messages[1] block like L0 — otherwise a 20+-turn conversation silently evicts always-on/haiku mode. The `seq == 1` preservation idiom is the exact pattern to extend.

---

### `cmd/aura/main.go` `buildBaseRegistry` (config wiring, MODIFIED) — D-05

**Analog:** itself — the existing `reg.Register(...)` block (`main.go:97-115`).

```go
reg.Register(tools.TextResponse{})
reg.Register(&tools.ToolSearch{Registry: reg})
reg.Register(tools.AskUser{})
reg.Register(newTaskTool(ts))
reg.Register(&tools.SandboxExec{Runner: sandboxagent.New(cfg.SandboxAgent)})
```
Add `reg.Register(&tools.SkillTool{Loader: ...})` (non-deferred, D-05). The `newTaskTool(ts)` helper (a consumer-side adapter wiring the live store onto the tool's seam) is the template for `newSkillTool(...)` — keep `internal/agent/tools` free of an `internal/skills` import via a consumer-declared interface (the `taskStore` seam at `task.go:70-76` is the pattern). Manifest auto-sorts (comment at `main.go:107`) — never hand-order.

---

## Shared Patterns

### Risk tiering — `scoring.ComputeSkillTier` (SHIPPED, unwired — Phase 11 is the consumer)
**Source:** `internal/scoring/scoring.go:160-177`
**Apply to:** skill `create|update|install|delete` write paths + the gate-recommended decision in `writer.go` and `tools/skill.go`.
```go
func ComputeSkillTier(action SkillAction, body string) RiskTier {
	_ = body
	switch action {
	case SkillDelete:
		return Destructive
	case SkillCreate, SkillUpdate, SkillInstall:
		return Risky
	default:
		return Risky
	}
}
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
func RequiresImmediateAlert(tier, threshold RiskTier) bool { return rank(tier) >= rank(threshold) }
```
The `SkillAction` constants (`SkillCreate`/`SkillUpdate`/`SkillInstall`/`SkillDelete`) are already declared at `scoring.go:48-57`. scoring is PURE — pass the threshold as an argument, never read env (config owns `AURA_RISK_ALERT_THRESHOLD`).

### Symlink strip (Lstat-no-follow)
**Source:** `internal/conversations/orphan_scan.go:74-88`
**Apply to:** `installer.go` (clone copy) + `materialize.go` (activation→export dir) + `loader.go` (FS scan) — Pitfall 4 (symlinks resolve in-container → escape `/skills`).
```go
info, lerr := os.Lstat(full)  // does NOT follow
if lerr != nil { /* warn + skip */ continue }
if info.Mode()&os.ModeSymlink != 0 {
	if rmErr := os.Remove(full); rmErr != nil { /* warn */ }
	continue  // unlink the symlink itself; never traverse it
}
```
spike 004b uses the WalkDir variant (`d.Type()&fs.ModeSymlink != 0` → skip) — both are valid; the orphan_scan idiom is the in-repo canonical.

### Sandbox execution — the ONLY exec seam (D-04/D-17)
**Source:** `internal/agent/tools/sandbox_exec.go` + `internal/sandboxagent/client.go`
**Apply to:** 7e snippet execution. Phase 11 adds NO new exec code — `action=use` returns the in-sandbox path; the model calls `sandbox_exec {command: "python3", args: ["/skills/<name>.py", ...]}`. ALWAYS interpreter+path, NEVER the exec bit (spike 005: Docker Desktop masks 777). `sandbox_exec` is non-deferred for the same schema-blindness reason as `skill` (comment at `sandbox_exec.go:64-67`).

### Notifier for gate-skipped headless alerts (D-26)
**Source:** `internal/cron/dispatch.go:143-161` (the composite Notifier + quiet-hours + immediate-alert flow)
**Apply to:** headless mutation alerts ("Aura proposed skill X overnight — approve?"). Reuse the Phase-10 composite chain + quiet-hours semantics AS-IS (CONTEXT Claude's Discretion). A headless mutation lands in `pending/` + IMMEDIATE Notifier alert + audit `gate_taken=false` (can never self-activate).

### goleak TestMain
**Source:** existing `internal/cron/handlers/main_test.go` (and per-package convention)
**Apply to:** `internal/skills/main_test.go` — `goleak.VerifyTestMain(m)` (loader TTL cache + sweep must not leak goroutines).

### ask_user approval/resume channel (D-03)
**Source:** `internal/askuser/store.go` (`ActionAccept`/`ActionDecline`/`ActionCancel` at `store.go:34-38`) + `internal/agent/llm_agent_pause.go`
**Apply to:** the D-03 resume handler = Writer.Activate + Invalidate + audit. NO model-facing `approve` action. The `proxied_*`/`paused_states` columns are already shipped (CONTEXT Reusable Assets).

## No Analog Found

| File | Role | Data Flow | Reason / Reference |
|------|------|-----------|--------------------|
| `internal/skills/frontmatter.go` | utility | transform | No in-repo YAML parse. NEW dep `github.com/goccy/go-yaml` v1.19.2 (RESEARCH Standard Stack); CRLF-normalize before parse (spike 006). picobot's hand-roll is the explicit ANTI-reference. |
| `internal/skills/validator_fuzz_test.go` | test (fuzz) | transform | No in-repo `go test -fuzz` target. `FuzzSkillValidator`, 10K NFKC/Unicode mutations of blocklist patterns (SC#3). Go stdlib native fuzzing — no third-party lib. |
| `0010` append-only / TRUNCATE triggers + D-29 CHECK SQL | migration | DDL | No existing trigger in-repo (0009 has role grants only). Write from the D-29 matrix + Pitfall #6 (two triggers: BEFORE UPDATE/DELETE row + BEFORE TRUNCATE statement). |

> All three are NEW logic but fully specified by RESEARCH (library + version verified)
> and CONTEXT (D-29 matrix, SC#3). The planner writes them from spec, not by copying
> an analog.

## Metadata

**Analog search scope:** `internal/agent/tools/`, `internal/scoring/`, `internal/cron/` (+ `handlers/`), `internal/identity/`, `internal/askuser/`, `internal/sandboxagent/`, `internal/conversations/`, `internal/db/migrations/`, `cmd/aura/`, `.planning/spikes/004b`
**Files scanned (read in full or targeted):** task.go, action.go, search.go, bm25.go, scoring.go, dispatch.go, store.go (cron/identity/askuser), backup.go, sandbox_exec.go, sandboxagent/client.go, prompt.go, context.go, orphan_scan.go, 0009_scheduler.up.sql, main.go (buildBaseRegistry), serve.go (seed map), spike 004b main.go
**Pattern extraction date:** 2026-06-05
