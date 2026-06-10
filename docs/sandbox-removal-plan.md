# Sandbox Removal — File-by-File Execution Plan

Decision (final): remove the local container sandbox entirely (`sandbox-agent` on
`:2468`, the `sandbox_exec` tool, the `internal/sandboxagent` client). The host full
terminal (`shell_exec`) is THE execution surface. Skill snippets (Slice 7e / Phase 18)
keep working but execute on the HOST via `os/exec` on the materialized export-dir file.

Scope: Go source under `cmd/` + `internal/`, `compose.yaml`, `docker/sandbox-agent/`,
`Makefile`, `.github/workflows/*.yml`, `.env.example`, `README.md`, `docs/system_prompt.txt`,
`internal/agent/prompt.go`. Everything under `.planning/` (incl. `.planning/spikes/`),
`docs/design/`, `docs/audit/` is OUT OF SCOPE (historical) and intentionally left untouched
— note that `.planning/spikes/005`, `006`, `012a` import `internal/sandboxagent`, so
deleting the package will break those spike `main.go` builds. They are excluded from
`go build ./...` only if they are not in the module build set — see RISKS #1.

---

## 1. ENUMERATION (in-scope hits)

### DELETE-FILE (whole file is sandbox-only)

| File | What it is |
|---|---|
| `internal/sandboxagent/client.go` | The sandbox-agent HTTP client (`New`, `Run`, `Health`). Sandbox-only. |
| `internal/sandboxagent/client_test.go` | Unit tests for the client. |
| `internal/sandboxagent/client_live_integration_test.go` | `//go:build sandbox_integration` live HTTP tests. |
| `internal/agent/tools/sandbox_exec.go` | The `sandbox_exec` tool (`SandboxExec`, `sandboxRunner`, `sandboxUnavailable`). |
| `internal/agent/tools/sandbox_exec_test.go` | Unit tests (`fakeSandboxRunner`, `TestSandboxExec*`). |
| `internal/agent/tools/sandbox_exec_live_integration_test.go` | `//go:build sandbox_integration` live tool E2E. |
| `docker/sandbox-agent/Dockerfile` | The only file in `docker/sandbox-agent/`; delete the directory. |

After deleting `internal/sandboxagent/*`, the directory is empty → the package vanishes.
After deleting the three `sandbox_exec*` files, `internal/agent/tools` keeps every other tool.

### EDIT (remove sandbox parts, keep the rest)

| File | Sandbox refs (line) | Action |
|---|---|---|
| `cmd/aura/main.go` | import `internal/sandboxagent` (31); `reg.Register(&tools.SandboxExec{Runner: sandboxagent.New(cfg.SandboxAgent)})` (116); comment (117-119) | Remove import + register line; fix comment. |
| `cmd/aura/skills_snippet.go` | import (17); `sandboxagent.New(env.cfg.SandboxAgent)` + `use.SandboxPath` (108-113); package/func doc (1-8, 85-90) | Rewire to HOST `os/exec` (see §3). |
| `internal/config/config.go` | import (25); `SandboxAgent sandboxagent.Config` field (44); `SandboxAgent: sandboxagent.Config{...}` block (222-225); stale comment (57-60) | Remove import + field + block + comment. |
| `internal/scoring/scoring.go` | `SandboxArgs` (30-36), `onlyPyPI` (87-100), `ComputeSandboxTier` (101-113) | Delete those 3 symbols; KEEP everything else. |
| `internal/scoring/scoring_test.go` | `ComputeSandboxTier` subtest (16-42) | Delete that one `t.Run` block; keep the rest. |
| `internal/agent/prompt.go` | `<machine>` line: "Run UNTRUSTED or model-generated code in the isolated sandbox tool …" (line 68 of the const) | Remove/replace that bullet; keep `shell_exec`. |
| `docs/system_prompt.txt` | same bullet (line 49) | Edit byte-identically with prompt.go (a test enforces sync). |
| `cmd/aura/chat_test.go` | `TestBuildChatRegistry_RegistersSandboxExec` (255-263) | DELETE the test. |
| `cmd/aura/registry_test.go` | `TestBuildRegistry_RegistersSandboxExec` (44-49); `SandboxAgent: config.LoadDB().SandboxAgent` (83, 117) | DELETE the test; remove the two field lines. |
| `internal/config/config_test.go` | `TestLoad_SandboxAgentDefaultsAndOverrides` (112-132) | DELETE the test. |
| `internal/skills/snippet_integration_test.go` | whole file: `sandbox_integration` tag + sandboxagent client | Rewrite to HOST exec, drop tag (see §3). |
| `compose.yaml` | `aura-sandbox-agent` service (161-199); `aura-sandbox-agent:` volume (339); `aura-sandbox-local:` network (343-346) | Remove service + volume + network. |
| `Makefile` | `sandbox-up` in `.PHONY` (9); help echo (38); `sandbox-up:` target (134-139) | Remove all three. |
| `.github/workflows/skills.yml` | many (see §5) | Remove the sandbox build/start/exec steps + tags; re-point SC#4 to host. |
| `.env.example` | `AURA_SANDBOX_*` block (37-42) | Delete the block. |
| `README.md` | "3. **Sandbox** — …" (16) | Replace/remove the Sandbox scope bullet. |

### COMMENT-ONLY (stale "sandbox" wording; NOT functional — optional cleanup, flag only)

These mention "sandbox" in prose but wire nothing to the container. They do NOT block the
build. Touch them only if doing a deep refactor-on-touch pass (CLAUDE.md), else leave:

- `internal/skills/loader.go:42,56` — "self-installed skills land on disk via the sandbox CLI" (means the npx host CLI, not the container).
- `cmd/aura/serve_adapters.go:244,267` — "find-skills … via the sandbox CLI" / "in-sandbox `cd /skills && npx skills add`".
- `cmd/aura/skills.go:7` — "always-on skill driving `npx skills` in the sandbox".
- `internal/agent/tools/spec.go:38` — "mutate the sandbox" (generic).
- `internal/agent/tools/shell_exec.go:20-21` — "Untrusted, model-generated code still has the deliberate sandbox_exec escalation" (now false; recommend updating since this file is the host surface).
- `internal/agent/tools/main_test.go:11` — goleak comment "(sandbox/session-bound)".
- `internal/eval/classify_cot_eval_test.go` + `skills_cot_eval_test.go` + `skills_xlsx_verify_cot_eval_test.go` — these ASSERT `sandbox_exec` is ABSENT (e.g. `classify_cot_eval_test.go:301,342`); they stay GREEN after removal and are positive evidence the eval registry is already host-only. Do NOT change them.

---

## 2. SCORING — delete-vs-keep per symbol (grep-verified repo-wide)

`internal/scoring/scoring.go` is SHARED. Only the sandbox-tier surface is removed.

| Symbol | Verdict | Evidence |
|---|---|---|
| `SandboxArgs` | **DELETE** | Only callers: scoring.go (`ComputeSandboxTier`) + scoring_test.go. No other package. |
| `ComputeSandboxTier` | **DELETE** | Only callers: scoring.go (none — it's the entry) + scoring_test.go. Zero production callers (grep `ComputeSandboxTier` → scoring.go + scoring_test.go only). |
| `onlyPyPI` | **DELETE** | Only caller: `ComputeSandboxTier`. grep `onlyPyPI` → scoring.go only (rest are `.planning/` + `docs/audit/`, out of scope). |
| `RiskTier` (type + `Safe/Normal/Risky/Destructive`) | **KEEP** | Used by `internal/agent/tools/task.go`, `internal/cron/dispatch.go`, `internal/skills/*`, `cmd/aura/task.go`, `cmd/aura/serve.go`, `cmd/aura/serve_adapters.go`. |
| `GateRecommended` | **KEEP** | Used by `cmd/aura/task.go:109`, `internal/agent/tools/task.go:195`, `internal/skills/writer.go:90`, `snippet_test.go`. (DB column `gate_recommended` is unrelated, stays.) |
| `ComputeTaskTier`, `TaskArgs`, `baseTaskTier`, `taskModifierBumps` | **KEEP** | cron + task tool. |
| `ComputeSkillTier`, `SkillAction`, `Skill*` consts | **KEEP** | skills writer + snippet. |
| `RequiresImmediateAlert` | **KEEP** | cron dispatch + task tool. |
| `rank`, `bumpTier`, `tierOrder`, `destructiveKeyword` | **KEEP** | shared internals of the kept functions. |

Edit details for `scoring.go`:
- Delete `type SandboxArgs struct {…}` (lines 30-36).
- Delete `func onlyPyPI(hosts []string) bool {…}` (lines 87-100).
- Delete `func ComputeSandboxTier(a SandboxArgs) RiskTier {…}` (lines 101-113).
- Update the package doc (line 1-7): drop "and sandbox calls" so the comment stays truthful.
- `strings` import: still used by `destructiveKeyword`? No — `destructiveKeyword` uses `regexp`. `strings` is used ONLY by `onlyPyPI` (`strings.ToLower`). **After deleting `onlyPyPI`, remove the `strings` import** or `go build` fails (unused import). Verify: grep `strings\.` in scoring.go → only line 93 (`strings.ToLower` inside onlyPyPI). So `strings` becomes unused → drop it. `regexp` stays (destructiveKeyword).

Edit details for `scoring_test.go`:
- Delete the `t.Run("ComputeSandboxTier", …)` block (lines 16-42). Keep `ComputeTaskTier`, `ComputeSkillTier`, `GateRecommended`, `RequiresImmediateAlert`, and the rapid property block.

---

## 3. SNIPPET HOST-EXEC REWIRE (the critical dependency)

### Current state (already host-ready)
The host machinery ALREADY EXISTS:
- `internal/skills/snippet.go` `UseSnippet` returns `SnippetUse{ HostPath, SandboxPath, Interpreter, … }`. `HostPath` = `filepath.Join(exportDir, name, name+"."+ext)` via `SnippetHostPath` (snippet.go:97-110, 326-334).
- The materialized file is REAL on the host: `Writer.Activate` → `Materialize(name, dstDir, w.exportDir)` copies the active skill tree (incl. `<name>.<ext>`) into `exportDir/<name>/` (materialize.go:22-37). So `HostPath` points at an actual on-disk file under `$AURA_SKILL_EXPORT_DIR` (default `~/.aura/skills/export`).
- `SnippetHostInvocation(name, language, exportDir)` (snippet.go:129-144) already resolves `(hostPath, interpreter)`.
- `StampUsage` (snippet_usage.go:94-105) is host-side already.

### Model-facing path — NO rewiring needed
Post-removal the model runs an active snippet via `shell_exec` on the host: `action=use`
returns `HostPath` + interpreter; the model issues `python3 <HostPath>`. The only required
change is REMOVING `sandbox_exec` from the registry (§1 main.go) and the prompt (§4). The
system prompt already teaches "A snippet skill returns a stable path: run it BY PATH with
the interpreter (e.g. python3 <path>)" (prompt.go §skills). No prompt change for snippets.

### Operator CLI rewire — `cmd/aura/skills_snippet.go` `skillsSnippetExec`
Replace the sandboxagent call (lines 108-117) with a host `os/exec` on `use.HostPath`:

```go
// imports: drop "github.com/chetto1983/aura/internal/sandboxagent"
// add: "os/exec", "bytes"

use, err := env.w.UseSnippet(name)
if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

runArgs := append([]string{use.HostPath}, extra...)
cmd := exec.CommandContext(ctx, use.Interpreter, runArgs...)
var stdout, stderr bytes.Buffer
cmd.Stdout, cmd.Stderr = &stdout, &stderr
runErr := cmd.Run()

if stdout.Len() > 0 { fmt.Print(stdout.String()) }
if stderr.Len() > 0 { fmt.Fprint(os.Stderr, stderr.String()) }

exit := 0
if runErr != nil {
    var ee *exec.ExitError
    if errors.As(runErr, &ee) {
        exit = ee.ExitCode()
    } else {
        fmt.Fprintf(os.Stderr, "snippet exec %q: %v\n", name, runErr)
        os.Exit(1)
    }
}
if exit == 0 {
    if serr := env.w.StampUsage(name, time.Now()); serr != nil {
        fmt.Fprintf(os.Stderr, "snippet exec %q: usage stamp failed: %v\n", name, serr)
    }
}
if exit != 0 { os.Exit(exit) }
```

- Keep `StampUsage` exactly (the D-19 deterministic stamp).
- Add `"errors"` to imports (for `errors.As`); add `os/exec`, `bytes`; drop `sandboxagent`.
- Update the package doc (lines 1-8) and `skillsSnippetExec` doc (85-90): replace
  "through the sandbox_exec seam (python3 /skills/<name>/<name>.py …) … calls the same
  sandboxagent client" with "on the HOST by path (`<interpreter> <export-dir path>`) via
  os/exec" — the host export-dir file is the materialized snippet.

### `UseSnippet` change needed
NONE required for the host path — `HostPath` is already returned. The MINIMAL change keeps
`SandboxPath`/`SnippetSandboxPath`/`SnippetInvocation`/`inSandboxSkillsRoot` as vestigial
(harmless; still unit-tested). The CLI just consumes `use.HostPath` instead of
`use.SandboxPath`.

OPTIONAL deeper strip (flag as separate follow-up — touches 4 files, larger blast radius):
remove the now-dead sandbox-path surface from `snippet.go` (`inSandboxSkillsRoot`,
`SnippetSandboxPath`, `SnippetInvocation`, the `SandboxPath` field of `SnippetUse`, and the
`renderSnippetDocs`/doc mentions of "/skills"). This forces edits to:
- `internal/skills/snippet_test.go` (`TestSnippetSandboxPath`, `TestSnippetInvocationResolvesInterpreter`, the `SandboxPath` asserts at 196-198),
- `internal/skills/smoke_test.go` (143-153, `u1.SandboxPath`),
- `internal/skills/snippet_integration_test.go` (already being rewritten),
- `.planning/spikes/012a-discovery-skill-driven/main.go:450` (out of scope — would break the spike build; another reason to PREFER the minimal path).

**Recommendation: do the MINIMAL rewire now (CLI → HostPath, delete sandbox_exec wiring).
Defer the snippet.go sandbox-path strip to a follow-up so the spike builds and the existing
unit/smoke tests stay green with zero churn.**

### Skills tests touching the sandbox path

| Test file | Build tag | Verdict |
|---|---|---|
| `snippet_integration_test.go` | `sandbox_integration && db_integration` | **REWRITE**: drop `sandbox_integration` tag → `//go:build db_integration`; drop `sandboxagent` import + the `AURA_SANDBOX_AGENT_URL` `envOrSkip`; after `Activate`, run `exec.CommandContext(ctx, use.Interpreter, use.HostPath)` on the host, assert marker in stdout + exit 0, then `StampUsage`. Keep the `migratedPool` + usage-stamp assertions. Remove the http.DefaultTransport goleak cleanup (no HTTP client anymore). |
| `snippet_test.go` | none (unit) | **KEEP** under minimal path (still references `SandboxPath`/`SnippetSandboxPath`, which survive). Only edit under the optional deep strip. |
| `snippet_restore_integration_test.go` | `db_integration` | **KEEP unchanged**. Its `GateRecommended` is the audit-row field, NOT scoring; no sandbox coupling. |
| `snippet_sweep_integration_test.go` | `db_integration` | **KEEP unchanged**. Same — audit-row `GateRecommended`, TTL sweep, no sandbox. |
| `smoke_test.go` | none (unit) | **KEEP** under minimal path (references `u1.SandboxPath`). Edit only under the deep strip. |

---

## 4. PROMPT EDITS (must be byte-identical across the two files)

A test (`internal/agent/prompt_test.go` `TestPrompt_DocSyncByteIdentical`, lines 77-87)
asserts `SystemPrompt` const == `docs/system_prompt.txt` (modulo one trailing newline).
Edit BOTH together.

Target line (prompt.go const ≈ line 68; system_prompt.txt line 49), inside `<machine>`:
```
- Run UNTRUSTED or model-generated code in the isolated sandbox tool -- a deliberate escalation, never the default.
```
Remove it (the host terminal is now the only surface) OR replace with a non-sandbox safety
note, e.g.:
```
- Treat model-generated code as untrusted: review it before running, and prefer a scratch directory you can clean up.
```
No other prompt test breaks: `TestPrompt_NoTimestamp`, `TestPrompt_MechanismNotEnumeration`
(only forbids `read_tool_output`/`current_time`), `TestPrompt_ShellTransactionDoctrine`,
`TestPrompt_NoSupersededSkillRouting` (forbids `action=catalog`/`action=install`) all stay
green. Keep `shell_exec`, `tool_search`, `text_response`, `ask_user`, the `<skills>` section.

---

## 5. INFRA / CONFIG EDITS

### `compose.yaml`
- Delete the `aura-sandbox-agent:` service block (lines 161-199, including the 6-line
  preceding comment).
- Delete `aura-sandbox-agent:` from the `volumes:` map (line 339).
- Delete the `aura-sandbox-local:` network (lines 343-346) under `networks:`.
- VERIFIED: NO other service has `depends_on: aura-sandbox-agent` (the only `depends_on` is
  at line 104 and is unrelated). The `${AURA_SKILL_EXPORT_DIR…}:/skills` bind mount lived
  ONLY on the sandbox service — it disappears with the service (the host writer still
  materializes into `$AURA_SKILL_EXPORT_DIR`; nothing else mounts it).

### `docker/sandbox-agent/`
- Delete the whole directory (only `Dockerfile`).

### `Makefile`
- `.PHONY` line 9: remove the trailing `sandbox-up`.
- Help echo line 38: remove `@echo "make sandbox-up …"`.
- Delete the `sandbox-up:` target (lines 134-139).
- VERIFIED: no `sandbox-down` target exists; nothing else depends on `sandbox-up`.

### `.github/workflows/skills.yml`
- Job name (line 35) + header comments (1-18): drop the `sandbox_integration` wording.
- `env` (47-60): remove `AURA_SANDBOX_AGENT_URL` (57). KEEP `AURA_SKILL_EXPORT_DIR` (it is
  now the host materialization dir the rewritten SC#4 test runs from) — update its comment
  (58-59) to drop the "/skills mount" framing.
- Delete the step "Build the sandbox-agent image" (97-101).
- Delete the step "Start the sandbox-agent (--no-token + /skills mount)" (103-104).
- Step "sandbox_integration tier (SC#4 …)" (106-110): RE-POINT to host. Change to a
  `db_integration` run of the rewritten test:
  `go test -tags db_integration -race -count=1 -p 1 -run TestSnippetExec ./internal/skills/`
  (it now execs the snippet on the runner's host `python3` — ubuntu-latest has python3).
  Fold it into the existing `db_integration` step (95) or keep it as a named SC#4 step
  without the sandbox build/start prerequisites.
- Coverage gate `env` (155-156): change `AURA_COVERAGE_TAGS: "db_integration sandbox_integration"`
  → `AURA_COVERAGE_TAGS: "db_integration"`.

### `.github/workflows/ci.yml`
- VERIFIED: ZERO sandbox references. No change needed.

### `.env.example`
- Delete the block lines 37-42 (the `# ---- Local sandbox container ----` header,
  `AURA_SANDBOX_AGENT_URL`, `AURA_SANDBOX_AGENT_TIMEOUT_SEC`, the port comment,
  `AURA_SANDBOX_AGENT_PORT`).

### `README.md`
- Line 16 scope bullet "3. **Sandbox** — …": remove it, or replace with a host-terminal
  bullet (e.g. "3. **Host terminal** — full `shell_exec` on the host; model-generated code
  runs in the operator's environment, not a container."). Renumber bullet 4 (Swarm) if you
  keep a 4-item list, or leave numbering and adjust prose.

---

## 6. ORDERED EXECUTION PLAN (keeps `go build` / `go vet` green at the END)

Steps that MUST land together to avoid an intermediate broken build are grouped.

1. **Config + its readers (atomic group).** Edit `internal/config/config.go` (drop import,
   `SandboxAgent` field, init block, comment) TOGETHER WITH the deletions/edits of every
   reader of that field, or the build breaks on the missing field:
   - `cmd/aura/main.go` (drop import + the `SandboxExec` register line + fix comment),
   - `cmd/aura/skills_snippet.go` (host rewire, §3),
   - `cmd/aura/registry_test.go` (drop the two `SandboxAgent:` lines + the `RegistersSandboxExec` test),
   - `cmd/aura/chat_test.go` (drop the `RegistersSandboxExec` test),
   - `internal/config/config_test.go` (drop `TestLoad_SandboxAgentDefaultsAndOverrides`).
   At the end of this group nothing references `cfg.SandboxAgent` or `tools.SandboxExec`.

2. **Delete the tool (atomic with step 1's main.go edit).** Delete
   `internal/agent/tools/sandbox_exec.go`, `sandbox_exec_test.go`,
   `sandbox_exec_live_integration_test.go`. (Safe once main.go no longer registers it —
   do in the same commit as step 1.)

3. **Delete the client package (atomic with steps 1-2).** Delete
   `internal/sandboxagent/client.go`, `client_test.go`, `client_live_integration_test.go`.
   The package has NO remaining in-scope importer after steps 1-2. (Out-of-scope
   `.planning/spikes/*` importers — see RISKS #1.)

4. **Scoring.** Edit `internal/scoring/scoring.go` (delete `SandboxArgs`, `onlyPyPI`,
   `ComputeSandboxTier`; drop the now-unused `strings` import; fix the package doc) TOGETHER
   WITH `internal/scoring/scoring_test.go` (delete the `ComputeSandboxTier` subtest).
   Independent of steps 1-3 but do it in the same PR.

5. **Snippet integration test rewrite.** Rewrite
   `internal/skills/snippet_integration_test.go` to the host-exec form (§3): drop the
   `sandbox_integration` tag + `sandboxagent` import, exec on `use.HostPath`.

6. **Prompt.** Edit `internal/agent/prompt.go` AND `docs/system_prompt.txt` byte-identically
   (§4). `TestPrompt_DocSyncByteIdentical` enforces sync — run `go test ./internal/agent/`.

7. **Infra.** `compose.yaml`, `docker/sandbox-agent/` (rm -rf dir), `Makefile`,
   `.github/workflows/skills.yml`, `.env.example`, `README.md` (§5). No Go build impact;
   land in the same commit for atomicity.

8. **Validate.** Run, in order:
   - `go build ./...`
   - `go vet ./...`
   - `go test ./internal/scoring/ ./internal/config/ ./internal/agent/ ./internal/skills/ ./cmd/aura/`
   - `go test -race ./internal/agent/tools/` (goleak TestMain still green)
   - `go test -tags db_integration -race -count=1 -p 1 -run TestSnippetExec ./internal/skills/` (the rewritten host SC#4, with the DB stack up + python3 on PATH)
   - Confirm `go test -tags sandbox_integration ./...` now finds NO `sandbox_integration` files (the tag is fully retired).

9. **Optional deep strip (separate commit, flagged).** If desired, remove the vestigial
   sandbox-path surface from `internal/skills/snippet.go` and update `snippet_test.go` +
   `smoke_test.go`. This will break `.planning/spikes/012a/main.go:450` — only do it if the
   spike is excluded from the build (RISKS #1) or you accept the spike breakage.

---

## 7. RISKS / OPEN QUESTIONS

1. **`.planning/spikes/*` import the deleted package (RESOLVED — safe).**
   `.planning/spikes/005/main.go`, `006/main.go`, `012a/main.go` import
   `internal/sandboxagent` and `012a` also uses `tools.SandboxExec` + `cfg.SandboxAgent` +
   `skills.SnippetInvocation`. VERIFIED `go list ./...` does NOT include any `.planning`
   path (run: `go list ./... | grep planning` → empty). So `go build ./...` / `go vet ./...`
   do NOT compile the spikes, and deleting the package/symbols does NOT break the module
   build. The spike `main.go` files become individually un-buildable, but they were already
   excluded from the build set — acceptable per the IGNORE-`.planning/` scope. No action
   needed. (Only the OPTIONAL deep snippet.go strip in step 9 would additionally invalidate
   `012a:450`'s `SnippetInvocation` call — still harmless for the same reason.)

2. **Snippet host exec on Windows (UNCERTAIN — `python3` availability).** The CLI rewire and
   the rewritten integration test call `use.Interpreter` (`python3`/`sh`/`node`) via
   `os/exec` on the host. On Windows `python3`/`sh` may not be on PATH (the container always
   had them). The model path is unaffected (it picks the interpreter that exists). For the
   CI SC#4 test on ubuntu-latest, `python3` is present. For local Windows operator runs of
   `aura skills snippet exec`, a shell/python may be missing — acceptable (operator's host),
   but note it: the old sandbox guaranteed the toolchain; the host does not.

3. **`StampUsage` on non-zero exit (behavior parity — confirm intended).** The current CLI
   stamps usage only on exit 0. The rewire preserves this. But `exec.ExitError` vs a spawn
   failure (interpreter not found) must be distinguished: a missing interpreter is `exit !=
   ExitError` → should NOT be treated as "exit code" and should NOT stamp. The §3 snippet
   handles this via `errors.As(&exec.ExitError)`. Verify this matches the desired operator
   UX (spawn failure → stderr + exit 1, no stamp).

4. **Minimal vs deep snippet.go strip (decision needed).** The plan recommends MINIMAL
   (keep vestigial `SandboxPath`). If the team wants zero dead code (CLAUDE.md
   "deep-refactor-on-touch"), the deep strip is the alternative but enlarges blast radius to
   `snippet_test.go` + `smoke_test.go` + the spike. Pick one before executing step 9.

5. **Coverage floor (≥85%) after removing the `sandbox_integration` coverage tag.** The
   coverage gate currently folds `sandbox_integration` into `internal/skills` coverage. The
   rewritten host SC#4 runs under `db_integration`, so its lines stay covered. But the
   deleted `internal/sandboxagent` package (previously covered by its unit+live tests) and
   the deleted `sandbox_exec.go` drop OUT of the owned surface entirely (deleted code isn't
   measured), which generally HELPS the ratio. Re-run `scripts/coverage_gate.sh` with
   `AURA_COVERAGE_TAGS="db_integration"` to confirm ≥85% holds. UNCERTAIN until measured.

6. **README scope numbering.** Removing scope bullet 3 leaves "4. Swarm" — decide whether to
   renumber or rename bullet 3 to the host terminal. Cosmetic, no test.
