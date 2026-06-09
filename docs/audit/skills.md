# Audit: internal/skills

**Verdict:** needs-work — one high-severity not-wired defect (ResumeHandler never activated in REPL), one medium bug (orphan dir from post-Archive SetUsageStatus), several low not-wired/dead-code exports, and a doc mismatch.

**Counts:** critical 0 / high 1 / medium 2 / low 7

---

## Findings

### [HIGH][NOT-WIRED] ResumeHandler never called in REPL skill-approval flow

**Location:** `internal/skills/resume.go:19-56`, `internal/runner/runner_resume.go:68-84`

**Confidence:** high

**Detail:** `skills.NewResumeHandler` / `ResumeHandler.Resume` exist to translate a human "yes" answer to a `KindApproval` pause into `Writer.Activate` — this is the D-03 REPL activation channel. But `runner.SubmitAnswer` → `injectAnswer` only appends the answer as a `RoleTool` turn and drives the next `Turn(convID, nil)`. There is no hook anywhere in the runner to dispatch to `ResumeHandler.Resume` on `KindApproval` + `ActionAccept`. A skill that fires `ErrAwaitingUserInput{Kind: KindApproval}` in the REPL will pause, the user will answer "y", the answer is injected as context, the conversation continues — but the skill remains in `pending/` and never activates. The operator must run `aura skills approve <name>` out-of-band; the REPL "y" answer is a no-op for activation. `NewResumeHandler` is referenced only from `resume_integration_test.go`.

**Suggested fix:** Wire `ResumeHandler` into `runner.Deps` as an optional `SkillApprover interface { ApproveSkill(ctx, name, pausedToken string, actor AuditActor) error }`. In `injectAnswer`, detect `pending.Kind == KindApproval && resp.Action == ActionAccept` and call `deps.SkillApprover.ApproveSkill(...)` before the conversation injection. For decline, call `writer.DiscardPending`. This closes the loop that `skills_write.go:42` already describes.

---

### [MEDIUM][BUG] SetUsageStatus recreates orphan active dir after Archive moves it

**Location:** `internal/skills/snippet_usage.go:135-172` (SweepExpiredSnippets, lines 164-168), `internal/skills/snippet_usage.go:56-88` (writeUsageAtomic, line 58)

**Confidence:** high

**Detail:** In `SweepExpiredSnippets`, after a successful `Archive(ctx, name, ApprovalAuto, actor)` call (which runs `promoteDir(activeDir/name, archiveDir/name)` — moving the entire dir including `.usage.json`), the code calls `_ = w.SetUsageStatus(name, "archived")`. `SetUsageStatus` calls `ReadUsage(name)` which reads `activeDir/name/.usage.json`; that path no longer exists, so `ReadUsage` returns a zero-value `UsageSidecar{Status: "active"}` without error (by design — missing sidecar is not an error). Then `writeUsageAtomic` calls `os.MkdirAll(filepath.Join(w.activeDir, name), 0o750)`, **recreating the now-empty active dir**, and writes a `.usage.json` there. Result: `activeDir/name/` is recreated as a ghost directory containing only `.usage.json`. The loader scans it next refresh, tries `loadSkillDir`, finds no `SKILL.md`, logs a warning, and skips it — but the orphan dir persists on disk until manually removed. On subsequent sweeps, `snippetIsStale` reads `activeDir/name/SKILL.md` → not found → `ok=false` → skip, so the ghost is permanently orphaned.

**Suggested fix:** Remove the `_ = w.SetUsageStatus(name, "archived")` call from `SweepExpiredSnippets` (line 168). The sidecar moved with the skill dir into `archiveDir/name/`; if `snippetIsStale` needs to confirm "already archived", it should read from `archiveDir` not `activeDir`. Alternatively, if the sidecar is still needed post-archive, update `SetUsageStatus` / `writeUsageAtomic` to accept an explicit dir path rather than always using `activeDir`.

---

### [MEDIUM][NOT-WIRED] WriteInstallPending has no callers in the production Go build

**Location:** `internal/skills/writer.go:134-187`

**Confidence:** high

**Detail:** `WriteInstallPending` is defined as an exported method but searched across every `.go` file in the repo, the only references are its own definition. The planning docs (`.planning/phases/11-skills/11-06-SUMMARY.md`) document it as the installer's pending+audit sink, but the installer (Slice 11-06) has not been built. Additionally, the method accepts a `body string` parameter (signature: `fm Frontmatter, body, stagedDir, hash string`) that is never read inside the function — all file content comes from `copyTreeNoSymlinks(stagedDir, tmp)`; the SKILL.md inside `stagedDir` is what lands in `pending/`.

**Suggested fix:** Either (a) build the installer and wire `WriteInstallPending`, or (b) drop the dead `body` parameter from the signature to prevent caller confusion. If keeping as a forward stub, add a build-tag or a clear `// not yet called` note.

---

### [LOW][BUG] yamlScalar does not escape embedded newlines in double-quoted scalars

**Location:** `internal/skills/writer.go:271-295`

**Confidence:** medium

**Detail:** `yamlScalar` checks for `\n` in a description and sets `needsQuote = true`, but the escaping loop (lines 287-292) only escapes `"` and `\`. A bare newline inside a YAML double-quoted scalar is subject to YAML line-folding: the parser replaces it with a single space. A description containing `\n` (e.g., `"Does X\nand Y"`) will be serialized as a double-quoted scalar with a literal newline and parsed back as `"Does X and Y"` — silently losing the linebreak. `ValidateForWrite` does not enforce single-line descriptions, so this is reachable.

**Suggested fix:** In the escaping loop, add `case s[i] == '\n': out = append(out, '\\', 'n'); continue` and similarly for `\r` → `\r`. This matches YAML 1.2 double-quoted escape sequences.

---

### [LOW][NOT-WIRED] ValidateNameAgainstDir has no production callers

**Location:** `internal/skills/validator.go:108-115`

**Confidence:** high

**Detail:** `ValidateNameAgainstDir` is documented as "the installer's name+dir chokepoint" but the installer (Slice 11-06) does not exist yet. Searching all `.go` files: the only references are the definition and `validator_test.go:194-199`. No production code imports it.

**Suggested fix:** Either build the installer that uses it, or unexport it as `validateNameAgainstDir` until the installer lands (keeping the test as an internal test).

---

### [LOW][NOT-WIRED] SnippetInvocation has no callers in the production module build

**Location:** `internal/skills/snippet.go:112-127`

**Confidence:** high

**Detail:** `SnippetInvocation` (sandbox-path resolver) is referenced only in `snippet_test.go` and `.planning/spikes/012a-discovery-skill-driven/main.go` (a `package main` outside the module build path). Production code uses `SnippetHostInvocation` (added in Phase 18-02). The 18-02-SUMMARY.md notes it is kept "for the sandbox_exec escalation path" but no production escalation path calls it.

**Suggested fix:** Keep it with a `// sandbox_exec escalation path — wired in Phase 12 AG-UI gateway` comment so intent is clear, or unexport it until the escalation path is built.

---

### [LOW][DEAD-CODE] InsertAuditTx exported but has no external callers

**Location:** `internal/skills/audit_store.go:159-171`

**Confidence:** high

**Detail:** `InsertAuditTx` is exported but only called within `internal/skills` (by `writer.go`, `writer_activate.go`, `snippet.go`). No package outside `internal/skills` imports it. It is designed for tx-bound audit inserts, which is an internal concern.

**Suggested fix:** Unexport as `insertAuditTx`. Update the three internal callers.

---

### [LOW][DEAD-CODE] SnippetCodeFile exported but has no external callers

**Location:** `internal/skills/snippet.go:75-84`

**Confidence:** high

**Detail:** `SnippetCodeFile` is exported but only called from within `snippet.go` (lines 90, 105). No external package references `skills.SnippetCodeFile`.

**Suggested fix:** Unexport as `snippetCodeFile`. Update the two internal callers.

---

### [LOW][BUG] DiscardPending doc says gate_taken=false but code records true

**Location:** `internal/skills/resume.go:59-80` (DiscardPending docstring vs `writer_activate.go:176-190` auditRejection)

**Confidence:** high

**Detail:** `DiscardPending`'s docstring says "marking the gate as recommended-but-not-taken (gate_taken=false)". But `auditRejection` (called by `DiscardPending`) sets `GateTaken: true`. The `auditRejection` docstring is correct: a decline exercises the gate (the human reviewed and said no), so `gate_taken=true` is the right semantic. The `DiscardPending` comment is misleading to future readers of the D-29 matrix.

**Suggested fix:** Fix the `DiscardPending` docstring: change "gate_taken=false" to "gate_taken=true (the human exercised the gate)".

---

## What was checked

- All 10 non-test `.go` files in `internal/skills/`: `manifest.go`, `frontmatter.go`, `loader.go`, `validator.go`, `audit_store.go`, `materialize.go`, `writer.go`, `writer_activate.go`, `resume.go`, `snippet.go`, `snippet_usage.go`, `contenthash.go`, `messages.go`, `builtin.go`.
- All exported symbols grepped across the full repo (every `.go` file in `D:/Aura`) to confirm wiring status.
- Test files read for intended behavior context.
- No races found: the `Loader` is mutex-guarded with a single lock; no shared mutable state; no goroutines spawned by any function in the package.
- No integer overflow risks: `int32(limit)` in `audit_store.go:196` is bounded by the default-100 path and CLI-supplied inputs; theoretical overflow at >2B not exploitable in practice.
- `go.mod` declares `go 1.26.4`: loop-variable capture is not an issue.
