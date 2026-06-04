# Phase 9: Swarm (Minimal) - Pattern Map

**Mapped:** 2026-06-04
**Files analyzed:** 16 (5 new, 8 edited, 3 test-only)
**Analogs found:** 16 / 16 (every surface has a verified in-tree analog — this phase is ~90% composition)

> All analogs are codebase ground-truth, read this session, file:line cited. The 09-RESEARCH.md pass already located every line reference; this map turns those into copy-from excerpts the planner pastes into plan actions. **There is NO "no analog found" section — every file has a direct in-repo pattern.**

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/swarm/swarm.go` (NEW) | service (ephemeral runner) | event-driven / batch fan-out | `internal/agent/workflow/parallel.go` | exact (engine to copy leak-idioms from, bypass cancel) |
| `internal/swarm/report.go` (NEW) | model (report contract) | transform | `internal/agent/tools/result.go` (spillover) + `event.go` AwaitingInput | role-match |
| `internal/swarm/brief.go` (NEW) | utility (prompt builder + literals) | transform | `internal/agent/llm_agent_finalize.go` (`finalizeNudge` literal) | role-match |
| `internal/agent/tools/swarm_spawn.go` (NEW) | tool (deferred) | request-response | `internal/agent/tools/web_search.go` (`Deferred:true` adapter) | exact |
| `internal/agent/tools/ask_user.go` (EDIT) | tool (HITL primitive) | request-response | its own current Spec/args shape | self |
| `internal/askuser/store.go` (EDIT) | service (persistence) | CRUD | its own `InsertParams`/`Insert` (proxied drop) | self |
| `internal/runner/runner_persist.go` (EDIT) | service (orchestration) | event-driven | its own `persistPause` (drops proxied) | self |
| `internal/agent/mcptools/bridge.go` (EDIT) | middleware (tool bridge) | transform | its own `Bridge`/`bridgedTool` (`Deferred:false`@88) | self |
| `internal/agent/mcptools/mount.go` (EDIT) | middleware (mount + allowlist) | request-response | its own `Mount`/`MountServer` | self |
| `cmd/aura/main.go` `buildRegistryWithMCP` (EDIT) | config (composition root) | request-response | its own boot loop (fail-hard @121-125) | self |
| `cmd/aura/chat.go` `bootChat` (EDIT) | config (boot) | request-response | its own MCP block (exit(1) @139-144) | self |
| `cmd/aura/mcp.go` `mcpRecipes` (EDIT) | config (recipe registry) | CRUD | its own `calculator` recipe @24-40 | self |
| `cmd/aura/main.go` `aura swarm-demo` (NEW, optional) | route (subcommand) | request-response | `cmd/aura/agent.go` `aura agent dry-run` | exact |
| `internal/swarm/swarm_test.go` (NEW) | test (race+goleak+unit) | — | `internal/agent/workflow/parallel_test.go` + `workflow_test.go` TestMain | exact |
| `internal/swarm/swarm_property_test.go` (NEW) | test (rapid property) | — | `pgregory.net/rapid` props (Phase 2 pattern) + goleak | role-match |
| `internal/eval/dataset_cot_eval.go` + harness (EDIT) | test (live E2E) | event-driven | `internal/eval/harness_cot_eval_test.go` + `dataset_cot_eval.go` | self |

---

## Pattern Assignments

### `internal/swarm/swarm.go` (service, event-driven fan-out) — NEW

**Analog:** `internal/agent/workflow/parallel.go` (the shipped concurrency engine). **CRITICAL: copy the leak-safety idioms VERBATIM, but BYPASS the cancel-siblings semantics** — D-02 wants partial-results (a failed child = report entry, siblings keep running), the OPPOSITE of ParallelAgent's escalate/first-error-cancels-siblings design. See RESEARCH Pattern 1 + Pitfall 2.

**Leak-safety idioms to COPY** (`parallel.go:77-101,117,148-162`):
```go
eg, egCtx := errgroup.WithContext(ic.Ctx) // first non-nil err cancels — DEFANG this for D-02
egCtx, cancel := context.WithCancel(egCtx)
defer cancel()                            // parallel.go:79-80 — always release
// ...
for _, sub := range a.subs {
    childIC := ic.WithContext(egCtx).WithSubAgent(sub)
    childIC.Budget = ic.Budget.Child(len(a.subs)) // parallel.go:93 — shared *atomic.Int32
    eg.Go(func() error {
        if egCtx.Err() != nil { return nil }      // parallel.go:96 — Go #61611 spawn-loop guard
        return runSub(egCtx, childIC, sub, results, done)
    })
}
defer close(done) // parallel.go:117 — drains every parked child on early break
```

**The runSub multi-arm-select drain (the leak invariant to preserve)** (`parallel.go:148-162`):
```go
select {
case <-done:        return nil // consumer broke — clean drain, NOT ctx.Err()
case <-ctx.Done():  return nil // sibling cancelled — nil, NOT ctx.Err() (keeps error slot clean)
case results <- result{event: ev, ack: ack}:
    select {
    case <-ack:        // iterator yielded; produce next
    case <-done:       return nil
    case <-ctx.Done(): return nil
    }
}
```

**D-02 DIVERGENCE (the swarm-specific change):** in the swarm runner, a child error must be **captured into that child's report slot and the goroutine returns NIL** (so `egCtx` never cancels siblings). Do NOT return the error to the errgroup. Use a plain `errgroup`/`WaitGroup` with `_ = eg.Wait()` and per-child report slots indexed by goal order. The swarm collects N reports and returns ONE `tools.ToolResult` — it does NOT yield an Event stream upward (Discretion grants "adapt or bypass ParallelAgent's error slot ... keeping its leak-safety invariants").

**Child pause detection (D-04)** — read AwaitingInput off the Event, don't suspend (`event.go:81-88` + RESEARCH Code Examples):
```go
for ev, err := range worker.Run(childIC) {
    if err != nil { report.Status, report.Error = "failed", err.Error(); break } // D-02
    if ai := ev.Actions.AwaitingInput; ai != nil {                                // D-04
        report.Status = "needs_user_input"
        report.Question, report.Options, report.ToolCallID = ai.Question, ai.Options, ai.ToolCallID
        // worker.Run already returned after emitPauses (llm_agent.go:210-214) — no suspend, no paused_states row
    }
    // final Event → report.Status="ok", report.Summary = ev.LLMResponse.Content
    dumpTranscript(ev) // D-18 best-effort, never fails the swarm
}
```

**Budget pre-flight + snapshot-once (D-09)** (`budget.go:238,285` — snapshot requirement documented at `budget.go:283-284`):
```go
rem := parentBudget.Remaining()        // ONCE before the fan-out loop (equal sibling shares)
if rem < len(goals)+reserve {          // reserve ~3, tune to parent post-swarm synthesis
    return tools.NewResult(ctx, fmt.Sprintf("error: insufficient budget — ...", ...))
}
// per wave, per child:
childBudget := parentBudget.Child(len(wave)) // budget.go:285 — SHARED *atomic.Int32 by pointer (SC#4)
```

**File-size discipline:** keep ≤600 LOC (CLAUDE.md NO GOD CLASS). The RESEARCH structure splits the engine (`swarm.go`), contract (`report.go`), and prompt literals (`brief.go`) — follow that `<name>_<concern>` split.

---

### `internal/swarm/report.go` (model, transform) — NEW

**Analog:** `internal/agent/tools/result.go` for the spillover plumbing (D-15 mandates the SHIPPED `tools.NewResult` — write NO second spillover mechanism).

**ChildReport struct (D-15)** — ordered by goal index, per-child summary cap only:
```go
type ChildReport struct {
    GoalIndex  int      `json:"goal_index"`
    ChildID    string   `json:"child_id"`            // deterministic "w1".."wN" by goal index (D-16)
    Status     string   `json:"status"`              // ok | failed | needs_user_input
    Summary    string   `json:"summary,omitempty"`   // per-child cap ~2-4KB
    Error      string   `json:"error,omitempty"`
    Question   string   `json:"question,omitempty"`  // needs_user_input
    Options    []string `json:"options,omitempty"`
    ToolCallID string   `json:"tool_call_id,omitempty"` // ground-truth for D-05 proxied_tool_call_id
}
```

**Spillover (the ONLY mechanism — `result.go:94`):** marshal the `[]ChildReport` to JSON, hand to `tools.NewResult(ctx, json)` — preview cap → rune-boundary truncate → sidecar + `read_tool_output` footer all handled. Reuse `AURA_CONTEXT_PREVIEW_CAP_BYTES` (`config.ToolPreviewCap`, default 2048) unless the E2E shows 2-4KB summaries truncate (RESEARCH Pitfall 5 — overflow is lossless regardless, only the inline preview shrinks).

**Per-child transcript dump (D-18)** is a SEPARATE direct write to `$AURA_RUN_DIR/<conv>/swarm/<w_i>.jsonl` via `Event.MarshalJSON` — NOT the tool-spillover sidecar (Pitfall 4: `validateID` `result.go:45-58` rejects `/` in session_id, so worker `SessionID` must be flat `<conv>-swarm-w<i>`).

---

### `internal/swarm/brief.go` (utility, transform) — NEW

**Analog:** `internal/agent/llm_agent_finalize.go` — the load-bearing-literal constant (`finalizeNudge`) + its asserting test (`llm_agent_finalize_internal_test.go:92`). D-24 + D-06/D-07 need the same shape.

**The load-bearing-literal + assert pattern (`llm_agent_finalize_internal_test.go:91-94`):**
```go
last := rc.last.Messages[len(rc.last.Messages)-1]
if last.Role != llm.RoleUser || last.Content != finalizeNudge {
    t.Errorf("finalize request tail = {%v, %q}, want the user-role finalizeNudge", last.Role, last.Content)
}
```
For D-24, the `swarm_spawn` Description's anti-over-spawn phrases (≥2 independent self-contained subtasks; simple single task = answer directly; each goal a complete brief; worker cannot see the conversation) get a `strings.Contains` assert in `swarm_spawn_test.go` — same substring-assert idiom as `recoveryNudgeToolPrefix`/`web_fetch` in `llm_agent_finalize_internal_test.go:111-115`.

**D-07 structured 4-part brief** rides in `UserTurns` (messages[1]), NEVER messages[0] (D-06 byte-stability — see worker construction below). `brief.go` holds the worker-overlay constant + the 4-part brief builder (objective / output format / tool guidance / task boundaries).

---

### `internal/agent/tools/swarm_spawn.go` (tool, deferred) — NEW

**Analog:** `internal/agent/tools/web_search.go` — the cleanest `Deferred: true` adapter (thin tool delegating to an engine, mapping failure to inline `error:` via `NewResult` so the model self-corrects).

**Spec shape (`web_search.go:42-69`):**
```go
func (e *WebSearch) Spec() Spec {
    params := json.RawMessage(`{ "type":"object", "properties": { ... }, "required":["query"] }`)
    return Spec{
        Name:        "web_search",
        Summary:     "Search the public web via the configured SearXNG instance.",
        Description: "Search the public web ... " + "Example: {\"query\":\"...\"}.",
        Parameters:  params,
        Deferred:    true,
    }
}
```
For `swarm_spawn`: `Parameters` = `{goals:[...]}` ONLY (D-03 no `tier`); `Description` carries the D-24 anti-over-spawn literal (BM25-discoverable — `bm25.go` indexes name+description+arg fields).

**Execute → inline-error contract (`web_search.go:71+` / `bridge.go:48-60`):** a domain failure (budget guard D-09, goals cap D-13) returns `tools.NewResult(ctx, "error: ...")` — a model-readable string, NOT a Go error; only a missing tool-call context propagates as a real Go error. `swarm_spawn.Execute` constructs the ephemeral `internal/swarm` runner (D-16), drains, marshals reports, returns `NewResult`.

**Worker construction inside Execute (D-06/07/08 — `llm_agent.go:63-103`):**
```go
worker := agent.NewLlmAgent(agent.LlmAgentConfig{
    Client:     parentClient,
    LLM:        parentCfg,                                   // D-03: all tiers = AURA_LLM_MODEL
    Registry:   workerRegistry,                             // parent registry MINUS swarm_spawn (D-08/D-10) — NEEDS a clone helper
    PreviewCap: cfg.ToolPreviewCap,
    RunDir:     cfg.RunDir,
    SessionID:  fmt.Sprintf("%s-swarm-w%d", convID, i+1),   // FLAT — no slash (Pitfall 4, result.go:45)
    UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}}, // D-07
})
// messages[0] = systemMessage() appended by NewLlmAgent (llm_agent.go:80) — byte-stable across all workers (D-06)
```
**FLAG (RESEARCH OQ2):** `tools.Registry` (`spec.go:63`) has NO `Clone`/`Without` method. The planner must add a small helper (`func (r *Registry) Without(names ...string) *Registry`, or build it in `internal/swarm`) using `Registry.All()` (`spec.go:80`). Plan this as an explicit task.

**FLAG (RESEARCH OQ3/A2):** `NewLlmAgent` has no system-overlay hook (`llm_agent.go:80`). D-06's "static worker overlay" should ride in the D-07 user brief (messages[1]) — preserves byte-stable messages[0]. The alternative (a new `LlmAgentConfig.SystemOverlay` field) busts cache and is discouraged.

---

### `internal/agent/tools/ask_user.go` (tool, request-response) — EDIT

**Analog:** its own current `Spec()`/`askUserArgs`/`ErrAwaitingUserInput` shape (D-05 adds optional fields).

**Add to `askUserArgs` (`ask_user.go:80-85`) + the Spec params (`ask_user.go:88-97`):**
```go
type askUserArgs struct {
    Question string   `json:"question"`
    Options  []Option `json:"options"`
    Kind     string   `json:"kind"`
    Priority *int     `json:"priority"`
    ProxiedFromChildID string `json:"proxied_from_child_id"` // D-05 optional
    ProxiedToolCallID  string `json:"proxied_tool_call_id"`  // D-05 optional
}
```
Carry both onto `ErrAwaitingUserInput` (`ask_user.go:67-73`) → the agent stamps them into the `Actions.AwaitingInput` Event (`event.go:81-88`). Best-effort, model-discretionary — do NOT make them required (the existing `required: ["question","kind"]` stays).

---

### `internal/askuser/store.go` + `internal/runner/runner_persist.go` (service, CRUD) — EDIT (D-05 3-layer plumb)

**Analog:** its own `InsertParams`/`Insert` — the sqlc columns EXIST (`paused_states.sql.go:88-89` `ProxiedFromChildID`/`ProxiedToolCallID`) but the domain layer DROPS them (RESEARCH Pitfall 3 — this is real work, not a one-line populate).

**Layer 2 — `InsertParams` (`store.go:86-95`) + `Insert` (`store.go:108-117`):**
```go
type InsertParams struct {
    // ... existing fields ...
    ProxiedFromChildID *string // D-05 — nil for direct calls (NULL)
    ProxiedToolCallID  string
}
// in Insert, after parsing, set on sqlc.InsertPausedStateParams:
arg.ProxiedFromChildID = ... // pgtype conversion at the boundary, like parseUUID (store.go:334)
arg.ProxiedToolCallID  = p.ProxiedToolCallID
```
Current `Insert` (`store.go:108-117`) builds `sqlc.InsertPausedStateParams` WITHOUT the two proxied fields — add them. The existing doc-comment at `store.go:98` ("proxied_* stay NULL for direct calls ... Phase 9 populates") is the signpost.

**Layer 3 — `persistPause` (`runner_persist.go:140-148`):** read `ai.ProxiedFromChildID`/`ai.ProxiedToolCallID` off the `*agent.AwaitingInput` and pass into `InsertParams`. The Runner is the SOLE `paused_states` writer (`runner_persist.go:120` doc) — no other site writes proxied.

---

### `internal/agent/mcptools/bridge.go` + `mount.go` (middleware) — EDIT (D-20)

**Analog:** its own `Bridge`/`bridgedTool`/`Mount` (first production exercise of the 8.1 seam).

**Flip @ `bridge.go:88`:** `Deferred: false` → `Deferred: true`. ALSO update the justifying doc-comment at `bridge.go:62-68` (it currently *argues* non-deferred — "the model MUST see that schema"; the spike found 16+12 non-deferred tools flood the manifest into the 30-50-tool degradation zone; BM25 `tool_search` discovers deferred ones).

**Allowlist param (Discretion — signature shape is the planner's call):** thread an optional `allow []string` (or `map[string]struct{}`) through `Mount` (`mount.go:16` `MountServer` → `mount.go:21` `Mount` → `bridge.go:69` `Bridge`). Filter `defs` in `Bridge` (`bridge.go:75`) before adapting — drop any tool not in the allowlist (mail footguns `delete_mailbox`/`move_message`/`create_mailbox`; spike-001 census). Empty/nil allowlist = mount all (back-compat for calculator). Extend `mount_test.go` per RESEARCH SC test map (D-20).

**FLAG (RESEARCH Pitfall 6):** after the flip, keep `buildBaseRegistry`'s non-deferred built-ins (`main.go:85-93`) + its `reg.Validate()` (`main.go:97`) running BEFORE MCP mounts, so a registry where every MCP server is dropped still passes `ErrNoNonDeferredTool` (`spec.go:94`).

---

### `cmd/aura/main.go buildRegistryWithMCP` + `cmd/aura/chat.go bootChat` (config) — EDIT (D-21 fail-soft)

**Analog:** its own boot loop. **Current fail-HARD (`main.go:120-127`):**
```go
for _, name := range serverNames {
    closer, _, err := mcptools.MountServer(ctx, reg, name, cfg.MCPServers[name])
    if err != nil {
        _ = closeMCPServers(closers)
        return nil, nil, err   // ← THE BUG: one dead server kills the whole boot
    }
    closers = append(closers, closer)
}
```
**Phase 9 → per-server WARN-and-drop (D-21):** on `MountServer` error, `slog.Warn("mcp mount failed", "server", name, "err", err)` and `continue` — do NOT abort, do NOT close the already-mounted servers. The `bootChat` exit path (`chat.go:139-144` — `buildRegistryWithMCP` err → `pool.Close()` + `os.Exit(1)`) then only fires on a TRULY fatal error (e.g. `cfg.MCPServersErr` config-parse). Use `log/slog` (D-17 convention). EXPLICIT non-goals (D-21): no ping ticker, no restart supervisor, no lazy mount, no OpenClaw machinery. Unit test: `TestBuildRegistryFailSoft` (a bad entry must NOT abort boot).

---

### `cmd/aura/mcp.go mcpRecipes` (config, CRUD) — EDIT (D-19)

**Analog:** the `calculator` recipe (`mcp.go:24-40`) pointing at the user's fork `chetto1983/calculator-mcp-server`. Add `mail` + `whatsapp` entries pointing at the validated forks:
```go
var mcpRecipes = map[string]mcpRecipe{
    "calculator": { /* existing */ },
    "mail": {
        Summary: "martinzarfl/mail-mcp over stdio (SMTP/IMAP env config)",
        Server:  mcp.ManagedServer{ Command: "...", Args: []string{...}, Env: []string{...}, Source: "recipe:mail" },
    },
    "whatsapp": {
        Summary: "chetto1983/whatsapp-mcp (whatsmeow bridge in WSL, stdio via wsl.exe)",
        Server:  mcp.ManagedServer{ Command: "wsl.exe", Args: []string{...}, Source: "recipe:whatsapp" },
    },
}
```
Same trust pattern as `recipe:calculator → chetto1983/*`. Canonical whatsapp source = the user's fork commit `6de1dcd`. Creds live in the managed-config `Env` (`--env KEY=VALUE`, `mcp.go:106`), NOT git. NO new `AURA_MCP_*_SERVER` boot env vars (D-21 supersedes D-23's env-add list — RESEARCH OQ1; surface to user in the Wave-0 amendment).

---

### `cmd/aura/main.go aura swarm-demo` (route, optional) — NEW

**Analog:** `cmd/aura/agent.go` `aura agent dry-run` (the SC#4 operator-proof precedent, 02-07). Mirror its shape (`agent.go:1-2,46-63`):
```go
// dispatcher in main.go's switch, mirroring db.go/neo4j.go
func runSwarm(args []string) {
    if len(args) < 1 { fmt.Fprintln(os.Stderr, "usage: aura swarm-demo"); os.Exit(2) }
    // ... flag.NewFlagSet, build a mock-LLM fixture (agenttest.FakeClient), run the swarm, print reports
}
```
Use `agenttest.FakeClient` / `InfiniteToolCallAgent` (`agent.go:100`) for a deterministic mock-LLM fixture — same as `dryRun` builds a `workflow.NewLoop` over a fake sub-agent. Optional per CONTEXT (Integration Points).

---

## Shared Patterns

### Deferred-tool pattern
**Source:** `internal/agent/tools/spec.go:30-37` (Spec.Deferred) + `web_search.go:67` (`Deferred: true` exemplar) + the package doc `spec.go:1-11`.
**Apply to:** `swarm_spawn.go` (Deferred:true), `bridge.go:88` flip (Deferred:false→true).
```go
type Spec struct {
    Name, Summary, Description string
    Parameters json.RawMessage
    Deferred   bool // true → full spec hidden until tool_search loads it
}
```
The non-deferred guard (`spec.go:94` `Validate` → `ErrNoNonDeferredTool`) must stay green after the bridge flip (Pitfall 6).

### tools.NewResult spillover (the ONLY spillover mechanism)
**Source:** `internal/agent/tools/result.go:94-133` + `WithToolCallContext` `result.go:26-33`.
**Apply to:** the swarm report (D-15), every bridged MCP tool result (`bridge.go:57-59`).
```go
return tools.NewResult(ctx, content) // cap → rune-truncate → sidecar + read_tool_output footer; degrade-clean on write fail
```
NEVER hand-roll byte-slicing + file write (RESEARCH Don't Hand-Roll).

### Budget tree (shared *atomic.Int32)
**Source:** `internal/agent/budget.go:285-299` (`Child`) + `:238-244` (`Remaining`) + the snapshot requirement `:283-284`.
**Apply to:** the swarm fan-out (D-09 pre-flight + per-child fork; SC#4 tree-total bound).
Snapshot `Remaining()` ONCE before the loop for equal sibling shares; `Child(fanout)` shares the step counter by pointer (depth-3 fan-3 ≤ max_steps proven Phase 2).

### slog structured logging
**Source:** `log/slog` stdlib (D-17). The fail-soft WARN (`main.go` D-21) and the 3 per-child lifecycle lines (`child.spawned{w_i,goal}` / `child.completed{w_i,status,dur}` / `child.failed{w_i,error}`).
**Apply to:** `internal/swarm/swarm.go`, `cmd/aura/main.go` fail-soft.

### goleak + race test setup
**Source:** `internal/agent/workflow/workflow_test.go:17-18` (`goleak.VerifyTestMain(m)` — "Copied verbatim from internal/db/db_test.go:26-28") + per-test `defer goleak.VerifyNone(t)` (`parallel_test.go:139` etc.).
**Apply to:** `internal/swarm/` (new `TestMain` mirroring the workflow one — RESEARCH Wave-0 gap) + every swarm test (D-25 goleak-clean).
```go
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
```
W8 note (`workflow_test.go:13-15`): use the Budget injectable fake clock, NOT Go 1.26 synctest (it spawns goroutines that trip goleak).

### rapid property tests
**Source:** `pgregory.net/rapid` (Phase 2 direct dep, RESEARCH Standard Stack). Properties for D-25: report length+order == goals; total tree steps ≤ parent remaining at spawn; goleak-clean after return; per-child isolation (a failed/timed-out child never affects sibling entries).
**Apply to:** `internal/swarm/swarm_property_test.go`.

### Load-bearing literal + asserting test
**Source:** `internal/agent/llm_agent_finalize.go` (`finalizeNudge` const) + `llm_agent_finalize_internal_test.go:91-94,111-115` (exact-match + `strings.Contains` asserts).
**Apply to:** D-24 `swarm_spawn` Description anti-over-spawn phrases (`swarm_spawn_test.go`).

### Env var naming + loading
**Source:** `internal/config/config.go:154,158-170` (`envIntDefault("AURA_<DOMAIN>_<UNIT>", default)`).
**Apply to:** NEW `AURA_SWARM_MAX_GOALS` (default 8, D-13), `AURA_SWARM_CHILD_TIMEOUT_SEC` (default ~120, D-11). `AURA_SWARM_MAX_CONCURRENT` already exists (PRD line 4766). Reuse `AURA_CONTEXT_PREVIEW_CAP_BYTES` (`config.go:154`) for the per-child summary cap unless E2E shows it too tight (D-15 Discretion).
```go
MaxSwarmGoals: envIntDefault("AURA_SWARM_MAX_GOALS", 8),
SwarmChildTimeoutSec: envIntDefault("AURA_SWARM_CHILD_TIMEOUT_SEC", 120),
```

### Live E2E (cot_eval build tag, operator-run, NOT CI)
**Source:** `internal/eval/harness_cot_eval_test.go:1-19` (build tag + OPENROUTER gating + the one legitimate skip) + `dataset_cot_eval.go:1-54` (scenario struct + dimension consts).
**Apply to:** the swarm E2E scenario + judge rubric (D-22, SC#5).
```go
//go:build cot_eval
// ... t.Skip if OPENROUTER_API_KEY unset (the ONE legitimate skip — locally only)
```
Add a swarm scenario to `dataset_cot_eval.go`'s `scenario` shape (natural prompt, NO "swarm" word; expected facts; mail/WhatsApp read-back via MCP; timing < 1.5× single-worker) + judge rubric ≥90% (autonomous parallelization, sub-answer correctness, aggregation, no over-spawn on a control). Numbers → `docs/aura-quality-snapshot.md`. Invocation: `set -a; . ./.env; set +a; export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/`.

### Subcommand dispatcher
**Source:** `cmd/aura/agent.go:1-2,46-63` (`runAgent` → `aura agent dry-run`, "mirroring db.go/neo4j.go"). The composition root `buildRegistryWithMCP` (`main.go:104`) + `buildBaseRegistry` (`main.go:83`) are where `swarm_spawn` registers into the PARENT registry only (workers get the clone-minus-swarm_spawn).

---

## No Analog Found

None. Every file has a direct in-repo analog (this phase is ~90% composition over shipped, tested code).

---

## Metadata

**Analog search scope:** `internal/agent/workflow/`, `internal/agent/tools/`, `internal/agent/mcptools/`, `internal/agent/` (llm_agent, budget, event, finalize), `internal/askuser/`, `internal/runner/`, `internal/eval/`, `internal/config/`, `cmd/aura/`.
**Files scanned (read this session):** parallel.go, spec.go, ask_user.go, result.go, sandbox_exec.go, web_search.go, bridge.go, mount.go, mcp.go, main.go, chat.go, store.go, budget.go, llm_agent.go, agent.go, llm_agent_finalize_internal_test.go, workflow_test.go, harness_cot_eval_test.go, dataset_cot_eval.go, config.go (+ Glob/Grep across tools, eval, workflow, cmd/aura).
**Pattern extraction date:** 2026-06-04
**Cross-check:** every line reference here matches 09-RESEARCH.md's verified file:line evidence; the 3 RESEARCH refinements (main.go abort @121-125 not 122-124; bridge.go:62-68 doc-comment must also flip; proxied_* is a 3-layer plumb not a populate) are reflected in the EDIT excerpts above.
