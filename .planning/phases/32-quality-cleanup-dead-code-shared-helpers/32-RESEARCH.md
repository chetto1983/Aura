# Phase 32: Quality Cleanup — Dead Code + Shared Helpers - Research

**Researched:** 2026-06-29
**Domain:** Go (sqlc/pgx/Neo4j) + Vite/React maintainability cleanup — dead-code triage, shared-helper extraction, parity testing
**Confidence:** HIGH (every claim is grounded in a direct read/grep of the current tree at the cited file:line)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 — Triage, not blind deletion.** Every flagged "dead" symbol is triaged first: *genuinely dead* → delete; *intended-but-unwired* → the bug is the **missing wiring**, not the code. A deadcode/knip flag is a question, not a verdict.
- **D-02 — Wire intended-but-unwired symbols here.** When triage finds a symbol is not dead, just never connected to its intended consumer, **wire it in this phase**. **Guardrail:** wiring is bounded to connecting the *already-existing* flagged symbol to its intended consumer. If a single wiring turns non-trivial (new feature behavior, a migration, a cross-phase surface) → **escalate to the operator**, do not expand scope. Likely bite: `assets.Status*` and possibly the `AURA_MEMORY_EMBED_*` keys.
- **D-03 — Reach = audit's named items + refactor-on-touch; no repo-wide sweep.** Operate on the audit's enumerated items; within any file already touched, also fold dead code / dup that `deadcode`/`knip`/`golangci-lint` flag. Do **not** run a whole-tree removal sweep beyond the audit list.
- **D-04 — Case-by-case on exports.** Unexported confirmed-dead symbols are deleted freely. Each **exported** confirmed-dead symbol is surfaced to the operator for a keep/kill call before removal. "Confirmed" = `deadcode` **and** `knip` **and** a repo-wide `rg` (including `_test.go` and build-tagged files) all agree.
- **D-05 — Remove-from-Go + document when sidecar-owned.** If triage confirms `AURA_MEMORY_EMBED_*` are sidecar-owned (read from compose env, nothing in-process reads them), **remove them from the Go settings struct** and document that the agent-memory sidecar owns them. If instead a Go→sidecar wiring gap, wire per D-02.
- **D-06 — Extraction depth decided per-package by Claude.** `neostore` likely **canonical**; `envutil` and `agentrender` likely **minimal**.
- **D-07 — Target packages/primitives:** `internal/neostore` ← `hashText`/`asString`/`asFloats`/`GraphClient`/`numericFromFloat`; `internal/envutil` ← the 3 env-helper copies + adopt for agent-tool knobs; `internal/agentrender` ← `chat_render`↔`eval` ~80-LOC set; agent: one `CanonicalArgs` + one `isTransientNetworkErr`; web: single `getJSON` + reuse shared `focusTrap.ts`.
- **D-08 — Fold in QA-D-08 (frontend skeleton unification).** Pick one skeleton/loading system and unify the duplicates during the web dedup pass. Explicit scope addition.
- **D-09 — Characterization/golden parity per extraction.** Each extraction gets a table test that feeds the extracted helper the **union of inputs the old copies handled** and asserts identical output.
- **D-10 — Test-first sequencing.** For each extraction: write the parity test against the **current duplicated code first** (confirm green) → extract → confirm still green. Dead-code triage/deletions run first as the clean-slate wave.
- **D-11 — Per-item atomic commits.** One commit per deletion / wiring / extraction / test-gap.
- **D-12 — Add the `memory_integration` CI matrix leg.** Wire it so memory-tagged tests run in CI. If inspection shows memory tests already run with no real gap, document that instead.
- **D-13 — New packages each ≥85%, registered in the gate.** Add `internal/neostore`, `internal/envutil`, `internal/agentrender` to `scripts/coverage_gate.sh` owned-surface; each must independently clear ≥85%.
- **D-14 — Sequential, no worktrees.** Run waves/plans sequentially without git worktrees. (Overrides `config.use_worktrees`/`parallelization` for this phase.)

### Claude's Discretion
Per-symbol dead-vs-unwired triage verdicts (D-01); per-package extraction depth (D-06); remove-vs-wire-vs-document for `AURA_MEMORY_EMBED_*` once in-process readers are checked (D-05); wave/plan ordering within the test-first principle (D-10). Escalate to the operator only when a wiring (D-02) proves non-trivial or an exported deletion needs sign-off (D-04).

### Deferred Ideas (OUT OF SCOPE)
- **QUAL-04 correctness** (int32 overflow guard QA-B-08; `bootChatEnvWithConfig` double-`Validate`/pool-leak QA-A-03; `AURA_*` hot-path env catalogue) → **Phases 33/34**.
- **MCP trust-normalization unify** (QA-C-03 / F-027) → **Phase 38**.
- **`decode*Body` strict-decode unify** (QA-C-01 / F-052) → **Phases 38/40**.
- **Whole-tree dead-code sweep** beyond the audit + refactor-on-touch (D-03). Log large untouched-file findings as follow-up; don't act this phase.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **QUAL-02** | Delete dead exports / reinvented-stdlib / placeholders (`assets.Status{Created,Embedding,Canceled}`, `AURA_MEMORY_EMBED_*`, `agui.indexByte`/`stringList`, telebot blank import, `RequestID` re-stamp), each confirmed via `deadcode`/`knip`/repo-wide `rg`. | Per-symbol triage verdicts (this doc §Dead-vs-Unwired Triage). **2 of 5 named items flip on triage**: `RequestID` re-stamp is **load-bearing (keep)**; `assets.Status*` is **intended-but-unwired (escalate per D-02/D-04)**. The other 3 are clean swaps/removals. |
| **QUAL-03** | Extract shared helpers — `internal/neostore`, `internal/envutil`, `internal/agentrender`, agent `CanonicalArgs` + `isTransientNetworkErr`, web single `getJSON` + shared `focusTrap`. Parity test per extraction. | Exact source file:line, call sites, proposed API surface, and import-cycle/boundary analysis (§Shared-Package Extraction). All extractions are import-cycle-safe and do **not** touch the `agent ⇸ agui` boundary. |
| **QUAL-05** | Close test gaps — `web/throttle.go`, setup `InvalidateToken`-before-SSE ordering, Telegram `answersFromText` keyword fallback, `truncateTailBytes`, Authula `ensureAuthulaSearchPath` DSN parsing, + `memory_integration` CI leg. | Located every target with signature (§Test-Gap Targets, §Validation Architecture). **`memory_integration` leg already exists & runs live** (ci.yml 606-719) — D-12 = document, not add. Authula DSN test **stays in 32** (existing pure function, no cutover infra). |
</phase_requirements>

## Summary

This is a behavior-preserving maintainability phase driven entirely by the 2026-06-29 quality audit. The technical risk is low and the work is mostly mechanical; the **research risk lives in two places the planner cannot derive from CONTEXT.md**: (1) which "dead" symbols are genuinely dead vs intended-but-unwired vs *load-bearing*, and (2) the exact union-of-inputs parity-test shape per extraction so the Feathers safety net (D-09/D-10) actually proves behavior is preserved.

Direct inspection of the tree produced four high-value catches that change the plan: **(a)** the "redundant" `RequestID` re-stamp at `cmd/aura/agent.go:127` is **NOT dead** — the dry-run uses the fake `InfiniteToolCallAgent`, which omits `RequestID` (verified at `agenttest/mocks.go:61`), so line 127 is the only uniform stamp on that path; the audit's premise (`LlmAgent.newEvent` copies it) applies only to the real agent path. **(b)** The `memory_integration` CI leg D-12 asks for **already exists and runs live with full no-skip-as-green discipline** (`ci.yml` 606-719); QA-A-09 is stale → the action is to document, not author a redundant leg. **(c)** The telebot blank import's in-code justification ("amendment-#58 CI pin gate's stable grep target") is **false** — there is zero telebot reference in `.github/`, `scripts/`, or `Makefile`; telebot is DIRECT in `go.mod:38` purely via the genuine consumer `telegram/bot.go:18`, so removal is safe after a `go mod tidy` check. **(d)** `assets.Status{Created,Embedding,Canceled}` are part of a **designed-but-unbuilt lifecycle** (see `docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md`); wiring them is new feature behavior → D-02 guardrail escalate; they are exported → D-04 operator sign-off.

The three extraction packages are all import-cycle-safe leaves/near-leaves and none introduce a back-edge across the `agent ⇸ agui` boundary. The most subtle extraction is the transient-error classifier: `retryableStreamOpenError` must be **strict parity** (no behavior change) while `isTransientToolErr` is an **intentional widening** — they are NOT symmetric and a naïve "both delegate to one function" would break the stream path (which deliberately returns `false` for `context.DeadlineExceeded`, the opposite of the tool path). The `AURA_MEMORY_EMBED_*` removal is a **full-stack** change (Go + frontend type union + frontend `BACKEND_SETTINGS` + i18n labelKeys en+it + dist rebuild), not the one-liner the audit's "S" effort implies.

**Primary recommendation:** Run dead-code triage as Wave 1 with the per-symbol verdicts below (expect 2 of 5 QUAL-02 items to resolve to keep/escalate, not delete). Then test-first extractions (Wave 2) using the per-helper union tables in §Validation Architecture. Close the test gaps (Wave 3) and **document** the already-live `memory_integration` leg. Honor D-14 (sequential, no worktrees) and D-11 (per-item atomic commits).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Neo4j/Cypher result coercion (`AsFloats`/`AsString`), content-hash MERGE key (`HashText`), graph seam (`GraphClient`) | `internal/neostore` (new) | reasoningstore / toolselectstore / activelearn (consumers) | All four are Neo4j-persistence concerns; a single leaf package funnels them (D-06 canonical). |
| Postgres `pgtype.Numeric` ↔ `float64` (`NumericFromFloat`/`FloatFromNumeric`) | `internal/db` (existing) | conversations / cachemetrics (consumers) | A Postgres concern, **not** Neo4j — see Open Question #1 (D-07 literally lists it under neostore). |
| Env-var parsing with silent fallback (`IntDefault`/`BoolDefault`) | `internal/envutil` (new) | config / channels / telegram (consumers) | Pure stdlib leaf; kills the 3 self-documented copies. |
| Event/usage rendering primitives (`FlushRemainder`/`AnyInt`/…) | `internal/agentrender` (new) | cmd/aura (chat_render) / internal/eval (consumers) | Shared by the REPL renderer and the eval scorer; centralizes drift-prone event shaping. |
| Tool-call canonicalization (`CanonicalArgs`) | `internal/canonicaljson` (existing leaf) | agent / agent/workflow (consumers) | Both call sites already import canonicaljson — zero new edges. |
| Transient-error classification (`isTransientNetworkErr` shared subset) | `internal/agent` (same package) | — | Both copies are already in package `agent`; same-package extraction. |
| HTTP read helper (`getJSON`) | `web/src/api/json.ts` (existing canonical) | conversations / governance (consumers) | Two byte-identical local copies collapse to the canonical import. |
| Focus trap | `web/src/a11y/focusTrap.ts` (existing canonical) | BoardLayout / McpLifecycleCluster (consumers) | The inline copies are **buggy**; adopting the util fixes latent a11y defects. |

**Boundary guarantee (verified):** `internal/agui` imports `internal/agent` (correct direction); `internal/agent` does NOT import `internal/agui`. None of the new packages (`neostore`, `envutil`, `agentrender`) import `agui`, and `agentrender → agent` does not close any cycle (agent never imports agentrender). The `agui.indexByte`/`stringList` simplifications happen **in place** inside `agui` — no extraction crosses the boundary.

## Project Constraints (from CLAUDE.md)

These have locked-decision authority for this phase:
- **Per-package coverage ≥85% hard floor** (overrides the PRD ≥75%/≥60%). The 3 new packages must each clear 85% via their parity tests.
- **No-skip-as-green CI** — skip helpers `t.Fatal` under `$CI`; a skipped tier fails the gate.
- **Refactor-on-touch + ≤600-LOC file cap** — every touched file gets dead-code removal + dup-folding in the same commit; never create a file >600 LOC.
- **Commit discipline: 1 concept = 1 commit** (reinforced by D-11), imperative subject + why-body + Co-Authored-By trailer.
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken (with justification).
- **READ BEFORE EDIT / NEVER SUPPOSE** — re-read a file not touched in the last 5 messages.
- **Post-edit validation (Gate 2):** `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/` per touched package.
- **Master-direct workflow** (MEMORY): commit directly on master; no feature branches/PRs unless asked. **Never `git push` unless explicitly requested this turn.**
- **NEVER run `.exe` on this host** (AV blocks) — build/test in WSL or container.
- **The PRD-mandated `agent ⇸ agui` import boundary must not gain a back-edge** from any extraction.

## Dead-vs-Unwired Triage (QUAL-02 — the #1 research output)

Provisional verdicts from direct code reads. **Confirmation command set for every removal** (run all three; per D-04 they must all agree, and exported symbols need operator sign-off):
```bash
# from WSL, ~/go/bin on PATH, stack not required for dead-code:
deadcode -test $(bash scripts/go_packages.sh)            # Go dead-code (the -test flag also scans test files)
rg -n 'SYMBOL' --glob '!**/dist/**'                       # repo-wide incl. _test.go + build-tagged
cd web && npm run deadcode                                 # knip (frontend symbols only)
```

| # | Symbol (file:line) | Provisional verdict | Evidence | Action |
|---|---|---|---|---|
| T1 | `cmd/aura/agent.go:127` — `ev.RequestID = requestID` | **LOAD-BEARING — KEEP. NOT dead.** [VERIFIED: read] | The dry-run drives `agenttest.InfiniteToolCallAgent`, whose `Run` builds `&agent.Event{Author, Branch, LLMResponse}` with **no `RequestID`** (`agenttest/mocks.go:61-65`). A repo-wide grep for `RequestID` in `internal/agent/agenttest/` returns only `RecordingAgent`'s doc comment. The audit's premise — "`LlmAgent.newEvent` already copies `ic.RequestID`" — is true only for the *real* agent path, which the dry-run never uses. Removing line 127 emits `"request_id":"00000000-…"` on every dry-run line, breaking SC#4 reproducibility. | **Do NOT remove.** Add a characterization test asserting every dry-run event carries `requestID` (passes WITH line 127, fails WITHOUT → proves load-bearing). Clarify the comment to say *the fake agent does not stamp it*. Surface to operator that this named QUAL-02 item resolved to keep. |
| T2 | `internal/assets/types.go:14,20,25` — `StatusCreated`/`StatusEmbedding`/`StatusCanceled` | **INTENDED-BUT-UNWIRED → ESCALATE (D-02 guardrail) + D-04 sign-off.** [VERIFIED: read+grep] | Production emits only `StatusPresigned` (`assets/store.go:41`) and `StatusUploaded` (via MarkUploaded). The full 12-status set is a designed lifecycle from `docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md` (created→presigned→uploaded→…→embedding→…→canceled). The 3 named are unused — **but so are ~7 more** (accepted/processing/searchable/complete/failed/refused/deleted). Note `onboarding.StatusCanceled` is a *different* package's constant and IS used — do not confuse them. No JSON-string consumer found. | Wiring the status machine = new feature behavior → **D-02 escalate**. Exported → **D-04 operator keep/kill**. Recommend **KEEP + annotate** (document the deferred lifecycle) over delete-and-churn. If operator says delete, delete only the 3 named (D-03 no sweep); log the other ~7 as a follow-up. |
| T3 | `internal/agui/governance_api.go:226` — `indexByte` | **REINVENTED-STDLIB, USED → SWAP.** [VERIFIED: read] | Reimplements `strings.IndexByte`; called by `envChips` (`:213`). Unexported. | Replace the call with `strings.IndexByte(entry, '=')` (or `strings.Cut`); delete `indexByte`. Behavior-preserving. |
| T4 | `internal/agui/governance_api.go:199` — `stringList` | **REINVENTED-STDLIB, USED → INLINE (preserve non-nil).** [VERIFIED: read] | No-op defensive copy; called at `:177` `NetworkAllowlist: stringList(server.Runtime.Network)`. ⚠️ Returns a **non-nil empty** slice for empty input (`make([]string,0,len)` + append). | Inline as `append([]string{}, server.Runtime.Network...)` — **must** keep `[]string{}` (not a `nil` var) so JSON stays `[]` not `null`. Add/keep a test asserting empty input → `[]` (not `null`) in the marshalled allowlist. |
| T5 | `internal/channels/deps.go:37` — `_ "gopkg.in/telebot.v4"` | **REDUNDANT → SAFE TO REMOVE (verify `go mod tidy`).** [VERIFIED: read+grep] | `telegram/bot.go:18` imports `tele "gopkg.in/telebot.v4"` genuinely → `go.mod:38` keeps it DIRECT regardless. The in-code comment claims it is "the amendment-#58 CI pin gate's stable grep target" — **but there is zero telebot reference in `.github/`, `scripts/`, or `Makefile`** (grep returned nothing). The justification is stale; the prior x/image and qrterminal anchors were already removed (per the same file's comments). | Remove the blank import. Run `go mod tidy` and confirm `gopkg.in/telebot.v4 v4.0.0-beta.9` stays in the **direct** require block; `go build $(bash scripts/go_packages.sh)` green. The import block becomes empty → either delete `deps.go` (move the package-doc comment to a `doc.go` or onto `registry.go`) or keep it doc-only. Unexported anchor → D-04 free-delete after the tidy check. |
| T6 | `internal/settings/settings.go:61-62` — `AURA_MEMORY_EMBED_BASE_URL`/`_API_KEY` | **SIDECAR-OWNED → REMOVE-FROM-GO + DOCUMENT (D-05).** [VERIFIED: read+grep] | No Go `os.Getenv("AURA_MEMORY_EMBED_*")` exists. `compose.yaml:405-406` maps them to the sidecar's `OPENAI_API_KEY`/`OPENAI_BASE_URL` **at container start**. `settings.OverlayEnv` does `os.Setenv` in the **daemon** process — which cannot reach an already-running sidecar container. So the cockpit control is a silent no-op for these two keys. | See §`AURA_MEMORY_EMBED_*` Removal Map below — it is a **full-stack** change. |
| T7 | `internal/llm`…`internal/agent/llm_agent.go:235` — discarded `Build()` (QA-A-11) | **DEAD WORK → RESTRUCTURE.** [CITED: slice-A] | When `adaptiveTierOK`, both `Build(...)` and `BuildWithReasoningTier(...)` run; the first is discarded (a full `RenderToolDefs()` per turn). | Restructure to `if adaptiveTierOK { req = BuildWithReasoningTier(...) } else { req = Build(...) }`. Parity: assert the chosen `req` is byte-identical to the old chosen `req` for both branches. |
| T8 | `truncateRunes` ×2 (`assets/context.go:133`, `rerank/client.go:167`) (QA-C-13) | **DUP 5-LINER → FOLD or ACCEPT.** [VERIFIED: grep] | Two copies in unrelated packages (param names differ: `value` vs `s`). Audit explicitly allows "accept at this scale." | See Open Question #2. A new `internal/strutil` would itself need coverage registration; prefer accept-with-cross-ref-comment unless an existing string-util home exists. Minor. |

**Note on `qrSVG` (QA-C-11) and `AgentTier` (QA-A-07):** README theme T3 mentions them, but they are **NOT** in the QUAL-02 named list — out of scope here (refactor-on-touch only if a touched file contains them; otherwise log as follow-up per D-03).

### `AURA_MEMORY_EMBED_*` Removal Map (D-05 — full-stack)

The audit's "S / one-liner" is wrong — removing these touches the whole stack. All four edits are behavior-preserving (removing a no-op control):

1. **Go:** delete the 2 entries from `settings.AllowedKeys` (`internal/settings/settings.go:61-62`). `OverlayEnv`'s allowlist guard (`if _, ok := AllowedKeys[r.Key]; !ok { continue }`) means any **stale `aura.settings` DB row is silently ignored** — **no migration required**. (Optional cosmetic: `DELETE FROM aura.settings WHERE key LIKE 'AURA_MEMORY_EMBED_%'`.)
2. **Frontend type union:** remove `'AURA_MEMORY_EMBED_BASE_URL' | 'AURA_MEMORY_EMBED_API_KEY'` from `SettingsKey` (`web/src/settings/ModelSettingsPanel.tsx:39-40`).
3. **Frontend array:** remove the 2 `BACKEND_SETTINGS` entries (`ModelSettingsPanel.tsx:102-112`).
4. **i18n:** remove `settings.fields.memoryEmbedBaseUrl` and `settings.fields.memoryEmbedKey` from **both** `en` and `it` resources (symmetric removal preserves the i18n-parity test; knip/tsc will flag if they're referenced elsewhere).
5. **Docs:** annotate in `compose.yaml` (near 402-406) and `.env.example` that `AURA_MEMORY_EMBED_BASE_URL`/`_API_KEY` remain valid **compose/.env** variables consumed by the agent-memory sidecar at container start — they are NOT runtime-overridable via the cockpit (the daemon's `os.Setenv` cannot reach a running sidecar).
6. **Operational:** rebuild + commit `internal/webui/dist` (the `web-dist-freshness` CI gate diffs it — see Pitfall 3).

Security note: `AURA_MEMORY_EMBED_API_KEY` is `Secret: true`. Removal must not log the value; `secret.IsSecretEnvKey` redaction is unaffected.

## Shared-Package Extraction (QUAL-03 — mechanics + import safety)

### `internal/neostore` (canonical, per D-06)

**Source (all byte-identical across copies — verified):**
- `AsFloats(v any) []float64` ← `reasoningstore/store.go:95-121` ≡ `toolselectstore/store.go:130-156`
- `AsString(v any) string` ← `reasoningstore/store.go:85-90` ≡ `toolselectstore/store.go:120-125`
- `HashText(s string) string` ← `reasoningstore/store.go:80-83` ≡ `toolselectstore/store.go:115-118` (`hashQuery`) ≡ `activelearn/learner.go:113-116` (3 copies)
- `GraphClient` interface (`Read`/`Write`) ← `reasoningstore/store.go:19-22` ≡ `toolselectstore/store.go:22-25`

**Proposed API:** exported `HashText`, `AsString`, `AsFloats`, and `GraphClient`. **Dependencies:** stdlib only (`crypto/sha256`, `encoding/hex`, `encoding/json`, `context`) — a pure **leaf**, zero cycle risk.
**Call-site migration:** `reasoningstore` + `toolselectstore` replace their unexported helpers/interface with `neostore.*` (delete the locals). `activelearn` imports `neostore.HashText`. `*knowledge.Client` already satisfies `neostore.GraphClient` structurally — no change at the satisfying type.
**Cycle check:** `activelearn → neostore`, `reasoningstore → neostore`, `toolselectstore → neostore`; `neostore → nothing`. Safe (verified the learn/store layering in slice-B §E).

### `internal/db` for `NumericFromFloat`/`FloatFromNumeric` (RECOMMENDED — see Open Question #1)

**Source (near-identical; only the error *string* differs):**
- `conversations/store_helpers.go:148-177` (`numericFromFloat` uses `"+/-%.4f"`) vs `cachemetrics/store_helpers.go:67-97` (`numericFromFloat` uses `"±%v"`). Numeric logic identical: NaN/Inf/range guard → scale ×1e4 → round half-away-from-zero → `big.NewInt` mantissa, `Exp: -numericScale`. `floatFromNumeric` is byte-identical. `numericMaxCost = 999999.9999` is defined in both.
**Proposed API:** `db.NumericFromFloat(f float64) (pgtype.Numeric, error)`, `db.FloatFromNumeric(n pgtype.Numeric) float64`, plus exported `DefaultNumericMaxCost`. Both callers already import `internal/db` (already coverage-gated).
**⚠️ Parity caveat:** assert the returned `pgtype.Numeric` (Int+Exp) and the *presence* of an error — **not** the error string (the two copies already differ).

### `internal/envutil` (minimal, per D-06)

**Source:** `config/config_env.go:28-54` (canonical `envIntDefault`/`envBoolDefault`) ≡ `channels/telegram/config.go:56-66` (`envIntDefault`) ≡ `channels/registry.go:162-173` (`envChannelEnabled` — a *channel-specific* wrapper of the bool-default contract).
**Proposed API:** `envutil.IntDefault(key string, fallback int) int`, `envutil.BoolDefault(key string, fallback bool) bool` (and optionally `StringDefault`, `SliceDefault` to mirror `config_env.go`). Pure stdlib leaf (`os`, `strconv`, `strings`).
**Call-site migration:** `config_env.go` helpers become thin `envutil.*` calls (or are deleted, with `loadBase` calling `envutil` directly); `telegram/config.go` drops its copy → `envutil.IntDefault`; `channels/registry.go` `envChannelEnabled` keeps its key-building but delegates the parse → `envutil.BoolDefault(key, true)`.
**Scope boundary (important):** D-07 says "adopt for agent-tool knobs (QA-A-05/08)." Treat that as the **mechanical `os.Getenv`+parse → `envutil` swap only**. The full QA-A-05/08 fix (move reads to construction time, add to `config.Load`/`Config`, catalogue the knob) is **QUAL-04 → Phase 33/34 (OUT)**. Keep envutil minimal: the 3 named copies; agent-tool adoption only if those files are otherwise touched.
**Cycle check:** `config → envutil`, `channels → envutil`, `telegram → envutil`; `envutil → nothing`. Safe.

### `internal/agentrender` (minimal, per D-06)

**Source (~80 LOC, with a real drift — verified):**
- `flushRemainder`, `isToolResultPreview(*agent.Event)`, `isTerminalToolCall([]llm.ToolCall)`, `usageFromStateDelta(map[string]any) llm.Usage`, `anyInt`, `anyFloat` in `cmd/aura/chat_render.go:111-229` ≡ `internal/eval/capture_cot_eval.go:153-229`.
- **⚠️ DRIFT:** `chat_render.go`'s `anyInt` handles `json.Number` (`:222-228`); `eval`'s `anyInt` does **NOT** (`capture_cot_eval.go:202-213`, default → 0). The eval copy silently zeroes `json.Number` token counts.
**Proposed API:** exported `FlushRemainder`, `IsToolResultPreview`, `IsTerminalToolCall`, `UsageFromStateDelta`, `AnyInt`, `AnyFloat`. **Adopt the superset** (`chat_render`'s `json.Number`-aware `AnyInt`) — this is a behavior **fix** for the eval path, which the parity test must document.
**Dependencies:** `internal/agent` (for `*agent.Event`), `internal/llm` (for `llm.ToolCall`/`llm.Usage`), stdlib. (Alternative to avoid the `agent` import: make `IsToolResultPreview` take `map[string]any` (the StateDelta) — minor signature change at 2 call sites. Either is boundary-safe.)
**Cycle/boundary check:** `agentrender → agent` + `agentrender → llm`; neither `agent` nor `llm` imports `agentrender`. Consumers `cmd/aura` and `internal/eval` already import `agent`. **`agentrender` does NOT import `agui`** and adds no back-edge across the `agent ⇸ agui` boundary. Safe.

### agent `CanonicalArgs` (QA-A-01)

**Source (byte-identical except name — verified):** `agent/workflow/loop.go:345-355` (`canonArgs`, package `workflow`) ≡ `agent/llm_agent_args.go:66-76` (`canonicalArgs`, package `agent`).
**Proposed home:** `internal/canonicaljson.CanonicalArgs(s string) []byte` — both call sites already import `canonicaljson` (a leaf), so no new edge and no cross-package cycle between `agent` and `agent/workflow`.

### agent `isTransientNetworkErr` (QA-A-02) — ⚠️ asymmetric, read carefully

**Source:** `agent/llm_agent_retry.go:56-68` (`isTransientToolErr`) and `agent/llm_agent_stream_retry.go:77-113` (`retryableStreamOpenError`) — both package `agent` (same-package extraction).
**They are NOT symmetric:** `isTransientToolErr` returns **true** for `context.DeadlineExceeded` (`:60`); `retryableStreamOpenError` returns **false** for `context.DeadlineExceeded`/`Canceled` (`:78`). A naïve "both delegate to one predicate" would break the stream path.
**Proposed shared subset** `isTransientNetworkErr(err) bool` = the **typed network** subset both share: `net.Error.Timeout()` + `errors.Is` of `io.ErrUnexpectedEOF`/`io.EOF`/`syscall.ECONNRESET`/`ECONNREFUSED`/`ETIMEDOUT`. `nil`/other → false. (Excludes `context.*`, HTTP status, url.Error, `retryableNetworkText`, `ErrStreamIdleTimeout` — those stay domain-specific.)
- **`isTransientToolErr` (INTENTIONAL WIDENING)** = `errors.Is(err, context.DeadlineExceeded) || isTransientNetworkErr(err)`. Now retries on ECONNRESET/EOF etc. (previously only timeout/deadline). Behavior change — characterize old, assert new widened set.
- **`retryableStreamOpenError` (STRICT PARITY)** = keep the leading `context.Canceled`/`DeadlineExceeded → false` guard FIRST, then HTTPError 429/5xx, url.Error, `ErrStreamIdleTimeout`, `isTransientNetworkErr(err)`, and the `retryableNetworkText` fallback last. No behavior change.

### Web: single `getJSON` + shared `focusTrap` + skeleton (D-07/D-08)

- **`getJSON` (QA-D-01):** `web/src/api/json.ts:1-10` is canonical. Two byte-identical copies: `conversations/useConversations.ts:61-70` (unexported) and `governance/governanceApi.ts:113-122` (exported). Delete both; import from `../api/json`. Check whether anything imports `getJSON` *from* `governanceApi` (it is exported) and repoint those to `api/json`. Opportunistic (refactor-on-touch): `useConversations.createConversation` (`:84-104`) hand-rolls a POST — fold to `postJSON` (QA-D-04) while in the file.
- **`focusTrap` (QA-D-02):** `web/src/a11y/focusTrap.ts` is canonical (`focusFirstDescendant`, `trapTabKey`, with the `disabled` filter). The inline copies are **buggy**: `BoardLayout.tsx:56-91` omits the disabled filter; `McpLifecycleCluster.tsx:208-238` (`RemoveDialog`) queries only `button`. Adopting the util **fixes** these (not strict byte-parity). The consumer component tests must stay green.
- **Skeleton unification (QA-D-08 / D-08):** two systems — `components/skeleton/Skeleton.tsx` (349 LOC, rich, CSS-wave, `--skeleton-*` tokens; used only by `AppSkeletons.tsx`) vs `components/ui/skeleton.tsx` (13 LOC shadcn `animate-pulse`; used by `ConversationSidebar`, `SearchPanel`, `governanceView`). **Recommend: keep the rich custom system, migrate the 3 shadcn consumers, retire `ui/skeleton.tsx`** (then the barrel exports QA-D-13/D-18 become real — fold those in). **Highest-regression frontend item** (visual) → requires Playwright E2E + dist rebuild.

## Test-Gap Targets (QUAL-05) — located with signatures

| Target | Location | Shape |
|---|---|---|
| `web/throttle.go` | `internal/web/throttle.go:1-49` — `hostThrottle.acquire/sem`, `perHostLimit=2` | Concurrency primitive: acquire blocks at limit, release frees a token, ctx-cancel → `ok=false` + no-op release, per-host isolation, concurrent race. |
| setup ordering | `internal/setup/handlers.go:146` — documented `InvalidateToken`-before-`writeSSE` fix | Regression test asserting `InvalidateToken` is called before the first SSE write (the race the comment fixes). |
| Telegram keyword fallback | `internal/channels/telegram/profile_onboarding.go:362` — `answersFromText(text string) profileflow.Answers` | Italian-keyword-fallback table (only the LLM-extractor path is currently tested). |
| `truncateTailBytes` | `internal/agent/llm_agent_completion.go:209-221` (+ `truncateBytesKeepingTail` `:196-206`) | UTF-8 boundary table (mirror `TestTruncateBytes`): `n<=0`, `len(s)<=n`, multi-byte mid-rune walk-back, head+marker+tail. |
| Authula DSN | `internal/webauth/authula.go:292` — `ensureAuthulaSearchPath(dsn string) (string, error)` | Table: empty / malformed / already-has-`search_path` / no-`search_path`. **Stays in Phase 32** (see Source Conflict). |
| `memory_integration` CI leg | `ci.yml:606-719` — **already exists** | **Document, do not add** (see D-12 Verdict). |

### Source Conflict resolution (Authula DSN test)
ROADMAP §32 C3 + REQUIREMENTS QUAL-05 place it **IN Phase 32**; audit README §E routes it to **Phase 34**. **Verdict: keep IN Phase 32.** `ensureAuthulaSearchPath` already exists (`authula.go:292`) as a pure string function; a table test of it needs **no Authula-cutover infrastructure** (that is the broader MUSR-06 work, distinct from this unit test). HIGH confidence; not a blocker.

### D-12 Verdict: `memory_integration` already runs live
The CI job **"Memory MCP (memory_integration tier, live agent-memory sidecar)"** (`ci.yml:606-719`) already: sets `CI:"true"` (arms `t.Fatal`), exports `AURA_AGENT_MEMORY_MCP_URL` (`:642`), compiles the tier always (`go vet -tags memory_integration ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/`, `:653-656`), brings up the full live stack, and runs `go test -race -tags memory_integration -count=1 -p 1 ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/` (`:711-715`). The two files QA-A-09 named are covered: `internal/agent/memory_recall_integration_test.go` carries `//go:build memory_integration` (`:1`) and `t.Fatal`s under `$CI` when the URL is unset (`:52`); `mcptools/memory_integration_test.go` is in `./internal/agent/mcptools/`. **QA-A-09 is stale.** Action per D-12: **document the already-live leg** (e.g. in the phase VALIDATION.md / a note), verify the two files' tags+`t.Fatal` one last time, and do NOT add a redundant matrix entry.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| First index of a byte | `agui.indexByte` | `strings.IndexByte` / `strings.Cut` | Reinvented stdlib (QA-C-10). |
| Defensive slice copy | `agui.stringList` | `append([]string{}, in...)` | Reinvented stdlib — but keep non-nil-empty for JSON `[]` (Pitfall 4). |
| Env parse + fallback | a 4th copy of `envIntDefault`/`envBoolDefault` | `internal/envutil` | 3 self-documented copies already drifted in comments. |
| sha256→hex content hash | a 4th copy of `hashText`/`hashQuery` | `neostore.HashText` | 3 byte-identical copies. |
| `pgtype.Numeric` ↔ float | a 3rd copy of `numericFromFloat` | `db.NumericFromFloat` | numeric(10,4) scale/rounding must stay in lockstep. |
| Parity/golden testing | bespoke assert loops | `golang-testing` table tests + `goleak.VerifyNone` in `TestMain` | The mandated D-09/D-10 vehicle. |
| Adding a CI integration tier | a new `memory_integration` job | the **existing** one (ci.yml 606-719) | Already live with no-skip-as-green. |

**Key insight:** in this codebase the duplication is *self-documented* ("Copied verbatim from reasoningstore", "local copy to avoid internal/config import", "duplicated by design"). The fix is always extract-to-leaf + redirect, never a generic abstraction — slice-B §E proves the store/learn *packages* are correctly specialized; only the *helpers* leaked.

## Runtime State Inventory

This phase is code/config + tests only. Verified there is **no** runtime-state migration hidden in it:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.settings` rows for `AURA_MEMORY_EMBED_*` may exist if an operator set them. After T6 removal, `OverlayEnv`'s allowlist guard **ignores** unlisted rows. | **None** (no migration). Optional cosmetic `DELETE FROM aura.settings WHERE key LIKE 'AURA_MEMORY_EMBED_%'`. |
| Live service config | `AURA_MEMORY_EMBED_*` consumed by the agent-memory sidecar via `compose.yaml:405-406` at container start. | **None** — keep them as compose/.env vars; only the cockpit overlay control is removed (it was a no-op). Document. |
| OS-registered state | None — verified (no Task Scheduler / systemd / pm2 surface in scope). | None. |
| Secrets/env vars | `AURA_MEMORY_EMBED_API_KEY` is `Secret`. Code rename/removal only; the compose/.env var name is unchanged. | None (do not log the value on removal). |
| Build artifacts | `internal/webui/dist` must be rebuilt after ANY `web/src` edit (T6, focusTrap, skeleton, getJSON). | **Rebuild + commit dist** (Pitfall 3). |

## Common Pitfalls

### Pitfall 1: Treating every deadcode flag as a delete
**What goes wrong:** removing `cmd/aura/agent.go:127` (RequestID) or `assets.Status*` destroys load-bearing/intended-future work. **Why:** the audit's confidence is Medium on exactly these and its premise is path-specific. **Avoid:** run the triage table above; characterization test for line 127; D-04 sign-off for `assets.Status*`. **Warning sign:** a "dead" symbol whose removal makes a test go red is not dead.

### Pitfall 2: Symmetric merge of the transient-error classifiers
**What goes wrong:** a single `isTransientNetworkErr` that both `isTransientToolErr` and `retryableStreamOpenError` simply *return* breaks the stream path's deliberate `context.DeadlineExceeded → false`. **Avoid:** shared subset = typed network sentinels only; keep the stream's context-guard FIRST and the tool's `DeadlineExceeded → true` separate. **Warning sign:** a stream test that previously returned `false` for a deadline now returns `true`.

### Pitfall 3: Web edits without a dist rebuild
**What goes wrong:** the `web-dist-freshness` CI job diffs `internal/webui/dist`; an un-rebuilt dist fails the next push (this already bit Phase 31, see MEMORY). **Avoid:** after any `web/src` change (T6, getJSON, focusTrap, skeleton, i18n), rebuild `web/` and commit `internal/webui/dist` in the same/adjacent commit. **Warning sign:** `git status` shows deleted/changed `dist/assets/*`.

### Pitfall 4: `stringList` inline drops non-nil-empty
**What goes wrong:** inlining to a `nil`-based copy changes the marshalled `NetworkAllowlist` from `[]` to `null`. **Avoid:** `append([]string{}, in...)`. **Warning sign:** an allowlist JSON snapshot test flips to `null`.

### Pitfall 5: `numericFromFloat` parity asserted on the error string
**What goes wrong:** the two copies use different error wording (`"+/-%.4f"` vs `"±%v"`); a string-equality parity assert can never pass. **Avoid:** assert the `pgtype.Numeric` value + `err != nil` presence, not the message.

### Pitfall 6: i18n key removal breaks the parity test asymmetrically
**What goes wrong:** dropping `settings.fields.memoryEmbed*` from `en` only fails the i18n parity test. **Avoid:** remove from `en` **and** `it` symmetrically. **Warning sign:** the i18n parity test reports a key present in one locale only.

### Pitfall 7: Coverage gate is `-p 1` and wipes shared PG
**What goes wrong:** `make coverage` runs `go test -tags 'db_integration neo4j_integration' -p 1 ./internal/...` which Resets the shared Postgres (MEMORY: coverage gate wipes shared PG). **Avoid:** dump evidence before running; new pure-unit packages don't need the stack but the aggregate run does. Also unset `.env`'s `AURA_WEB_AUTH_SECRET` before `make coverage` (MEMORY: .env leak breaks config tests).

## Validation Architecture

> Nyquist validation is ENABLED (`config.json` `workflow.nyquist_validation: true`). This section is the parity/characterization-test contract per D-09/D-10. The plan-phase orchestrator consumes this heading to generate VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + table tests; `go.uber.org/goleak` (`goleak.VerifyNone` in `TestMain` for any package with goroutines, e.g. `web/throttle`); race detector |
| Go quick run | `go test -race ./internal/<pkg>/` (per touched package) |
| Go full / coverage | `bash scripts/coverage_gate.sh` → `go test -tags 'db_integration neo4j_integration' -p 1 -covermode=atomic ./internal/...` (owned-surface ≥85%) |
| Go static | `golangci-lint run $(bash scripts/go_packages.sh)` (dupl threshold 100, `_test.go` excluded); `deadcode -test $(bash scripts/go_packages.sh)`; `govulncheck $(bash scripts/go_packages.sh)` |
| Web framework | Vitest (≥85% per-file coverage) + Stryker (≥70% killed); `npm run lint` (ESLint max-warnings=0 + jscpd + knip); Playwright e2e (skeleton change) |
| Web run | `cd web && npm test` · `npm run lint` · `npm run deadcode` (knip) · Stryker · `npm run build` (then commit `internal/webui/dist`) |

### Parity-Test Strategy (D-09/D-10: write GREEN against the OLD duplicated code first, then extract, then GREEN again)

For each extraction the executor writes a table test whose cases are the **union of inputs both old copies handled**, asserts the old copies agree, then asserts the extracted helper matches — committed green *before* the move.

| Extraction | "Union of inputs" cases | Parity assertion | Behavior change? |
|---|---|---|---|
| `canonicaljson.CanonicalArgs` (from `canonArgs`+`canonicalArgs`) | valid JSON object (unsorted keys→sorted), array, nested, number forms (`1e3`, `1.0`, large int), string/bool/null, malformed JSON (→raw), empty string (→raw `""`), whitespace-only | `canonArgs(x)==canonicalArgs(x)==CanonicalArgs(x)` (byte-equal) for all x | **No** (byte-identical copies) |
| `neostore.HashText` (3 copies) | `""`, `"a"`, unicode, long string | all old copies == new; deterministic sha256→hex | No |
| `neostore.AsString` (2 copies) | `string`→itself; `int`/`nil`/`[]any`/`map`→`""` | old==new for all | No |
| `neostore.AsFloats` (2 copies) | APOC JSON string `"[-0.02,0.5]"`→`[]float64`; malformed string→`nil`; `[]any{float64}`→ok; `[]any{int64,int}`→converted; `[]any{string}`→`nil`; `nil`→`nil`; other→`nil`; `"[]"` and `[]any{}` (capture exact nil-vs-empty) | old==new incl. the nil/empty distinction | No |
| `db.NumericFromFloat`/`FloatFromNumeric` (2 copies) | `0`, `±1.2345`, rounding `0.12345`→`0.1235`, boundary `±999999.9999`, over-range `1e9`→err, `NaN`/`±Inf`→err, tiny `0.00001`; round-trip `Float(Numeric(f))≈f` (±1e-4) | **`pgtype.Numeric` (Int+Exp) equal + err-presence equal** — NOT error string (Pitfall 5) | No |
| `agentrender.AnyInt` (chat_render superset vs eval lossy) | `int`, `int64`, `float64`, `json.Number(valid)`, `json.Number(invalid)`, `nil`, other | merged == `chat_render`'s (superset); **document** eval's old `json.Number`→0 | **Yes (eval fix):** eval gains `json.Number` handling |
| `agentrender.AnyFloat`/`UsageFromStateDelta`/`FlushRemainder`/`IsToolResultPreview`/`IsTerminalToolCall` | float64/int/json.Number; full StateDelta map; prefix/divergent/empty remainder; event with/without `tool_call_id`; calls with/without `text_response` | old==new (byte/struct-equal) | No |
| `isTransientNetworkErr` shared subset | `net.Error{timeout}`→T; `io.EOF`/`io.ErrUnexpectedEOF`→T; `syscall.ECONNRESET`/`ECONNREFUSED`/`ETIMEDOUT`→T; `nil`→F; plain error→F | new predicate matches the intended subset | n/a (new) |
| `isTransientToolErr` (WIDENED) | `context.DeadlineExceeded`→T (kept); net timeout→T; `ECONNRESET`→**T (was F)**; `io.ErrUnexpectedEOF`→**T (was F)**; validation error→F | characterize OLD, then assert NEW widened set; **document the change** | **Yes (intentional widening)** |
| `retryableStreamOpenError` (STRICT) | `context.Canceled`→F; `context.DeadlineExceeded`→F; `HTTPError{429/500/503}`→T; `HTTPError{400}`→F; net timeout→T; `url.Error{timeout}`→T; `url.Error{retryable text}`→T; `ErrStreamIdleTimeout`→T; `io.EOF`/`ErrUnexpectedEOF`→T; syscall sentinels→T; `retryableNetworkText` markers + bare `"eof"`→T; plain error→F | **golden table captured BEFORE refactor; output identical after** | **No** (must not change) |
| `agui` `indexByte`→`strings.IndexByte` | `"k=v"`→1; `"="`→0; `"novalue"`→-1; `""`→-1 | `envChips` output unchanged | No |
| `agui` `stringList`→inline | empty→`[]` (non-nil); 1 elem; N elems | marshalled `NetworkAllowlist` stays `[]`/list, never `null` | No |
| `llm_agent.go:235` Build restructure | `adaptiveTierOK=true` and `=false` | chosen `req` byte-identical to old chosen `req` per branch | No (same output) |
| web `getJSON` (3 identical) | n/a (provably identical) | existing fetch-mock tests stay green; `api/json` test covers the contract | No |
| web `focusTrap` adoption | open/close/Tab/Shift-Tab; disabled element skipped; non-button focusable reached | consumer tests (`BoardLayout.test.tsx`, `McpLifecycleCluster`) + `focusTrap` test green | **Yes (a11y fix):** buggy copies replaced |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| QUAL-02 | `agui.indexByte`/`stringList` swap preserves output | unit | `go test -race ./internal/agui/` | ✅ (extend) |
| QUAL-02 | `RequestID` stamp load-bearing on dry-run | unit | `go test -race -run Dry ./cmd/aura/` | ✅ (extend) |
| QUAL-02 | telebot removal keeps build + go.mod direct | build | `go mod tidy && go build $(bash scripts/go_packages.sh)` | n/a |
| QUAL-02 | `AURA_MEMORY_EMBED_*` removed Go+web; settings unaffected | unit+web | `go test ./internal/settings/` · `cd web && npm test` | ✅ (extend) |
| QUAL-03 | `neostore` helpers parity | unit | `go test -race -cover ./internal/neostore/` | ❌ Wave 0 |
| QUAL-03 | `db.NumericFromFloat` parity | unit | `go test -race ./internal/db/` | ✅ (extend) |
| QUAL-03 | `envutil` parity | unit | `go test -race -cover ./internal/envutil/` | ❌ Wave 0 |
| QUAL-03 | `agentrender` parity (+ eval json.Number fix) | unit | `go test -race -cover ./internal/agentrender/` | ❌ Wave 0 |
| QUAL-03 | `CanonicalArgs` parity | unit | `go test -race ./internal/canonicaljson/ ./internal/agent/...` | ✅ (extend) |
| QUAL-03 | transient-error classifiers (parity + widening) | unit | `go test -race ./internal/agent/` | ✅ (extend) |
| QUAL-03 | web getJSON/focusTrap/skeleton | web+e2e | `cd web && npm test && npm run build` + Playwright (skeleton) | ✅ (extend) |
| QUAL-05 | `web/throttle` concurrency | unit | `go test -race ./internal/web/` | ❌ Wave 0 |
| QUAL-05 | setup ordering | unit | `go test -race ./internal/setup/` | ✅ (extend) |
| QUAL-05 | `answersFromText` keyword fallback | unit | `go test -race ./internal/channels/telegram/` | ✅ (extend) |
| QUAL-05 | `truncateTailBytes` UTF-8 | unit | `go test -race ./internal/agent/` | ✅ (extend) |
| QUAL-05 | Authula DSN edge cases | unit | `go test -race ./internal/webauth/` | ✅ (extend) |
| QUAL-05 | `memory_integration` runs in CI | doc/verify | inspect `ci.yml:606-719` + confirm tags/`t.Fatal` | ✅ exists |

### Sampling Rate
- **Per task commit:** `go test -race ./internal/<touched-pkg>/` (and `cd web && npm test` for web tasks).
- **Per wave merge:** `go vet $(bash scripts/go_packages.sh)` + `go build $(bash scripts/go_packages.sh)` + `golangci-lint run` + `deadcode -test`.
- **Phase gate:** `bash scripts/coverage_gate.sh` (≥85% owned + each new package ≥85%) + `cd web && npm run lint && npm test` + Stryker + dist-freshness, all green before `/gsd-verify-work`.

### Wave 0 Gaps (new test files / packages before implementation)
- [ ] `internal/neostore/{neostore.go,neostore_test.go}` — covers QUAL-03 (HashText/AsString/AsFloats/GraphClient); parity test drives ≥85%.
- [ ] `internal/envutil/{envutil.go,envutil_test.go}` — covers QUAL-03 (IntDefault/BoolDefault); `t.Setenv` table → ≥85%.
- [ ] `internal/agentrender/{agentrender.go,agentrender_test.go}` — covers QUAL-03 (render primitives + eval json.Number fix); ≥85%.
- [ ] `internal/web/throttle_test.go` — covers QUAL-05 (acquire/release/ctx-cancel/per-host/race) with `goleak`.
- [ ] Coverage-gate registration check: confirm `neostore`/`envutil`/`agentrender` are **not** caught by any exclude pattern in `scripts/coverage_gate.sh` (they are under `internal/` → auto-included; D-13). No script edit needed for inclusion; verify each clears 85% with `go test -cover ./internal/<pkg>/`.
- [ ] Extend existing tests: `internal/db` (numeric parity), `internal/agent` (CanonicalArgs, transient classifiers, truncateTailBytes), `internal/agui` (indexByte/stringList), `cmd/aura` (RequestID dry-run), `internal/setup`, `internal/channels/telegram`, `internal/webauth`, `internal/settings`, web (`useConversations`, `governanceApi`, `BoardLayout`, `McpLifecycleCluster`, skeleton consumers).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (WSL, CGO for -race) | all Go work | ✓ (per CLAUDE.md WSL primary) | go.mod-pinned | — |
| `golangci-lint` / `deadcode` / `govulncheck` / `goleak` | static + dead-code gates | ✓ (`~/go/bin`, `make tools`) | CI-pinned (golangci-lint v2.12.2) | — |
| `knip` (web dead-code) | QUAL-02 frontend confirmation | ✓ if `web/node_modules` present | per web/package.json | CI `web-lint` is the hard gate |
| Node + Vitest + Stryker + Playwright | web dedup + skeleton | ✓ (`cd web && npm ci`) | per web/package.json | — |
| Docker stack (PG/Neo4j/embed/memory sidecar) | `make coverage` aggregate + `memory_integration` verify | ✓ (compose; WSL→127.0.0.1) | compose.yaml | new pure-unit packages don't need it |

No new external packages are installed this phase → **no Package Legitimacy Audit / Standard Stack table needed** (cleanup-only). All extractions use the standard library and existing internal packages.

## Security Domain

This is a behavior-preserving refactor with **no new attack surface**. ASVS categories are not newly engaged. Three security-adjacent guards to respect:
- **V6 Cryptography / secrets:** `AURA_MEMORY_EMBED_API_KEY` is `Secret: true`. Its removal from `settings.AllowedKeys` must not log the value; `secret.IsSecretEnvKey` redaction is unaffected. `neostore.HashText` is sha256 content-addressing (a MERGE key, not a password) — leave as-is.
- **V5 Input validation:** the `agui.stringList` inline must preserve the non-nil allowlist (Pitfall 4) so the network-allowlist surface marshals identically.
- **Out of scope (do NOT touch):** MCP trust-normalization (QA-C-03/F-027 → Phase 38) and `decode*Body` strict-decode (QA-C-01/F-052 → Phase 38/40) are security-relevant dups deliberately deferred; merging them casually here would advance security work without the required trust tests.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | No JSON-string consumer reads `assets.Status{Created,Embedding,Canceled}` by literal value | Triage T2 | If a wire/DB consumer expects the string, deletion breaks it — mitigated by D-04 sign-off + the rg confirmation set before any removal. |
| A2 | `go mod tidy` keeps `gopkg.in/telebot.v4 v4.0.0-beta.9` DIRECT after removing the deps.go anchor | Triage T5 | If tidy demotes/drops it, a pin could change — executor MUST run the tidy+build check before committing (the action already requires it). |
| A3 | `internal/db` is the better home for `numericFromFloat` than `neostore` | Extraction / OQ#1 | Deviates from D-07's literal wording; planner/operator confirms. Low risk (both are valid; db is the semantic fit). |
| A4 | Recommending "keep the rich custom skeleton, retire shadcn `ui/skeleton.tsx`" | Web / D-08 | The reverse choice is defensible; visual-regression risk → Playwright E2E gates either direction. Operator/designer may prefer otherwise. |
| A5 | Authula DSN unit test needs no cutover infra (function already exists) | Source Conflict | If `ensureAuthulaSearchPath` is mid-refactor in a parallel branch, the test target could move — verified present at `authula.go:292` today. |

## Open Questions

1. **`numericFromFloat` home: `internal/db` (recommended) vs `internal/neostore` (D-07 literal).** What we know: it is a Postgres `pgtype.Numeric` helper; both callers already import `internal/db`; `db` is already coverage-gated; `neostore` is a Neo4j-named package. What's unclear: whether D-07's listing was deliberate or a bundling shorthand. Recommendation: put it in `internal/db` (cleaner, no Postgres-helper-in-Neo4j-package), flag to operator; this also keeps "3 new packages" (D-13) = exactly neostore/envutil/agentrender.
2. **`truncateRunes` fold (QA-C-13): shared util vs accept.** What we know: 2 copies of a 5-liner in unrelated packages (`assets`, `rerank`); the audit allows "accept at this scale"; CONTEXT QUAL-02 names it for folding. What's unclear: whether a new `internal/strutil` (which would itself need coverage registration) is worth it. Recommendation: check for an existing string-util home; if none, accept-with-cross-reference-comment, or a minimal `internal/strutil` if the operator wants the fold. Low priority.
3. **`assets.Status*` keep vs delete (D-04).** Needs operator sign-off; recommend KEEP+annotate (deferred lifecycle) over delete-and-re-add churn. If delete, only the 3 named (D-03).
4. **`deps.go` after the anchor's removal: delete file vs keep doc-only.** The package doc comment is valuable; recommend moving it to a `doc.go` (or onto `registry.go`) and deleting `deps.go`, vs keeping `deps.go` with only the package comment. Executor's call within refactor-on-touch.

## Sources

### Primary (HIGH confidence — direct reads/greps this session, 2026-06-29)
- `internal/assets/types.go`, `assets/store.go` (T2); `docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md` (designed lifecycle)
- `internal/settings/settings.go:46-126`, `compose.yaml:402-406`, `web/src/settings/ModelSettingsPanel.tsx` (T6 / removal map)
- `internal/channels/deps.go`, `internal/channels/telegram/bot.go:18`, `go.mod:38`; greps of `.github/`, `scripts/`, `Makefile` (T5 — no pin gate)
- `cmd/aura/agent.go:85-133`, `internal/agent/agenttest/mocks.go:54-71` (T1 — RequestID load-bearing)
- `internal/agui/governance_api.go:177,199,213,226` (T3/T4)
- `internal/agent/workflow/loop.go:340-356`, `internal/agent/llm_agent_args.go:62-76` (CanonicalArgs); `llm_agent_retry.go`, `llm_agent_stream_retry.go` (transient errors); `llm_agent_completion.go:196-221` (truncateTailBytes)
- `internal/reasoningstore/store.go`, `internal/toolselectstore/store.go`, `internal/activelearn/learner.go:113` (neostore); `internal/conversations/store_helpers.go:148-206`, `internal/cachemetrics/store_helpers.go:53-97` (numeric/parseUUID)
- `internal/config/config_env.go`, `internal/channels/registry.go:150-173`, `internal/channels/telegram/config.go:40-66` (envutil)
- `cmd/aura/chat_render.go:100-229`, `internal/eval/capture_cot_eval.go:145-229` (agentrender + the json.Number drift)
- `web/src/api/json.ts`, `web/src/a11y/focusTrap.ts`, `web/src/conversations/useConversations.ts:55-110`, `web/src/governance/governanceApi.ts:105-139` (frontend dedup)
- `internal/web/throttle.go`, `internal/channels/telegram/profile_onboarding.go:362`, `internal/webauth/authula.go:292` (test-gap targets)
- `.github/workflows/ci.yml:606-719` + `internal/agent/memory_recall_integration_test.go:1,52` (D-12 — leg already live)
- `scripts/coverage_gate.sh`, `scripts/go_packages.sh`, `.planning/config.json` (gates, nyquist on)

### Secondary (audit synthesis — CITED)
- `docs/audit/quality/{README,slice-A-agent-core,slice-B-persistence,slice-C-transport-web,slice-D-frontend-ops}.md`
- `.planning/REQUIREMENTS.md` (QUAL-02/03/05), `.planning/STATE.md`, `.planning/phases/32-*/32-CONTEXT.md`

## Metadata

**Confidence breakdown:**
- Dead-vs-unwired triage: HIGH — every verdict is grounded in a direct read at the cited file:line; the two flips (T1, T2) and the false-pin-gate (T5) are verified, not inferred.
- Extraction mechanics + import safety: HIGH — source copies read and confirmed identical; cycle/boundary analysis traced.
- Validation Architecture: HIGH — union tables derived from the actual code; the two asymmetric cases (transient errors, agentrender json.Number) are explicitly captured.
- D-12 (memory leg) / Authula source conflict: HIGH — ci.yml job and the existing function read directly.

**Research date:** 2026-06-29
**Valid until:** 2026-07-29 (stable internal codebase; re-verify file:line anchors if other phases touch these files first — Phase 32 is sequenced before 33+).
