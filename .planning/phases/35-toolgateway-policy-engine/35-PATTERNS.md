# Phase 35: ToolGateway + Policy Engine - Pattern Map

**Mapped:** 2026-07-03
**Files analyzed:** 4 net-new `internal/gateway` files + 3 composition-root call sites + 4 modify targets (ledger query/store/spec edits)
**Analogs found:** 11 / 11 (every net-new file has a live in-tree analog; zero external-only patterns)

> **Scope note.** `35-RESEARCH.md` already maps the seams to analogs with file:line citations (§Code-Verification Findings, §Don't Hand-Roll, §Architectural Responsibility Map). This document does **not** re-derive that seam map. Its net-new value is **copy-ready code-excerpt skeletons** for the net-new `internal/gateway` package: for each file, the closest live analog + the real excerpt the executor mirrors + a labelled `SKELETON` of the net-new code. Every `ANALOG` block is verbatim on-disk at HEAD (2026-07-03); every `SKELETON` block is net-new code to write, shaped to mirror the analog. Line numbers re-verify against the cited file if `internal/agent`/`internal/toolinvocations` are refactored before execution.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/gateway/decide.go` (NEW) | service (PEP) | request-response | `internal/agent` injected-collaborator (`HookManager` field) + `internal/config.RuntimeProfile` consumer | role-match (net PEP; no prior policy service) |
| `internal/gateway/classify.go` (NEW) | utility (pure transform) | transform | `internal/scoring` tier funcs + `tools/skill.go`/`task.go` head-unmarshal | exact (pure classifier over existing tiers + arg shapes) |
| `internal/gateway/reserve.go` (NEW) | service (store orchestration) | CRUD (INSERT reserve + SELECT replay) | `askuser/store.go` `MarkResumedTx` (`:execrows` rows==0 idiom) + `toolinvocations.Store.Insert`/`toParams`/`RedactForLedger` | exact (byte-for-byte the Phase-34 `:exec→:execrows` move) |
| `internal/gateway/reconcile.go` (NEW) | service (background worker) | batch / event-driven | `conversations/sweeper.go` (Start/Stop/goleak) + `orphan_scan.go` (`tmpTTL` age-grace, WARN-recover loop) | exact (mirror the lifecycle + grace-window) |
| `internal/gateway/*_test.go`, `main_test.go` (NEW) | test | — | `toolinvocations/main_test.go` (`goleak.VerifyTestMain`) + `store_integration_test.go` (`//go:build db_integration`) | exact |
| `internal/agent/llm_agent.go` `LlmAgentConfig` (MODIFY) | config struct | — | its own `HookManager *HookManager` field (llm_agent.go:125-126) | exact (add one optional field, nil = no-op) |
| `internal/agent/llm_agent_retry.go` `execTool` (MODIFY) | enforcement point | request-response | the existing `execTool` retry seam (llm_agent_retry.go:38-52) | exact (interpose Decide above `tool.Execute`) |
| `internal/agent/tools/{skill,task,swarm_spawn}.go` `Spec()` (MODIFY) | tool descriptor | — | `tools.Spec.Mutating` field (spec.go:38-45) — 3× `Mutating:true` | exact |
| `internal/db/queries/tool_invocations.sql` (MODIFY) | SQL | CRUD | `paused_states.sql:48-53` `MarkPausedStateResumed :execrows` | exact |
| `internal/toolinvocations/store.go` (MODIFY) | store | CRUD | its own `Insert`/`toParams` (store.go:61-154) | exact (add `Reserve`+`GetEnd`, adjust `Insert` rowcount) |
| `internal/runner/runner.go` `buildAgent` (MODIFY) | composition root | — | its own `NewLlmAgent(...)` call (runner.go:540-552) | exact |
| `internal/swarm/swarm.go` `runChild` (MODIFY) | composition root | — | its own `NewLlmAgent(...)` call (swarm.go:167-175) | exact |
| `internal/cron/handlers/handler.go` `newAgentWorker` (MODIFY) | composition root | — | its own `NewLlmAgent(...)` call (handler.go:113-122) + `AgentDeps` (76-89) | exact |

---

## Pattern Assignments

### `internal/gateway/decide.go` (service — PEP, request-response)

**Analog A — how an `internal/agent` seam receives an injected collaborator** (`HookManager`, the optional Phase-21 extension surface — this is exactly the shape the `Gateway` mirrors: an optional pointer, nil = no-op).

ANALOG — `internal/agent/llm_agent.go:125-126` (struct field) + `internal/agent/llm_agent_construct.go:36` (assignment):
```go
// HookManager is the optional Phase-21 extension surface. nil is a no-op.
HookManager *HookManager
// ...in NewLlmAgent:
hooks:       cfg.HookManager,
```

**Analog B — the `RuntimeProfile` consumer branch** (Phase 35 is its FIRST runtime consumer; the enum + `Strict()` are shipped, but `Strict()` alone is insufficient for D-03b — branch on the full enum for the `server_production` approve-degradation, per RESEARCH CV-6).

ANALOG — `internal/config/config_runtimeprofile.go:22-32,56-58`:
```go
const (
	ProfileDev                RuntimeProfile = "dev"
	ProfileLocalTrusted       RuntimeProfile = "local_trusted"
	ProfileSingleUserHardened RuntimeProfile = "single_user_hardened"
	ProfileServerProduction   RuntimeProfile = "server_production"
)
func (p RuntimeProfile) Strict() bool {
	return p == ProfileSingleUserHardened || p == ProfileServerProduction
}
```

SKELETON — `internal/gateway/decide.go` (net-new; mirror Analog A's optional-collaborator shape, Analog B's profile branch):
```go
package gateway

// Gateway is the single in-process policy-enforcement point (GATE-01). It holds
// the resolved runtime profile + the append-only ledger store; the agent stays
// DB-free by delegating to it (mirrors how LlmAgent holds *HookManager).
type Gateway struct {
	profile config.RuntimeProfile
	store   reservationStore // seam over *toolinvocations.Store (reserve.go)
	// clock/grace/OTel knobs are Claude's Discretion.
}

type Decision string

const (
	Allow   Decision = "allow"
	Deny    Decision = "deny"
	Approve Decision = "approve"
)

type Verdict struct {
	Decision Decision
	Tier     scoring.RiskTier
	Reason   string
	Replay   *toolinvocations.Event // non-nil ⇒ rows==0, return recorded outcome instead of Execute
}

// Decide is the PEP. Called INSIDE execTool before tool.Execute (see execTool
// interposition below). nil *Gateway ⇒ Allow no-op (dev-parity for tests/standalone).
func (g *Gateway) Decide(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) (Verdict, error) {
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "no-op (dev/local_trusted)"}, nil // SC-4
	}
	tier := classify(spec, rawArgs)          // classify.go (monotone de-escalator)
	if !spec.Mutating {                       // D-01e: read-only → decision-fact only, no reservation
		return Verdict{Decision: Allow, Tier: tier}, nil
	}
	if scoring.GateRecommended(tier) {        // Risky||Destructive → approve routing (D-03)
		return g.routeApprove(ctx, spec, tier, key)
	}
	acquired, replay, err := g.store.Reserve(ctx, key, spec, rawArgs) // reserve.go
	if err != nil {
		return Verdict{Decision: Deny, Tier: tier, Reason: "reservation failed"}, nil // GATE-03: fail-closed, NO Execute
	}
	if !acquired {
		return Verdict{Decision: Allow, Tier: tier, Replay: replay}, nil // GATE-04: rows==0 → replay
	}
	return Verdict{Decision: Allow, Tier: tier}, nil
}
```

**Analog C — the approve branch reuses `ErrAwaitingUserInput` + `skillApprovalPriority` (D-03/D-03c); it builds NO new approval UX.**

ANALOG — `internal/agent/tools/ask_user.go:67-83` (the pause sentinel + `ResumeContext` the `{"type":"gateway_approval",…}` rides) and `internal/agent/tools/skill_write.go:280-285` (priority so a security approval is not buried in the FIFO):
```go
type ErrAwaitingUserInput struct {
	Question   string
	Options    []Option
	Kind       string   // KindApproval = "approval" (ask_user.go:23)
	Priority   int
	ToolCallID string
	ResumeContext json.RawMessage // e.g. {"type":"skill_approval","skill_name":"x"} → here {"type":"gateway_approval",...}
	ProxiedFromChildID string
	ProxiedToolCallID  string
}
func skillApprovalPriority(tier scoring.RiskTier) int {
	if tier == scoring.Destructive { return 80 }
	return 60
}
```

SKELETON — `routeApprove` (D-03a default-DENY unless an interactive responder is positively known; D-03b `server_production` degrades to deny-with-guidance until Phase 36; the resume MUST re-enter `Decide` so the approved call still takes its reservation):
```go
func (g *Gateway) routeApprove(ctx context.Context, spec tools.Spec, tier scoring.RiskTier, key ReservationKey) (Verdict, error) {
	// D-03b: production identity unverified pre-Phase-36 → never interactive.
	if g.profile == config.ProfileServerProduction || !responderPresent(ctx) { // D-03a default DENY
		return Verdict{Decision: Deny, Tier: tier, Reason: "no interactive approver — action declined"}, nil
	}
	// single_user_hardened + responder present → emit the shipped pause sentinel.
	return Verdict{Decision: Approve, Tier: tier}, &tools.ErrAwaitingUserInput{
		Kind:          tools.KindApproval,
		Priority:      skillApprovalPriority(tier), // reuse — do NOT re-derive
		ResumeContext: gatewayApprovalContext(spec, key), // {"type":"gateway_approval",...}
	}
}
```

> Enforcement-vs-classification stays separated (D-02d): `classify` assigns the tier; `scoring.GateRecommended`/`profile.Strict()` decide enforcement. Do NOT double-gate `skill`create/update/delete or `task`schedule — they already run their own internal pause (RESEARCH CV-4); the gateway's role is the Mutating floor + reservation + recorded verdict.

---

### `internal/gateway/classify.go` (utility — pure transform)

**Analog A — the risk-tier vocabulary, reused verbatim** (`internal/scoring` is UNTOUCHED). Note the built-in `unknown→Risky` saturation (`rank`) and the mutation-only `ComputeSkillTier` landmine (its `default` returns `Risky` — feeding it `list` gates).

ANALOG — `internal/scoring/scoring.go:16-27,58-65,126-136,140`:
```go
type RiskTier string
const ( Safe RiskTier="safe"; Normal RiskTier="normal"; Risky RiskTier="risky"; Destructive RiskTier="destructive" )
func rank(t RiskTier) int { for i,k := range tierOrder { if k==t { return i } }; return rank(Risky) } // unknown→Risky
func ComputeSkillTier(action SkillAction, body string) RiskTier {
	switch action {
	case SkillDelete: return Destructive
	case SkillCreate, SkillUpdate, SkillInstall: return Risky
	default: return Risky            // ⚠ D-02c LANDMINE: "list"/"info"/"use" hit this → Risky. NEVER route reads here.
	}
}
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
```

**Analog B — the ground-truth arg shapes + head-unmarshal** (parse the discriminator exactly as the tools do; a parse failure saturates to `Risky`, never `Safe`).

ANALOG — `internal/agent/tools/skill.go:84-88,112` and `:158-169` (the head-unmarshal idiom):
```go
type skillArgs struct { Action string `json:"action"`; Name string `json:"name"`; Query string `json:"query"` }
// action enum (skill.go:112): list, info, use, create, update, delete, save_snippet, restore, archive
var head struct{ Action string `json:"action"` }
if err := json.Unmarshal(raw, &head); err != nil { return ToolResult{}, fmt.Errorf("skill args: %w", err) }
```
ANALOG — `internal/agent/tools/task.go:88-100,110,116` + the `agent_job` force-gate at `:211-213`:
```go
type taskArgs struct { Action string `json:"action"`; ScheduleKind string `json:"schedule_kind"`; /*...*/ Kind string `json:"kind"`; Payload json.RawMessage `json:"payload"`; /*...*/ }
// action enum (task.go:110): schedule, list, cancel, run_now   |   kind enum (task.go:116): reminder, agent_job, backup_postgres, backup_neo4j
if a.Kind == "agent_job" { status = "pending_approval" } // AG-016: the gateway must replicate this ≥Risky force — scoring alone returns Normal for a plain agent_job (RESEARCH CV-4)
```
ANALOG — `internal/agent/tools/swarm_spawn.go:48-50` (NO action field → flat `Risky`, never de-escalated; also `Deferred:true` at :78):
```go
type swarmSpawnArgs struct { Goals []string `json:"goals"` }
```

SKELETON — `internal/gateway/classify.go` (monotone de-escalator: default mutating; ONLY explicitly-enumerated reads lower to `Safe`; unknown/empty/parse-fail → `Risky`):
```go
// classify is the monotone saturate-upward de-escalator (D-02). Default is always
// mutating; reads are allow-listed HERE (never scored). Unknown/empty/parse-error → Risky.
func classify(spec tools.Spec, rawArgs json.RawMessage) scoring.RiskTier {
	switch spec.Name {
	case "skill":
		return classifySkill(rawArgs)
	case "task":
		return classifyTask(rawArgs)
	case "swarm_spawn":
		return scoring.Risky // no action field — full-capability worker turn, AG-016 parity
	default:
		if spec.Mutating { return scoring.Risky } // shell_exec/fs_write + MCP !ReadOnlyHint already correct
		return scoring.Safe
	}
}

func classifySkill(raw json.RawMessage) scoring.RiskTier {
	var a struct{ Action string `json:"action"` }
	if err := json.Unmarshal(raw, &a); err != nil { return scoring.Risky } // D-02c: parse-fail → Risky, never Safe
	switch a.Action {
	case "list", "info", "use":                 return scoring.Safe    // READ allow-list (NOT ComputeSkillTier)
	case "restore", "archive", "save_snippet":  return scoring.Normal
	case "create", "update", "delete":          return scoring.ComputeSkillTier(scoring.SkillAction(a.Action), "")
	default:                                     return scoring.Risky   // empty/unknown → Risky
	}
}

func classifyTask(raw json.RawMessage) scoring.RiskTier {
	var a struct{ Action, Kind string; Payload json.RawMessage }
	if err := json.Unmarshal(raw, &a); err != nil { return scoring.Risky }
	switch a.Action {
	case "list":    return scoring.Safe
	case "cancel":  return scoring.Normal
	case "run_now": return scoring.Risky
	case "schedule":
		t := scoring.ComputeTaskTier(scoring.TaskArgs{Kind: a.Kind, Payload: a.Payload})
		if a.Kind == "agent_job" && scoring.Rank(t) < scoring.Rank(scoring.Risky) { return scoring.Risky } // AG-016 force ≥Risky
		return t
	default:        return scoring.Risky
	}
}
```
> `scoring.rank` is unexported today; if the classifier needs the ordering, either add an exported `scoring.Rank`/`AtLeast` helper (keeps `scoring` pure) or compare tiers via a tiny local order map — planner micro-decision. `install` in D-02b's table is dead (not in the live 9-action enum — RESEARCH CV-4); mapping it is harmless.

**Optional boot-guard (D-02d, recommended)** — turn "a new unlisted multiplexed tool silently under-gates" into a loud wiring panic, mirroring the established `Register`-panic idiom.

ANALOG — `internal/agent/tools/spec.go:111-117` (fail-loud registration) + `:138-146` (`Validate` fail-closed at boot):
```go
func (r *Registry) Register(t Tool) {
	name := t.Spec().Name
	if _, dup := r.tools[name]; dup { panic(fmt.Sprintf("tools.Registry.Register: duplicate tool name %q ...", name)) }
	r.tools[name] = t
}
```

---

### `internal/gateway/reserve.go` (service — CRUD: reserve INSERT + replay SELECT)

**Analog A — the `:exec→:execrows` conditional-write IS the idempotency key** (Phase-34 D-03/D-06, byte-for-byte). The Go consumer maps `rows==0` to a sentinel; `reserve.go` maps it to "replay instead of re-invoke".

ANALOG — `internal/db/queries/paused_states.sql:48-53` (the `:execrows` precedent) + its generated signature `internal/db/sqlc/paused_states.sql.go:260-266` (what `sqlc generate` produces — returns `RowsAffected()`) + the Go rows==0 idiom `internal/askuser/store.go:286-293`:
```sql
-- name: MarkPausedStateResumed :execrows
UPDATE aura.paused_states SET resumed_at = now(), resumed_answer = $2
WHERE token = $1 AND resumed_at IS NULL;
```
```go
func (q *Queries) MarkPausedStateResumed(ctx context.Context, arg ...) (int64, error) {
	result, err := q.db.Exec(ctx, markPausedStateResumed, arg.Token, arg.ResumedAnswer)
	if err != nil { return 0, err }
	return result.RowsAffected(), nil
}
// consumer (askuser/store.go:286-293) — the rows==0 branch reserve.go mirrors:
n, err := q.MarkPausedStateResumed(ctx, ...)
if err != nil { return fmt.Errorf("mark resumed %s: %w", token, err) }
if n == 0 { return fmt.Errorf("mark resumed %s: %w", token, ErrPauseNotFound) } // rows==0 ⇒ already held
```

**Analog B — the ledger query to promote + the net-new replay SELECT** (`ListToolInvocationsByConversation :many` is too coarse; narrow it to a single `end` fact by the GATE-04 key).

ANALOG — `internal/db/queries/tool_invocations.sql:1-15` (`:exec`→`:execrows`, KEEP `DO NOTHING`) and `:17-25` (the `:many` to narrow into a `:one`):
```sql
-- name: InsertToolInvocation :exec        ← CHANGE to :execrows (keep ON CONFLICT DO NOTHING)
INSERT INTO aura.tool_invocations ( …21 cols… ) VALUES ( … )
ON CONFLICT (conversation_id, request_id, tool_call_id, event_kind) DO NOTHING;  -- the GATE-04 triple + event_kind
```
SKELETON — the net-new replay query (mirror the `:many` column list, narrow the WHERE, make it `:one`):
```sql
-- name: GetToolInvocationEnd :one
SELECT id, conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts,
       started_at, ended_at, duration_ms, args_raw, args_bytes,
       status, error, result_preview, preview_bytes, result_bytes, result_truncated,
       result_sidecar_path, exit_code, meta
FROM aura.tool_invocations
WHERE conversation_id = $1 AND request_id = $2 AND tool_call_id = $3 AND event_kind = 'end';
```

**Analog C — the store INSERT + redaction chokepoint** (every reservation row goes through `toParams`, so `RedactForLedger` runs for free; the verdict rides `meta jsonb`, D-01 zero-migration). ⚠ `uuidParam` is the headless flat-session landmine (RESEARCH CV-8 / Open Q1 — resolve the `conversation_id` key before writing the reserve seam).

ANALOG — `internal/toolinvocations/store.go:61-70` (`Insert`, currently discards the result) + `:89-154` (`toParams`, the `event_kind`/redaction mapping) + `:156-162` (`uuidParam` — the UUID FK the flat `<conv>-swarm-w2` session violates):
```go
func (s *Store) Insert(ctx context.Context, e Event) error {
	p, err := e.toParams()
	if err != nil { return fmt.Errorf("insert tool invocation: %w", err) }
	if err := s.q.InsertToolInvocation(ctx, p); err != nil { return fmt.Errorf("insert tool invocation %s/%s/%s: %w", ...) }
	return nil // ← after :execrows, InsertToolInvocation returns (int64,error); Insert keeps ignoring the count (behavior-preserving)
}
// toParams (store.go:142,146): RedactForLedger runs BEFORE the durable column — the reservation gets redaction for free.
ArgsRaw:       textOrNull(RedactForLedger(e.Arguments, ArgsRawCapBytes), ...),
ResultPreview: textOrNull(RedactForLedger(e.ResultPreview, ResultPreviewCapBytes), e.Event == EventEnd),
func uuidParam(name, s string) (pgtype.UUID, error) { u, err := uuid.Parse(s); if err != nil { return pgtype.UUID{}, fmt.Errorf("%s %q: %w", name, s, err) }; ... }
```

SKELETON — `internal/toolinvocations/store.go` new methods (add beside `Insert`; the gateway calls `Reserve`):
```go
// Reserve is the synchronous, fatal-on-failure pre-execution reservation (GATE-03).
// rows==1 ⇒ acquired (proceed to Execute); rows==0 ⇒ already held ⇒ SELECT the end
// fact and return it for replay (GATE-04). Mirrors MarkResumedTx's rows==0 idiom.
func (s *Store) Reserve(ctx context.Context, start Event) (acquired bool, replay *Event, err error) {
	p, err := start.toParams() // start Event: EventStart, verdict in Meta (D-01 rides meta jsonb)
	if err != nil { return false, nil, fmt.Errorf("reserve: %w", err) }
	n, err := s.q.InsertToolInvocation(ctx, p) // :execrows now returns (int64, error)
	if err != nil { return false, nil, fmt.Errorf("reserve %s/%s/%s: %w", start.ConversationID, start.RequestID, start.ToolCallID, err) }
	if n == 1 { return true, nil, nil }
	end, err := s.GetEnd(ctx, start.ConversationID, start.RequestID, start.ToolCallID) // may be absent if crash-orphaned in-flight
	if err != nil { return false, nil, err }
	return false, end, nil
}
```
> **Replay-fidelity (Claude's Discretion / Pitfall 6):** the replayed `end` preview is capped (2KiB) + `RedactForLedger`-redacted, and its `result_sidecar_path` may be GC'd by F-040 — replay tolerates a missing sidecar (return preview + a `result expired` marker); do NOT extend sidecar retention. **Pitfall 4:** the reconciler's `end` cannot use `status='indeterminate'` (`status IN ('ok','error')` CHECK, migrations/0011:22) — use `status='error'` + an `indeterminate` flag in `meta`.

---

### `internal/gateway/reconcile.go` (service — background worker, batch/event-driven)

**Analog A — the leak-clean Start/Stop lifecycle** (`conversations.Sweeper`: boot one-shot + interval tick, bounded Stop join, `goleak`-clean). Mirror this VERBATIM — do NOT write a new worker from scratch.

ANALOG — `internal/conversations/sweeper.go:14,33-50,69-89,94-105`:
```go
const stopJoinTimeout = 30 * time.Second
type Sweeper struct {
	interval time.Duration
	sweep    func(context.Context)
	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}
func (s *Sweeper) Start(ctx context.Context) {
	if s.interval <= 0 || s.sweep == nil { return }
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval); defer ticker.Stop()
		for {
			select {
			case <-ctx.Done(): return
			case <-s.stop:    return
			case <-ticker.C:  s.sweep(ctx)
			}
		}
	}()
}
func (s *Sweeper) Stop() {
	s.once.Do(func() { close(s.stop) })
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopJoinTimeout):
	}
}
```

**Analog B — the age-grace cutoff + WARN-recover-not-fatal scan loop** (an orphan younger than the grace window is left alone; per-item failures WARN and recover next tick; only a structural read failure returns an error). Mirror the named-const grace-window idiom.

ANALOG — `internal/conversations/orphan_scan.go:19-32` (the grace const) + `:53-67,79-121` (the scan: per-item WARN-recover, structural-failure-returns-error):
```go
const tmpTTL = 24 * time.Hour
const sidecarOrphanGrace = tmpTTL // a crash-orphan is reconciled only once older than this AND unreferenced
// per-item: WARN + continue (recovered next tick); structural read failure → wrapped error
if lerr != nil { slog.Warn("orphan scan: lstat failed", "path", full, "err", lerr); continue }
```

SKELETON — `internal/gateway/reconcile.go` (append-only, conservative — D-01d):
```go
// reservationOrphanGrace is a crash-signal window (a stuck in-flight mutating call),
// NOT a scratch-file TTL — recommend 15-60 min, smaller than tmpTTL (Open Q4). Named
// const mirroring the sidecarOrphanGrace idiom.
const reservationOrphanGrace = 30 * time.Minute

// Reconciler mirrors conversations.Sweeper: boot one-shot + interval tick, goleak-clean.
type Reconciler struct { /* interval, sweep func, wg, stop, once — copy Sweeper verbatim */ }

// reconcileOrphans appends (never UPDATEs) an end fact for a start∧¬end older than the
// grace window. APPEND-only ⇒ status='error' + {"indeterminate":true} in meta (Pitfall 4).
// A MUTATING orphan is NEVER re-invoked (its side effect may have fired) — mark it and move
// on (D-01d/Pitfall 5); only known read-only/idempotent tools may re-invoke. This makes
// D-02 (trustworthy Mutating) a hard prerequisite.
func (r *Reconciler) reconcileOrphans(ctx context.Context) {
	cutoff := time.Now().Add(-reservationOrphanGrace)
	for _, orphan := range r.store.ListInFlightBefore(ctx, cutoff) { // start∧¬end older than grace
		end := toolinvocations.Event{
			Event: toolinvocations.EventEnd, Status: "error",
			Error: "reservation reconciled: crash-orphaned in-flight call",
			Meta:  map[string]any{"indeterminate": true, "reconciled": true},
			// same {conv,req,toolcall} key ⇒ ON CONFLICT DO NOTHING is a safe no-op if a real end raced in
		}
		if err := r.store.Insert(ctx, end); err != nil { slog.Warn("reconcile: append end failed", "err", err); continue }
	}
}
```
> `ListInFlightBefore` is a net-new query (a `start` with no matching `end`, `started_at < $cutoff`) — model it on the `:many` column list (tool_invocations.sql:17-25) with a `NOT EXISTS`/anti-join predicate.

---

## Composition-Root Injection Sites

The `Gateway` threads in exactly where `HookManager` already does: one optional field on `LlmAgentConfig`, assigned in `NewLlmAgent`, wired at each of the three roots. The PEP call lands inside `execTool`.

**Step 1 — add the field** (mirror `HookManager *HookManager`, llm_agent.go:125-126):
```go
// in LlmAgentConfig (llm_agent.go:100-127):
// Gateway is the optional Phase-35 policy PEP. nil is a no-op (dev-parity).
Gateway *gateway.Gateway
// in NewLlmAgent (llm_agent_construct.go:25-42, beside `hooks: cfg.HookManager`):
gateway: cfg.Gateway,
```

**Step 2 — interpose Decide in `execTool`** (the single PEP; ⚠ thread the `{conv,req,toolcall}` triple — `request_id` is NOT on the tool ctx today, RESEARCH CV-8 / Open Q2).

ANALOG — `internal/agent/llm_agent_retry.go:38-52` (Decide goes at the TOP, before the retry loop) + the call site `internal/agent/llm_agent.go:518,523,530` (`Mutating` is already computed; `WithToolCallContext` carries sessionID+toolCallID but NOT request_id):
```go
// llm_agent.go:518-530 (the caller — note the missing request_id):
run.Mutating = tool.Spec().Mutating
toolCtx := tools.WithToolCallContext(spanCtx, a.sessionID, call.ID, a.runDir, a.previewCap) // no request_id
res, err := a.execTool(toolCtx, tool, run.Mutating, json.RawMessage(call.Function.Arguments))
// llm_agent_retry.go:38-52 (the seam — interpose Decide above the loop):
func (a *LlmAgent) execTool(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, error) {
	var res tools.ToolResult; var err error
	for attempt := 0; ; attempt++ {
		res, err = tool.Execute(ctx, args) // ← Decide gates THIS
		...
	}
}
```
SKELETON (net-new interposition):
```go
func (a *LlmAgent) execTool(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, error) {
	v, verr := a.gateway.Decide(ctx, tool.Spec(), args, reservationKeyFromCtx(ctx, a.sessionID)) // nil gateway ⇒ Allow
	if verr != nil { return tools.ToolResult{}, verr }
	switch v.Decision {
	case gateway.Deny:    return tools.ToolResult{}, &gateway.ErrDenied{Reason: v.Reason}
	case gateway.Approve: return tools.ToolResult{}, v.PauseErr // *ErrAwaitingUserInput (resume RE-ENTERS execTool → Decide)
	}
	if v.Replay != nil { return replayResult(v.Replay), nil } // GATE-04: recorded outcome, no Execute
	// ... existing retry loop unchanged ...
}
```
> `ask_user` is EXEMPT (RESEARCH CV-1/A1): it never reaches `execTool` on the mutating path (the second `Execute` site, llm_agent_pause.go:100, is `ask_user`-only). Do not gate it (recursion risk).

**Step 3 — wire the three roots** (each already calls `NewLlmAgent(LlmAgentConfig{...})`; add `Gateway: <injected>`):

1. ANALOG — `internal/runner/runner.go:540-552` `buildAgent` (the UUID-safe main path; add beside `HookManager: r.hookManager`):
```go
la := agent.NewLlmAgent(agent.LlmAgentConfig{
	Client: r.client, LLM: r.cfg, Registry: r.registry, PreviewCap: r.previewCap, RunDir: r.runDir,
	SessionID: convID, // session_id == conversation_id (D-26) — a real UUID, reservation-safe
	Workspace: r.workspace, UserTurns: seed, Classifier: r.classifier, Breaker: r.breaker,
	HookManager: r.hookManager, // ← add: Gateway: r.gateway,
})
```
2. ANALOG — `internal/swarm/swarm.go:167-175` `runChild` (⚠ FLAT non-UUID SessionID — the Open Q1 landmine; the reservation must key on `rc.ConvID`, not this session):
```go
worker := agent.NewLlmAgent(agent.LlmAgentConfig{
	Client: rc.Client, LLM: rc.LLM, Registry: tools.Without(rc.ParentRegistry, swarmSpawnTool),
	PreviewCap: rc.Cfg.ToolPreviewCap, RunDir: rc.Cfg.RunDir,
	SessionID: fmt.Sprintf("%s-swarm-%s", rc.ConvID, childID), // FLAT — uuid.Parse FAILS → FK 23503
	UserTurns: []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}}, // ← add: Gateway: rc.Gateway,
})
```
3. ANALOG — `internal/cron/handlers/handler.go:113-122` `newAgentWorker` (⚠ FLAT `agent_job:<runID>` SessionID; carry the gateway on `AgentDeps`, handler.go:76-89, keyed on `CreateTaskInput.OriginConversationID`):
```go
return agent.NewLlmAgent(agent.LlmAgentConfig{
	Client: deps.Client, LLM: deps.LLM, Registry: childRegistry(deps.Registry),
	PreviewCap: deps.PreviewCap, RunDir: deps.RunDir,
	SessionID: "agent_job:" + runID, // FLAT — uuid.Parse FAILS
	Workspace: ws, UserTurns: prior, // ← add: Gateway: deps.Gateway,
})
```
> **Open Q1 (top blocker, resolve in planning before the reserve seam):** headless swarm/cron sessions are non-UUID and violate the ledger's `conversation_id uuid NOT NULL REFERENCES aura.conversations(id)` (migrations/0011:7 + `uuidParam` store.go:156-162). Minimal-industrial resolution: key the reservation on the originating conversation UUID (`swarm.RunConfig.ConvID`; cron `CreateTaskInput.OriginConversationID`, task.go:66-70) with the child's `request_id`/`tool_call_id`; OR scope GATE-03 *reservation* to the interactive runner path for Phase 35 (still emit+record the GATE-01 *decision* on headless paths) and document the deferral.

---

## The 3 `Mutating:true` Edits (D-02d)

The `tools.Spec.Mutating` field already exists (spec.go:38-45) and is already consumed (`run.Mutating = tool.Spec().Mutating`, llm_agent.go:518). The only change is flipping it on for the three multiplexed tools; do NOT add a `Classify` closure to `Spec` (keeps policy out of the LLM-visible descriptor).

| File | `Spec()` location | Edit |
|------|-------------------|------|
| `internal/agent/tools/skill.go` | `:127-135` | add `Mutating: true` (fixes the D-43 completion-gate consumer too) |
| `internal/agent/tools/task.go` | `:127-137` | add `Mutating: true` |
| `internal/agent/tools/swarm_spawn.go` | `:65-80` | add `Mutating: true` (keep `Deferred: true`) |

---

## Shared Patterns

### Secret redaction at the persistence chokepoint
**Source:** `internal/toolinvocations/redact.go:82-91` (`RedactForLedger`) + caps `:26-42` (`ArgsRawCapBytes`=8KiB, `ResultPreviewCapBytes`=2KiB), invoked in `store.go:142,146`.
**Apply to:** every reservation/`end` write from `reserve.go` + `reconcile.go` — it runs automatically inside `toParams`, so writing through `Store` gets it for free. Do NOT hand-roll redaction.

### Risk tiers (reused verbatim, `scoring` untouched)
**Source:** `internal/scoring/scoring.go` — `RiskTier`, `ComputeSkillTier`(:126-136), `ComputeTaskTier`(:83-104), `GateRecommended`(:140), `rank`(:58-65, unknown→Risky).
**Apply to:** `classify.go` (mutating branch only). **Landmine:** never route a read action through `ComputeSkillTier` (default→Risky) — allow-list reads in the gateway table.

### `:execrows` conditional-write = idempotency key
**Source:** `internal/db/sqlc/paused_states.sql.go:260-266` (generated signature) + `internal/askuser/store.go:286-293` (rows==0 idiom).
**Apply to:** `reserve.go` — `rows==1` acquire / `rows==0` replay. Run `sqlc generate` after the query edit (RESEARCH Runtime State Inventory: stale generated code will not compile the new `(int64, error)` signature).

### Leak-clean background-worker lifecycle
**Source:** `internal/conversations/sweeper.go:33-105` (Start/Stop/`once`/bounded join) + `orphan_scan.go:53-121` (WARN-recover per item, structural-failure error).
**Apply to:** `reconcile.go`. Wire `Start` into the serve composition root's boot, `Stop` into shutdown; `main_test.go` gets `goleak.VerifyTestMain` (mirror `toolinvocations/main_test.go`).

### Fail-loud boot wiring (optional D-02d guard)
**Source:** `internal/agent/tools/spec.go:111-117` (`Register` panic) + `:138-146` (`Validate` fail-closed).
**Apply to:** an optional `Multiplexed bool` on `Spec` + a `Validate`-style boot-guard that panics if a tool the gateway cannot classify to a concrete tier still has `Mutating=false`.

### Test harness (net-new files)
**Source:** `internal/toolinvocations/main_test.go` (`goleak.VerifyTestMain`) + `store_integration_test.go:1` (`//go:build db_integration`).
**Apply to:** all `internal/gateway/*_test.go`. Reservation/replay/reconcile proofs need the live PG stack (`go test -tags db_integration -race`); the tier `t.Fatal`s under `$CI` if the DSN env is unset (no-skip-as-green). ⚠ Re-seed the `local` identity before the tier (FK 23503 if a parallel/coverage run wiped `...001`) — `tool_invocations` FK to `conversations`→`identity`.

---

## No Analog Found (planner uses RESEARCH.md / net-new logic)

| Concern | Role | Data Flow | Reason |
|---------|------|-----------|--------|
| `decide.go` responder-presence detection (`responderPresent(ctx)`) | service | event-driven | No single existing predicate answers "is an interactive responder positively known here." It is net logic composed from shipped signals (cockpit SSE subscriber / live CLI REPL / live Telegram chat vs headless cron/swarm). RESEARCH CV-7 cites the seams (agentjob.go:23,79-100 auto-reject; swarm.go:196-199 relay-up); the default is DENY (D-03a). |
| `request_id` threading to `execTool` | plumbing | — | Signature change (extend `WithToolCallContext` or pass the triple to `Decide`) with no drop-in analog — RESEARCH Open Q2. Pick the shape in planning. |

---

## Metadata

**Analog search scope:** `internal/agent` (+ `tools`), `internal/config`, `internal/scoring`, `internal/toolinvocations`, `internal/db/{queries,sqlc}`, `internal/conversations`, `internal/askuser`, `internal/runner`, `internal/swarm`, `internal/cron/handlers`.
**Files read (analogs):** 18 (all ≤600 LOC; whole-file or targeted non-overlapping reads).
**Pattern extraction date:** 2026-07-03 (HEAD). Re-verify line numbers if `internal/agent`/`internal/toolinvocations` are refactored before execution.
