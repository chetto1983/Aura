# Audit: internal/skills

**Verdict:** needs-work — one stale comment documents a lie (promoteDir EXDEV fallback), one doc-code contract mismatch in resume.go, and several exported symbols with zero production callers.

**Counts:** critical 0 / high 1 / medium 2 / low 3

---

## Findings

### [HIGH][BUG] promoteDir comment promises EXDEV fallback that is not implemented

**Location:** `internal/skills/writer_activate.go:245-258`

**Confidence:** high

**Detail:**
The function comment reads "A cross-device rename (EXDEV) falls back to a copy+remove." The body does no such thing: it calls `os.Rename` and wraps any error unconditionally. If `pending/`, `active/`, and `archived/` are ever placed on different filesystems (e.g. a Docker volume bind-mount for `archived/`), every `Activate`/`Restore`/`Archive` call will return `EXDEV` and fail. In the default single-`SkillsDir` configuration this never triggers, but the comment creates false confidence for any operator who does split the paths. The contract is a lie-in-comment, not a latent runtime safety net.

**Suggested fix:**
Either (a) implement the EXDEV fallback — detect `syscall.EXDEV` via `errors.Is` and fall back to `copyTreeNoSymlinks` + `os.RemoveAll(src)` — or (b) remove the claim from the comment. Option (b) is the minimal fix; option (a) is the correct long-term fix and uses `copyTreeNoSymlinks` which already exists.

---

### [MEDIUM][BUG] resume.go doc comments contradict the audit record on decline/cancel gate_taken

**Location:** `internal/skills/resume.go:44`, `internal/skills/resume.go:63`

**Confidence:** high

**Detail:**
Two doc comments state that a decline/cancel resume records `gate_taken=false`:
- Line 44 (Resume doc): "on decline/cancel it DISCARDS the pending skill + audit the ask_user rejection (gate_taken=false: the human declined the gate)"
- Line 63 (DiscardPending doc): "recording the gate as recommended-but-not-taken (gate_taken=false)"

The implementation at `writer_activate.go:187` uses `GateTaken: true`. The `auditRejection` comment at line 171 correctly says `gate_taken=true`. The implementation is consistent and intentional (the D-29 matrix, as documented in `writer_activate.go:171-175`, treats a human reject as `gate_taken=true` — the human DID take the gate, they just said no). The `resume.go` doc comments are wrong and will mislead anyone building on or auditing the D-29 tuple contract.

**Suggested fix:**
Update `resume.go` lines 44 and 63 to say `gate_taken=true`. The correct rationale is in `writer_activate.go:171-175`: the approve-vs-reject distinction lives in the resume answer, not the `gate_taken` field; both accept AND decline are `gate_taken=true` (the gate was exercised).

---

### [MEDIUM][NOT-WIRED] BM25Corpus exported but has no production caller

**Location:** `internal/skills/manifest.go:53-62`

**Confidence:** high

**Detail:**
`BM25Corpus` is exported and intended to feed the overflow `list` ranker (D-09). The only non-test reference is a comment in `internal/agent/tools/skill.go:62`. The live overflow list ranker lives in `internal/agent/tools/bm25.go` and operates over tool specs, not skills. The `skilladapters.Loader` never calls `BM25Corpus` — it calls `RenderManifest` only. The function is reachable only from tests (`manifest_test.go:71`). The planning docs for the D-09 overflow ranker exist but the wiring commit has not landed.

**Suggested fix:**
Either wire the overflow list ranking — `skillLoader` seam needs a `Corpus() []string` method that the list action uses to rank results — or mark the function with a `// TODO(D-09): wire into the list action overflow ranker` comment to signal intentional scaffolding. Until wired, every overflow list hit falls back to a name-prefix substring match.

---

### [LOW][NOT-WIRED] ValidateNameAgainstDir exported but has no production caller

**Location:** `internal/skills/validator.go:108-115`

**Confidence:** high

**Detail:**
The doc says "the installer's name+dir chokepoint" (plan 11-06 installer). The installer (Slice 7d/11-06) does not exist yet. The function is called only from `validator_test.go:194-199`. It is not wired in any production path.

**Suggested fix:**
No action needed before the installer lands. Document with `// TODO(plan-11-06)` to make the scaffolding intent explicit, or keep it as is — the function is correct and ready.

---

### [LOW][NOT-WIRED] SnippetInvocation / SnippetCodeFile / SnippetSandboxPath / SnippetHostPath — exported helpers with no production callers outside the package

**Location:** `internal/skills/snippet.go:78-110, 117-127`

**Confidence:** high

**Detail:**
Four exported snippet helpers:
- `SnippetCodeFile` — only called from within `snippet.go` itself (lines 90, 105).
- `SnippetSandboxPath` — only called from within `snippet.go` (line 326) and smoke tests.
- `SnippetHostPath` — only called from within `snippet.go` (line 327) and tests.
- `SnippetInvocation` — only called from a spike prototype (`.planning/spikes/012a-discovery-skill-driven/main.go`, not in the production binary).

The live production path uses `UseSnippet` (which calls these internally) and `SnippetHostInvocation` (via `skilladapters`). These are correct scaffolding — they were the building blocks before `UseSnippet` was added as the stable seam — but their external surface is now unused.

**Suggested fix:**
If no installer or external caller is planned, consider unexporting `SnippetCodeFile`, `SnippetSandboxPath`, and `SnippetHostPath` (keep `SnippetHostInvocation` and `SnippetInvocation` for the installer's use). Or leave exported with a note that the installer will consume them.

---

### [LOW][BUG] DiscardPending hashes pendingDir unconditionally even when pendingDir is empty

**Location:** `internal/skills/resume.go:67-68`

**Confidence:** low

**Detail:**
```go
pendingDir := filepath.Join(w.pendingDir, name)
hash, _ := HashSkillDir(pendingDir) // best-effort
if w.pendingDir != "" {
    ...
}
```
When `w.pendingDir == ""`, `pendingDir` resolves to `"/" + name` (an absolute path derived from an empty component). `HashSkillDir` is then called on that path — on most systems this will fail (directory not found) and `hash` will be `""` (the error is swallowed). This is a best-effort hash so the impact is limited to an empty `content_hash` in the rejection audit row. In production `pendingDir` is always set (see `newSkillWriter`), so the condition never fires. Still, a misplaced empty-string guard makes the hash call structurally redundant.

**Suggested fix:**
Move `hash, _ := HashSkillDir(pendingDir)` inside the `if w.pendingDir != ""` block, mirroring the guard that protects `os.RemoveAll`.

---

## What was checked and found clean

- **No goroutine leaks.** The loader is mutex-guarded lazy-on-read with no background goroutine. `TestMain` installs `goleak.VerifyTestMain` confirming this.
- **No races.** `Loader` uses `sync.Mutex` for all snapshot accesses. `Writer` is stateless between calls (all state is in files/DB). No shared mutable maps written concurrently.
- **No unchecked errors that matter.** All `hash, _ :=` discards are documented best-effort (recovery-path hash — an empty hash is auditable but not fatal). The write paths (`writePending`, `writePendingSnippet`) check every FS error.
- **No resource leaks.** No open file handles, no `http.Response.Body` (no HTTP in this package), no DB rows (sqlc cursor is closed by the generated layer).
- **No nil-pointer derefs.** `w.pool` nil is only reached in FS-only test writers and the audit-INSERT paths are guarded by the integration test tag.
- **No JSON mishandling.** `json.Marshal`/`json.Unmarshal` on `UsageSidecar` check errors; goccy YAML unmarshaling checks errors.
- **No off-by-one in RenderManifest overflow.** The `rendered > 0` guard intentionally allows the first entry regardless of size (always-show-at-least-one design).
- **`yamlScalar` byte indexing is safe.** UTF-8 continuation bytes (0x80–0xBF) never alias `"` (0x22) or `\` (0x5C), so the byte-walk escape is correct.
- **`int32(limit)` in AuditStore.List.** Overflow only possible if a caller passes Limit > 2^31, which no internal caller does (default 100, CLI-bounded).
- **`SweepExpiredSnippets` double-status-write.** After `Archive` moves the dir, `setUsageStatusInRoot(archiveDir, name, "archived")` correctly updates the sidecar in the new location. Not a double-write bug.
- **`SanitizeName` before every filepath.Join** in Archive/Delete/Restore/SetAlways/UseSnippet — all path-traversal chokepoints are present.
