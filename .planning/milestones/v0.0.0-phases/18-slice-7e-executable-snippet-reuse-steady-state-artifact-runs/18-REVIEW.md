---
phase: 18
status: clean
reviewed: 2026-06-06
fixed_at: 2026-06-06
depth: standard
files_reviewed: 24
critical_count: 1
warning_count: 5
info_count: 5
fixed:
  - CR-01
  - WR-01
  - WR-02
  - WR-03
  - WR-04
  - WR-05
  - IN-01
  - IN-02
  - IN-03
  - IN-04
  - IN-05
fix_notes: >-
  All Critical + Warnings resolved. WR-02 is now IMPLEMENTED (not
  documented-deferred): internal/toolinvocations/redact.go caps args_raw to 8 KiB
  / result_preview to 2 KiB (UTF-8-boundary-safe) and runs RedactForLedger — a
  credential-pattern table (Authorization/Bearer headers, sk-/sk-or- keys, AKIA
  AWS ids, password=/token=/api_key=/secret= values → [REDACTED]) — at the
  toParams persistence chokepoint, so no secret lands verbatim in the un-deletable
  ledger (commit cfd4c36a). WR-05 + IN-05 folded into the CR-01 fix (tool-boundary
  name trim + grammar validation). IN-04 is now IMPLEMENTED (not deferred): the
  skill loader/writer adapters were extracted into the shared internal/skilladapters
  package importable from both cmd/aura and internal/eval, collapsing the eval-test
  mirror (commit 240edd2d).
---

# Phase 18 Code Review — Slice 7e Executable Snippet Reuse / Steady-State Artifact Runs

**Reviewed:** 2026-06-06
**Depth:** standard (per-file analysis + cross-file trace of the model→writer→FS path)
**Status:** issues-found

## Summary

Reviewed the append-only `aura.tool_invocations` ledger (store + migration + sqlc + runner wiring), the snippet host-path lifecycle (`save_snippet`/`restore`/`archive`/`use`), the `skill` tool action router, the new `aura shell` entry, the composition-root adapters, and the eval registry-parity + steady-state gate.

The ledger design is sound: the migration's append-only triggers + role grants are correct, the `start`/`end` shape CHECK is well-formed, `ON CONFLICT DO NOTHING` makes inserts idempotent, and the runner projects the typed `ToolInvocation` event faithfully. The `ask_user`-only-pause invariant holds (`actionSaveSnippet`/`restore`/`archive` all return normal `ToolResult`s, never the sentinel). The JSON-Schema discipline (property-level `action`/`language` enums, root `required:["action"]` only) is intact. The eval registry mirrors the production `skill` tool with matching actor/ApprovalSource.

One BLOCKER: the model-reachable `archive` action joins an **unsanitized** model-supplied name into a filesystem path and `os.RemoveAll`s it — every other write/use method funnels through `SanitizeName` FIRST, but `Writer.Archive` does not, yielding arbitrary directory deletion via `../` traversal. Plus several warnings, chiefly that a transient ledger-insert failure aborts the user-facing turn even though the ledger is explicitly "observability, not a permission system."

## Critical

### CR-01: `archive` action deletes arbitrary directories — unsanitized model name reaches `os.RemoveAll` (path traversal)

**File:** `internal/skills/writer_activate.go:63-84` (`Writer.Archive`); reached from `internal/agent/tools/skill_write.go:213-224` (`actionArchive`) → `internal/agent/tools/skill_write.go:228-240` (`requireWriteName`) → `cmd/aura/serve_adapters.go:331-336` (`ArchiveSnippet`).

**Issue:** `Writer.Archive` is the ONLY model-reachable write/use method that does **not** call `SanitizeName(name, name)` before joining the name into a path. Compare:
- `Restore` (writer_activate.go:103) — sanitizes first.
- `SetAlways` (writer_activate.go:206) — sanitizes first.
- `UseSnippet` (snippet.go:307) / `SnippetUsage` (snippet_usage.go:153) — sanitize first.
- `Delete` (writer_activate.go:132) — itself unsanitized, but its only caller `WriteMutation` (writer.go:92) runs `ValidateForWrite` → `SanitizeName` upstream, so the delete path is covered.
- `Archive` — **no guard anywhere on the model path.** `actionArchive` → `requireWriteName` only checks `a.Name == ""`; `ArchiveSnippet` passes the raw name straight to `Writer.Archive`.

The model fully controls `a.Name`. With e.g. `{"action":"archive","name":"../../../../some/dir"}`:
- `Dematerialize(name, w.exportDir)` (writer_activate.go:67) → `os.RemoveAll(filepath.Join(exportDir, "../../../../some/dir"))` (materialize.go:100-102) — recursively deletes a tree **outside** the export dir.
- `promoteDir(dstDir, filepath.Join(w.archiveDir, name))` (writer_activate.go:71) → `os.RemoveAll(dst)` on a traversed archive path (writer_activate.go:238), then `os.Rename` of a traversed `activeDir`.

`SanitizeName`'s regex `^[a-z0-9-]{1,64}$` forbids `/`, `\`, and `.`, so a single chokepoint call closes this. The TTL-sweep caller (snippet_usage.go:164) and the integration test pass loader-derived (already-validated) names, which is why this has not surfaced in tests — but the model path is live and unsanitized. This is arbitrary host-directory deletion with the operator's own privileges (the shell_exec trust model amplifies the blast radius).

**Fix:** Add the same chokepoint guard at the top of `Writer.Archive`, mirroring `Restore`/`SetAlways`:
```go
func (w *Writer) Archive(ctx context.Context, name string, src ApprovalSource, actor AuditActor) error {
	if err := SanitizeName(name, name); err != nil {
		return fmt.Errorf("archive %q: %w", name, err)
	}
	...
}
```
Defense-in-depth: also sanitize in `Delete` (so it is self-guarding even if a future caller bypasses `WriteMutation`), and have `requireWriteName` reject a name that fails the grammar before it ever reaches the adapter.

## Warnings

### WR-01: A ledger-insert failure aborts the user-facing turn — observability must not be a turn-killer

**File:** `internal/runner/runner.go:211-214` (the `persistEvent` yield) + `internal/runner/runner_persist.go:69-101` (`persistToolInvocation`).

**Issue:** In the per-round loop, `if perr := r.persistEvent(ctx, tr, ev); perr != nil { yield(nil, perr); return }`. `persistEvent` routes tool-invocation events to `persistToolInvocation`, which returns a hard error on any `r.toolInvocations.Insert` failure (e.g. a transient pg hiccup, pool exhaustion, a context cancel mid-round). That error aborts the entire turn — the user gets no answer — even though migration 0011's own header declares the ledger "operational observability, not a permission system." A forensic/audit sink should be best-effort: a failed ledger write should be logged (`slog.Warn`) and the turn should continue. Contrast `persistAssistantAnswer`, where a failure legitimately must abort because the turn's durable history depends on it; the ledger does not gate `LoadHistory`.

Additionally, `persistToolInvocation` returns a hard error when `r.toolInvocations == nil` (runner_persist.go:70-72). The prod composition root injects it, but this makes the ledger a *required* dependency for any turn that dispatches a tool — a stricter contract than "observability." Consider treating a nil store as a logged no-op rather than a turn-fatal error.

**Fix:** In the loop (or in `persistEvent`), special-case tool-invocation persistence as best-effort:
```go
if ev.Actions.ToolInvocation != nil {
	if err := r.persistToolInvocation(ctx, tr, ev); err != nil {
		slog.Warn("tool invocation ledger insert failed (continuing)", "tool", ev.Actions.ToolInvocation.ToolName, "err", err)
	}
	// fall through; never yield the error
}
```
Keep `persistPause`/`persistAssistantAnswer` failures turn-fatal as today.

### WR-02: `args_raw` persists the full raw tool arguments — secret-leakage surface in the durable ledger

**File:** `internal/agent/llm_agent_events.go:49` & `:80` (`Arguments: call.Function.Arguments`), persisted via `internal/runner/runner_persist.go:85` → `internal/toolinvocations/store.go:121` (`ArgsRaw`).

**Issue:** The ledger stores the verbatim `shell_exec`/`sandbox_exec`/MCP argument JSON (the full command line, any inline env values, tokens, or credentials the model placed on a command line) and the verbatim `result_preview`. The migration grants `aura_app` SELECT, and `ListByConversation` reads it all back. Invariant 2 of the review brief explicitly flags "args/result previews must not leak secrets." There is no redaction, length cap, or allowlist on what lands in `args_raw`/`result_preview`. A `shell_exec` call like `curl -H "Authorization: Bearer sk-..."` is now durable, append-only (un-deletable by design — the triggers reject DELETE), and FK-cascades only on conversation delete. For a single-operator host this is lower-severity, but the append-only immutability makes any captured secret permanent.

**Fix:** At minimum cap `args_raw` to a bounded size (mirroring `preview_bytes`/result spill) and document the leakage surface. Preferably run a lightweight secret-pattern redactor (the same NFKC-fold the blocklist uses is already in-package) over `args_raw`/`result_preview` before persistence, or persist only `args_bytes` + a redacted preview for tools flagged as credential-bearing. If "store everything for forensics" is the deliberate choice, add an explicit code comment + PRD note so it is not a silent leak.

### WR-03: `int4OrNull`/`int8OrNull` silently truncate via unchecked `int32` casts

**File:** `internal/toolinvocations/store.go:120-160` — `int4OrNull` (`int32(n)`), `int8OrNull` (fine, int64), the `Seq: int32(e.Seq)` cast (store.go:116), and `ArgsBytes`/`PreviewBytes`/`ResultBytes` int→int32.

**Issue:** `ArgsBytes`, `PreviewBytes`, and `ResultBytes` are `int` and cast to `int32` with no bounds check. A tool result or argument blob over 2 GiB (or a very large `result_bytes` from a sidecar-spilled payload) overflows `int32` and persists a wrong (possibly negative) byte count — a silent data-corruption of the forensic counts the steady-state gate and any future analytics read. Same for `Seq` if a pathological round emitted >2^31 tool events (not realistic, but the cast is unguarded). The likelihood is low (args are token-bounded), so this is a Warning, not a Blocker.

**Fix:** Clamp before casting, e.g. a helper `clampInt32(n int) int32` that saturates at `math.MaxInt32`, used for the byte-count fields; or widen the columns to `bigint` and the params to `int8` if multi-GiB results are expected.

### WR-04: `snippet use` host frame is not shell-safe on Windows (backslashes + spaces)

**File:** `internal/agent/tools/skill_read.go:123-134` (`renderSnippetUse`) consuming `internal/skills/snippet.go:104-110` (`SnippetHostPath` via `filepath.Join`).

**Issue:** `SnippetHostPath` uses `filepath.Join`, so on Windows the host path is `C:\Users\...\export\name\name.py` (backslashes). `renderSnippetUse` then hands the model `command="python3 C:\Users\...\name.py"` as the literal to run through `shell_exec`. But `shell_exec` runs through **bash `-c`** on Windows (Git Bash, shell_exec.go:252-265), where `\U`, `\n`, etc. are escape sequences and a backslash path is mangled; and an export dir under `C:\Program Files\...` (a space) would word-split into multiple argv. The `%q` in `renderSnippetUse` quotes the Go string for *display*, it does not produce a shell-quoted command the model can paste verbatim. This is guidance the model has to reassemble, so it is a correctness risk (the snippet may fail to run on a Windows host with a spaced or backslashed export dir), not a code-execution bug — hence Warning. The in-sandbox path (`SnippetSandboxPath`, forward-slash `/skills/...`) is unaffected.

**Fix:** When emitting the host frame, normalize to a shell-usable form (`filepath.ToSlash` for the bash-on-Windows surface — Git Bash accepts `/c/Users/...` or forward-slash drive paths) and single-quote the path in the rendered command so spaces are safe, e.g. `command="python3 '/c/Users/.../name.py'"`. Mirror the `filepath.ToSlash` treatment the runner already applies to the announced workspace (runner.go:109).

### WR-05: Inconsistent name normalization across the skill actions (`TrimSpace` vs raw `== ""`)

**File:** `internal/agent/tools/skill_read.go:102` & `:146` (`strings.TrimSpace(a.Name)`) vs `internal/agent/tools/skill_write.go:117` (`a.Name == ""`), `:172` (`a.Name == ""`), `:233` (`requireWriteName`, `a.Name == ""`).

**Issue:** The read actions (`use`/`info`) trim whitespace before the empty check; the write/lifecycle actions (`create`/`update`/`delete`/`save_snippet`/`restore`/`archive`) do a bare `a.Name == ""`. A name of `"   "` therefore passes the write-side guard and is forwarded to the writer. Today the writer's `SanitizeName`/`ValidateForWrite` rejects it downstream — except on the `archive` path (CR-01), where there is no downstream sanitizer, so a whitespace/`../` name flows straight through. Even after CR-01 is fixed, the inconsistency is a latent footgun and a divergence from the established pattern.

**Fix:** Trim consistently and validate the grammar at the tool boundary for all name-bearing actions (a shared helper), so the tool layer never forwards a structurally-invalid name regardless of which downstream method it hits.

## Info

### IN-01: `start`-event insert depends on the emitter always populating `StartedAt`

**File:** `internal/toolinvocations/store.go:118` (`StartedAt: timestamptz(e.StartedAt)`) vs the table CHECK `internal/db/migrations/0011_tool_invocations.up.sql:32-42`.

**Issue:** The `tool_invocations_event_shape` CHECK requires `started_at IS NOT NULL` for a `start` row, but `toParams` writes `started_at` from `e.StartedAt` directly (not the computed `ts` fallback used for the `ts` column). If a future emitter ever sends a `start` event with a zero `StartedAt`, the insert fails the CHECK rather than degrading gracefully. The current emitter (`toolStartEvent`, llm_agent_events.go:40-54) always sets it, so this is latent only.

**Fix:** Default `StartedAt` to the resolved `ts` for `start` events in `toParams` (it already computes `ts`), so the column is never NULL for a start fact.

### IN-02: `eventFromRow` swallows a malformed-meta unmarshal error

**File:** `internal/toolinvocations/store.go:211-213` (`_ = json.Unmarshal(r.Meta, &out.Meta)`).

**Issue:** A corrupt `meta` jsonb silently yields a nil/partial map with no signal. For an append-only forensic ledger, a read-path decode failure is worth a `slog.Warn`. Low impact (meta is written by the same package), hence Info.

**Fix:** Log on unmarshal error rather than discarding it.

### IN-03: `anyFloat` does not handle `int64` / `json.Number`, unlike `anyInt`

**File:** `internal/runner/runner_persist.go:331-340` (`anyFloat`) vs `:318-329` (`anyInt`).

**Issue:** `anyInt` handles `int`/`int64`/`float64`; `anyFloat` handles only `float64`/`int`. The StateDelta is decoded with `UseNumber()` in some paths (event.go:194) which yields `json.Number`, not `float64`. If `cost_usd` ever arrives as a `json.Number` or `int64`, `anyFloat` returns `(0, false)` and the cost falls back to the price table. This pre-dates Phase 18 (usage path), but it sits in a touched file. Info.

**Fix:** Mirror `anyInt`'s type set in `anyFloat`, including a `json.Number` case.

### IN-04: Eval adapter duplication acknowledged but unresolved (deep-refactor-on-touch)

**File:** `internal/eval/skills_snippet_reuse_registry_cot_eval_test.go:70-147` duplicates `cmd/aura/serve_adapters.go:363-403` & `:287-336`.

**Issue:** `evalSkillLoaderAdapter`/`evalSkillWriterAdapter` are byte-for-byte mirrors of the production `skillLoaderAdapter`/`skillWriterAdapter`. The file header (lines 10-20) explicitly defers the extraction to a STATE.md follow-up because `cmd/aura` is unimportable from `internal/eval` and a parallel session was editing it. This is the project's CLAUDE.md "DEEP REFACTOR ON TOUCH / no duplication" rule being knowingly deferred. The parity guard (`TestRegistrySnippetReuse_HasSkillTool`) mitigates drift risk. Flagging so the follow-up is tracked, not lost.

**Fix:** Extract a shared `internal/skilladapters` package importable from both `cmd/aura` and `internal/eval` when `serve_adapters.go` is next touched (as the header commits to).

### IN-05: `requireWriteName` / `actionSaveSnippet` error messages omit which actions need which fields

**File:** `internal/agent/tools/skill_write.go:159-191` (`actionSaveSnippet`).

**Issue:** Minor UX: the per-field required errors are good, but a model that sends `save_snippet` with `code` but no `language` gets "language is required (one of python, shell, js)" — fine — yet the `restore`/`archive` `requireWriteName` errors are generic "name is required" without the [a-z0-9-] grammar hint the schema description carries. Aligning the error text with the schema's grammar note would speed model self-correction. Cosmetic.

**Fix:** Include the name-grammar hint in the `requireWriteName` empty/invalid-name error.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
