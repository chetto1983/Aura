# Phase 18: Slice 7e executable snippet reuse — steady-state artifact runs sotto i 40s - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 10 (9 modify / commit-as-is + 1 new test surface)
**Analogs found:** 10 / 10 (this is a WIRING phase — every analog is an already-shipped sibling on the SAME file, not a distant cousin)

> **Posture note (D-01):** the host-primary decision flips the snippet `use` frame from `sandbox_exec` to host `shell_exec` by-path. The analog for the new host frame is `shell_exec.go` (the keystone host tool) + the instruction-skill `useAuthorityFrame` pattern. The analog for the snippet-save action is the create/update `writeAction` flow in `skill_write.go`, but **D-02 makes it UNGATED** — so it does NOT copy the `*ErrAwaitingUserInput` pause tail; it copies the validate→Writer-call→NewResult shape instead.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agent/tools/skill.go` | tool (router) | request-response | itself (the `notYetWired` reserved keys) + `action.go` | exact (same file) |
| `internal/agent/tools/skill_read.go` | tool (action handler) | transform | itself (`renderSnippetUse`) + `shell_exec.go` (host frame) | exact (same file) |
| `internal/agent/tools/skill_write.go` | tool (action handler) | request-response | itself (`writeAction` create/update) | exact (same file) — but UNGATED (D-02) |
| `internal/skills/snippet.go` | service | file-I/O | itself (`SaveSnippet`/`UseSnippet`/`renderSnippetDocs`) | exact (same file) |
| `internal/skills/snippet_usage.go` | service | file-I/O | itself (`SetUsageStatus`) + `writer_activate.go` (`Archive`/`Materialize`) | exact (same file) |
| `internal/skills/writer_activate.go` | service | file-I/O + CRUD (audit tx) | `Archive` (the inverse of `restore`) + `Activate` (`Materialize`) | exact (same file) |
| `cmd/aura/serve_adapters.go` | provider (adapter) | request-response | itself (`skillLoaderAdapter.Snippet`, `skillWriterAdapter.WriteMutation`) | exact (same file) |
| `internal/toolinvocations/store.go` (+ migration 0011) | store | event-driven (append-only) | itself + `runner_persist.go` wiring | exact — COMMIT as-is |
| `internal/eval/scenarios_skills.go` + `skills_cot_eval_test.go` | test (E2E gate) | request-response | itself (the xlsx North-Star gate) | exact (same file) — extend for ledger window |
| `internal/agent/tools/skill_test.go` + `skill_read_test.go` | test | — | `snippet_test.go` `TestUseSnippetReturnsPath` (load-bearing literal) | role-match (NEW tests) |

---

## Pattern Assignments

### `internal/agent/tools/skill.go` (tool router — wire restore/archive, maybe snippet-save)

**Analog:** itself — the `notYetWired` reserved keys (lines 153-182) and the generic `action.go` `ActionRouter`.

**Router fill-in pattern** (lines 162-182) — replace `notYetWired("restore")`/`notYetWired("archive")` with real handler method values, exactly like the create/update/delete keys already added:
```go
t.router = NewActionRouter(map[string]ActionFunc{
    "list":   t.actionList,
    "info":   t.actionInfo,
    "use":    t.actionUse,
    "create": t.actionCreate,
    "update": t.actionUpdate,
    "delete": t.actionDelete,
    "restore": notYetWired("restore"),  // ← replace with t.actionRestore
    "archive": notYetWired("archive"),  // ← replace with t.actionArchive
    // (D-02) maybe add: "save_snippet": t.actionSaveSnippet  — confirm action name in plan
})
```

**Schema enum note** (lines 93-104): the `action` property enum already lists `restore`/`archive` (downstream-stable by design — D-01). When wiring them, update only the `action` description string (drop "not yet available"). The schema is OpenAI-wire-safe: a **property-level** enum on a string is fine, but there is intentionally NO root-level `oneOf/anyOf/enum` (a root enum 400s DeepSeek). If a snippet-save action lands, ADD `"save_snippet"` to the enum + a per-field description for `language`/`code` — never a root oneOf.

**Reserved-key handler signature** (lines 156-160): `notYetWired` returns an `ActionFunc`; the new handlers are methods `func (t *SkillTool) actionRestore(ctx, raw) (ToolResult, error)`, placed in `skill_write.go` (the write-action file) or a new `skill_lifecycle.go` if `skill_write.go` approaches 600 LOC (it is currently 148 LOC — room to add there).

---

### `internal/agent/tools/skill_read.go` (action=use snippet frame — POSTURE, load-bearing literal)

**Analog:** itself — `renderSnippetUse` (lines 115-129) + `useAuthorityFrame` (line 14); the host invocation shape comes from `shell_exec.go`.

**Current sandbox frame** (lines 119-128) — the literal that D-01 flips to host:
```go
func renderSnippetUse(instructions, sandboxPath, interpreter string) string {
    var b strings.Builder
    b.WriteString(useAuthorityFrame)
    if instructions != "" { b.WriteString(instructions); b.WriteString("\n") }
    fmt.Fprintf(&b, "Run this snippet by path: call sandbox_exec with command=%q and args=[%q] (add further args as needed). Do NOT execute it as %s directly — always invoke the interpreter with the path.\n",
        interpreter, sandboxPath, sandboxPath)
    return b.String()
}
```

**Host-posture rewrite (D-01):** hand the model a `shell_exec` command line, not a `sandbox_exec` argv. The host command shape mirrors what the model already knows from `shell_exec.go`'s Description ("`python3 script.py`", "any installed interpreter just works"). Suggested frame: `Run this stored snippet by path with shell_exec: command="<interpreter> <hostPath> <args...>"`. Keep `sandbox_exec` available as the explicit untrusted-code escalation the prompt teaches.

**Load-bearing literal discipline** (lines 10-14): `renderSnippetUse`/`useAuthorityFrame` are **contract literals with asserting tests** (`snippet_test.go:TestUseSnippetReturnsPath`, `skill_read_test.go`). Changing the frame REQUIRES updating those tests in the same commit (CLAUDE.md "comments-updated in the SAME commit" + RESEARCH Wave-0 gap).

**The seam carries the path:** `actionUse` (lines 93-113) calls `t.Loader.Snippet(name)` which returns `(instructions, sandboxPath, interpreter, ok)`. For host posture, the SEAM must emit a HOST path (under `AURA_SKILL_EXPORT_DIR`), not the `/skills` mount path. Two options per RESEARCH Runtime State Inventory: (a) widen the seam to return a host path, or (b) compute the host path fresh at read time in the adapter. Prefer (b) — `snippet.go` already has the building blocks (`SnippetCodeFile`, `AURA_SKILL_EXPORT_DIR` via the loader config).

---

### `internal/agent/tools/skill_write.go` (snippet-save action — UNGATED, D-02)

**Analog:** itself — the `writeAction` create/update/delete flow (lines 86-138). **CRITICAL DIFFERENCE: D-02 makes snippet-save UNGATED** (Claude-Code parity, no ask_user ceremony), so it copies the validate→writer-call shape but NOT the pause tail.

**What to COPY from `writeAction`** (the seam + decode + writer dispatch, lines 93-111):
```go
var a skillWriteArgs            // (extend with Language string `json:"language"`, Code string `json:"code"`)
if err := json.Unmarshal(raw, &a); err != nil { ... }
if a.Name == "" { return ToolResult{}, fmt.Errorf("skill save_snippet: name is required") }
if t.Writer == nil { return ToolResult{}, fmt.Errorf("skill save_snippet: no writer is wired in this context") }
status, err := t.Writer.SaveSnippet(ctx, a.Name, a.Language, a.Code, a.Description /*, needs flags */)
if err != nil { return ToolResult{}, fmt.Errorf("skill save_snippet %q: %w", a.Name, err) }
```

**What to DROP from `writeAction` (D-02 divergence):** do NOT return `&ErrAwaitingUserInput{...}` (lines 133-137). The create/update/delete path PAUSES for human approval; snippet-save does NOT. Instead return a `NewResult(ctx, ...)` confirming the snippet was saved (and where), exactly like the read actions return results. This is the no-ceremony directive (2026-06-05) — future gating lands with capability_grants (Slice 1.7), NOT here.

**Anti-pattern guard (RESEARCH Pitfall + Anti-Patterns):** ONLY `ask_user` may return the `*ErrAwaitingUserInput` sentinel (`TestAskUserOnlyPauseConstraint`). An UNGATED save action returning a normal `ToolResult` is correct and does not trip that constraint.

**New writer seam method:** add `SaveSnippet(ctx, name, language, code, description, ...) (status string, err error)` to the `skillWriter` interface (lines 36-42), mirroring `WriteMutation`'s consumer-declared-seam shape. The live `internal/skills.Writer.SaveSnippet` (already shipped) is adapted in `serve_adapters.go`.

---

### `internal/skills/snippet.go` (service — host-path derivation + docs)

**Analog:** itself — `SaveSnippet` (lines 134-184), `UseSnippet` (lines 267-294), `SnippetSandboxPath`/`SnippetInvocation` (lines 86-112).

**SaveSnippet is already model-callable** (lines 134-184): it validates the language enum + write-boundary blocklist on the CODE, computes the RISKY tier, writes pending atomically, records the D-29 pending audit tuple, NEVER self-activates. The save action's writer adapter calls this verbatim with `actor=model`. **No change to SaveSnippet itself is needed** beyond confirming the `Frontmatter`/`AuditActor` arg shape the adapter passes (see CLI `skillsSnippetSave` at `cmd/aura/skills_snippet.go:64-70` for the exact construction).

**Host-path derivation (D-01 posture):** `SnippetSandboxPath` (lines 86-95) and `inSandboxSkillsRoot = "/skills"` (line 52) hardcode the sandbox mount. For host posture, add a sibling `SnippetHostPath(name, lang, exportDir)` that joins `exportDir/<name>/<name>.<ext>` (mirror `SnippetSandboxPath`'s structure exactly), and have `UseSnippet`/`SnippetInvocation` return it when the posture is host. The export dir is `Writer.exportDir` (already a field — see `writer_activate.go:36` `Materialize(name, dstDir, w.exportDir)`).

**Docs-body posture (RESEARCH Runtime State Inventory):** `renderSnippetDocs` (lines 229-250) bakes a `sandbox_exec {command, args}` frame INTO the materialized SKILL.md body (line 233). Per RESEARCH, prefer computing the invocation frame at `use` time (in `renderSnippetUse`, the tool layer) and keep this docs body GENERIC — so existing materialized snippets don't need re-materialization on the posture flip.

---

### `internal/skills/writer_activate.go` (restore = the inverse of Archive)

**Analog:** itself — `Archive` (lines 63-84) is the EXACT inverse `restore` mirrors; `Activate` (lines 24-57) shows the `Materialize` + audit-tx shape.

**Archive (the shipped inverse, lines 63-84):** Dematerialize from export dir → `promoteDir(active → archived)` → `auditActivationLike(AuditArchive)`. `restore` reverses each step:
```go
// restore: promoteDir(archived/<name> → active/<name>) → Materialize(name, activeDir, exportDir)
//          → SetUsageStatus(name, "active") → auditActivationLike(AuditActivate, ..., ApprovalCLI)
//   NOTE: restore audits as AuditActivate (NOT a new AuditRestore) — the 0010 action CHECK
//         does not list 'restore'; an AuditRestore constant would fail the live CHECK.
```

**Materialize call site** (lines 36 + 179): `Materialize(name, dstDir, w.exportDir)` re-projects the active skill into the export dir — restore calls this after promoting archived→active. **`SetUsageStatus("active")`** (snippet_usage.go:110-117) is the sidecar-status inverse of the sweep's `SetUsageStatus(name, "archived")` (snippet_usage.go:168).

**Audit tuple pattern** (lines 136-152): `auditActivationLike` records a `gate_taken=true` row inside `db.WithTx`. NO new `AuditRestore` action constant — restore audits as the EXISTING `AuditActivate` (restore IS a re-activation) with `ApprovalCLI` source. The 0010 `action` CHECK list (`create|update|delete|install|activate|archive|auto_archive|cleanup_pending_stale`) does NOT include `'restore'`, and this phase is forbidden from adding a snippet migration (RESEARCH Open Question 6 / D-19) — so a new `AuditRestore` constant would fail the live DB CHECK. A code comment on `Restore` explains the intentional `activate`/`cli` mapping. Restore is SAFE-tiered (RESEARCH Pattern 1: "tier SAFE, no gate"), so no ask_user pause.

**archived dir guard** (line 70): `if w.archiveDir != ""` — restore must symmetrically read from `w.archiveDir` and error clearly if it's unset.

---

### `cmd/aura/serve_adapters.go` (adapters — wire save + host path + restore seam)

**Analog:** itself — `skillLoaderAdapter.Snippet` (lines 350-360) and `skillWriterAdapter.WriteMutation` (lines 293-295).

**Snippet seam adapter** (lines 350-360) — currently returns the sandbox path via `skills.SnippetInvocation`:
```go
func (a *skillLoaderAdapter) Snippet(name string) (instructions, sandboxPath, interpreter string, ok bool) {
    s, found := a.loader.Get(name)
    if !found || s.Type != skills.TypeSnippet { return "", "", "", false }
    path, interp, perr := skills.SnippetInvocation(s.Name, s.Language)  // ← host-path variant for D-01
    if perr != nil { return "", "", "", false }
    return s.Body, path, interp, true
}
```
For host posture, swap `SnippetInvocation` (sandbox path) for the new host-path resolver, OR widen the seam to return both. The adapter already has the config (it's built in `newSkillTool` with `cfg`).

**Writer-save adapter** (lines 282-295) — mirror `skillWriterAdapter.WriteMutation` for `SaveSnippet`, labeling actor `model`:
```go
func (a *skillWriterAdapter) SaveSnippet(ctx context.Context, name, language, code, description string, ...) (string, error) {
    res, err := a.w.SaveSnippet(ctx, name, language, code, skills.Frontmatter{Name: name, Description: description, ...}, skills.AuditActor{ActorID: "model"})
    if err != nil { return "", err }
    return res.Status, nil
}
```

**Production registry wiring (RESEARCH Pitfall 3 — eval↔production parity):** `newSkillTool` (lines 253-271) already wires the Writer when a pool exists. `main.go:buildBaseRegistry` (line 111) registers it. The eval registry (`skills_cot_eval_test.go:buildSeamFreeSkillsRegistry`, lines 231-244) currently registers NO skill tool — that parity gap is in-scope (see eval section below).

---

### `internal/toolinvocations/store.go` + `internal/db/migrations/0011_tool_invocations.*` (the measurement substrate — COMMIT as-is)

**Analog:** itself + `runner_persist.go` (the wiring). This is in-flight, uncommitted, and ALREADY AUTHORED — Wave-0 commits it; the gate queries it.

**Ledger query for the gate** (`store.go:67-81`):
```go
func (s *Store) ListByConversation(ctx context.Context, conversationID string) ([]Event, error)
// each Event carries: ToolName, Event ("start"|"end"), DurationMS, Seq, RequestID, Status, ...
```

**Wiring is done** (`runner_persist.go:69-101` `persistToolInvocation`): the Event-sourced seam writes one append-only fact per `ToolInvocation` start/end Event, correlated by `ConversationID`/`RequestID`/`ToolCallID`. Injected via `runner.Deps.ToolInvocations` (runner.go:44) over the `ToolInvocationStore` interface (interfaces.go:72-77).

**Append-only is enforced at the DB** (migration 0011 lines 54-71): BEFORE UPDATE/DELETE/TRUNCATE triggers RAISE EXCEPTION; `aura_app` holds SELECT+INSERT only. The gate's ledger reads are SELECT — within `aura_app`'s grant.

**Steady-state window assertion (Pattern 3 + D-03):** count distinct `RequestID` (LLM roundtrip proxy) and SUM/MAX `DurationMS` over the 2nd-run window; assert ≤ ~5 calls / < 40s. **D-03 — the ~5 target is provisional** until the Wave-0 characterization probe dumps the real per-call breakdown from a live xlsx ledger run.

---

### `internal/eval/scenarios_skills.go` + `skills_cot_eval_test.go` (E2E gate — extend for steady-state + parity)

**Analog:** itself — the xlsx North-Star gate. Two changes:

**1. Registry parity (Pitfall 3, lines 231-244):** `buildSeamFreeSkillsRegistry` registers `text_response + tool_search + read_tool_output + current_time + ask_user + web_search + web_fetch + shell_exec + fs_*` but NO `skill` tool. A snippet-REUSE scenario CANNOT resolve `skill action=use` without it. Add the production `skill` tool (live loader/writer over a temp skills root), mirroring `cmd/aura/serve_adapters.go:newSkillTool`. The comment block (lines 222-230) is the contract to update.

**2. Steady-state 2-run + ledger window (Pitfall 5 + Pattern 3):** the existing gate runs ONCE and asserts the artifact via `verifyXlsxArtifact` (artifact-not-reply, lines 288-291). The new assertion runs the scenario TWICE (or pre-seeds the snippet), and asserts the STEADY-STATE (2nd) run's ledger window (≤ ~5 distinct request_ids, < 40s). **Keep `verifyXlsxArtifact` discipline** (the `.xlsx` must exist/open/contain today's date) — never assert on `r.Reply`. The freshness windowing (`runStart := time.Now()`, line 271 + mtime≥run-start) is the discipline to reuse for the steady-state window.

**Artifact-not-reply mandate** (CONTEXT D-03 discretion + `enforceSkills` lines 414-440): every hard-floor assertion reads structured ground truth (`classifyCall` over `Function.Arguments`, the `.xlsx` read-back, the ledger) — never the model's prose.

---

### `internal/agent/tools/skill_test.go` + `skill_read_test.go` (NEW + UPDATED tests — Wave 0 gaps)

**Analog:** `internal/skills/snippet_test.go:TestUseSnippetReturnsPath` (lines 138-178) — the load-bearing-literal assertion shape.

**Load-bearing literal test pattern** (snippet_test.go:156-168):
```go
use, err := w.UseSnippet("calc")
if use.SandboxPath != "/skills/calc/calc.py" { ... }  // ← updates to the HOST path under D-01
if use.Interpreter != "python3" { ... }
if use.Instructions != "run it by path" { ... }
```
When the `use` frame flips to host (D-01), THIS literal and the `skill_read_test.go` `renderSnippetUse` assertion update in the SAME commit.

**New handler tests (RESEARCH Test Map):**
- `TestActionRestore` — `action=restore` re-materializes + SetUsageStatus("active") + audits as `activate`/`cli` (NOT a 'restore' action — see writer_activate.go note) (unit + db_integration)
- `TestActionArchive` — `action=archive` (SAFE, no gate) de-materializes + audit (unit + db_integration)
- `TestSnippetSaveAction` — `action=save_snippet` → Writer.SaveSnippet → pending, returns a NORMAL ToolResult (NOT a pause — assert it does NOT return `*ErrAwaitingUserInput`, the D-02 inverse of `writeAction`)

Mirror the table-driven + `goleak` discipline already in the package; no test asilo nido (realistic fixtures, real Writer over a temp root).

---

## Shared Patterns

### Consumer-declared seam (interface segregation — keep tools free of internal/skills)
**Source:** `internal/agent/tools/skill.go:59-72` (`skillLoader`) + `skill_write.go:36-42` (`skillWriter`); adapted in `cmd/aura/serve_adapters.go:229-360`.
**Apply to:** the new snippet-save seam + the host-path Snippet seam.
The tools package NEVER imports `internal/skills` concretely — it declares a narrow interface and the live `*skills.Writer`/`*skills.Loader` is adapted at the composition root (`cmd/aura`). Every new tool↔skills capability follows this: declare the method on `skillWriter`/`skillLoader`, implement the adapter in `serve_adapters.go`.

### ActionRouter dispatch (one tool, N actions, one wire-safe schema)
**Source:** `internal/agent/tools/action.go:30-58` + `skill.go:162-182`.
**Apply to:** wiring restore/archive/save_snippet.
Add the action→method to the router map; add the action to the property-level `action` enum + a per-field description; NEVER a root-level oneOf/anyOf (DeepSeek 400s). Unknown actions return a structured error naming valid actions (never a panic).

### Audit-in-tx (every snippet lifecycle mutation is audited)
**Source:** `internal/skills/writer_activate.go:138-152` (`auditActivationLike`) + `snippet.go:162-175` (SaveSnippet's D-29 pending tuple).
**Apply to:** restore (audits as `AuditActivate`/`cli` — NO new constant, the 0010 CHECK has no 'restore'), the save action (reuses SaveSnippet's existing audit).
Every FS mutation records an `aura.skill_audit` row inside `db.WithTx`; the D-29 tuple shape is implied by `ApprovalSource` (auto vs cli vs ask_user). FS promotion + materialize happen BEFORE the tx (reconcilable by boot scan); the tx is the audit INSERT.

### Atomic FS write (temp + rename, crash-safe)
**Source:** `internal/skills/snippet.go:189-223` (`writePendingSnippet`) + `snippet_usage.go:56-88` (`writeUsageAtomic`) + `writer_activate.go:191-202` (`promoteDir`).
**Apply to:** restore's archived→active promotion (reuse `promoteDir`).
Sibling temp dir/file + rename; a crash mid-write never leaves a half-written artifact the loader/sweep reads.

### Append-only ledger as the measurement ground truth (no r.Reply)
**Source:** `internal/toolinvocations/store.go` + `runner_persist.go:69-101` + migration 0011.
**Apply to:** the E2E steady-state gate.
The ledger is the durable, queryable, correlated source the `<40s / ≤~5-call` acceptance reads. DB triggers enforce append-only; `aura_app` cannot mutate it. The gate reads `ListByConversation` and windows on distinct `RequestID` + `DurationMS`.

### Eval↔production registry parity (no eval-only seams)
**Source:** `internal/eval/skills_cot_eval_test.go:19-24` (the WHY) + `buildSeamFreeSkillsRegistry` (lines 231-244); production `cmd/aura/main.go:buildBaseRegistry` (lines 99-141).
**Apply to:** the snippet-reuse eval scenario (must register the `skill` tool + the locked execution surface).
The eval registry MUST mirror what ships (#52/#53 rule 4 + the 2026-06-06 directive). A snippet-reuse scenario that can't resolve `skill action=use` is a parity bug.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every target has an exact in-repo analog — this is a pure WIRING phase. The closest thing to "new" is `SnippetHostPath` (a 1:1 mirror of the shipped `SnippetSandboxPath`). There is NO new audit-action constant — restore reuses the shipped `AuditActivate` (the 0010 CHECK has no 'restore'). No greenfield, no `executor.go`, no new migration (RESEARCH Anti-Patterns + Open Question 6). |

> **Forbidden by D-04/amendment #44 — do NOT create:** `internal/skills/executor.go`, `sandbox.Runner.Execute`, `internal/skills/pattern_analyzer.go`, `0011_snippet_runs` table, Neo4j `:UserSnippet` mirror, an `AuditRestore` action constant (the 0010 action CHECK does not list 'restore'). Execution is ALWAYS by-path through `shell_exec`/`sandbox_exec`; snippet-run forensics ride the `tool_invocations` ledger.

---

## Metadata

**Analog search scope:** `internal/agent/tools/`, `internal/skills/`, `internal/cron/handlers/`, `internal/toolinvocations/`, `internal/runner/`, `internal/eval/`, `cmd/aura/`, `internal/config/`, `internal/db/migrations/`
**Files scanned:** 18 read in full or targeted; the 10 load-bearing analogs read completely
**Tree state:** mapped AS-IS ON DISK — `internal/toolinvocations/`, migration `0011_tool_invocations.*`, `runner_persist.go` wiring, `cmd/aura/shell.go` are UNCOMMITTED in-flight work and were read in their current on-disk form
**Pattern extraction date:** 2026-06-06
</content>
</invoke>
