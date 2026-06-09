# Audit: internal/skilladapters

**Verdict:** needs-work — one silent error-swallow that degrades snippet resolution to a confusing instruction-skill fallback with no diagnostic trace; no unit tests for the package.

**Counts:** critical 0 / high 0 / medium 1 / low 1

---

## Findings

### [MEDIUM][BUG] Snippet: silent error discard on SnippetHostInvocation failure

**Location:** `internal/skilladapters/skilladapters.go:79-82`

**Confidence:** high

**Detail:**

When `skills.SnippetHostInvocation` returns an error (bad or unknown `Language` in the active SKILL.md frontmatter — e.g. after external editing, an upgrade/rollback, or on-disk corruption), `Loader.Snippet` discards `perr` silently and returns `ok=false`. The caller (`tools.SkillTool.actionUse`, `skill_read.go:107-110`) falls through to `Loader.Body(name)`, which returns the snippet's SKILL.md docs body verbatim (`renderSnippetDocs` output). That body says: *"Executable snippet (python). Run the python3 interpreter against the snippet file by path (the exact invocation is provided when you use it)"* — but no invocation is ever provided because the snippet branch returned `ok=false`. The model receives contradictory instructions with no usable path.

There is no log line emitted at any level, so an operator has no trace that a snippet skill silently fell back to the instruction path.

Note: this is not a panic or data-corruption risk, but it produces a confusing model experience and is invisible at the operations layer.

```go
// current (line 80-81):
if perr != nil {
    return "", "", "", false   // perr dropped on the floor
}
```

**Suggested fix:**

Add a `slog.Warn` before returning `ok=false` so the operator can see the degraded skill:

```go
if perr != nil {
    slog.Warn("skilladapters: snippet host invocation failed; falling back to instruction path",
        "skill", name, "language", s.Language, "err", perr)
    return "", "", "", false
}
```

---

### [LOW][DEAD-CODE] Package has zero tests

**Location:** `internal/skilladapters/skilladapters.go` (whole file)

**Confidence:** high

**Detail:**

`internal/skilladapters` has exactly one non-test file and no `*_test.go` files at all. The only test exercising the adapters is `internal/eval/skills_snippet_reuse_registry_cot_eval_test.go`, gated behind the `cot_eval` build tag (requires a live OpenRouter key and is excluded from normal CI). The unit behaviour of every method — `List`, `Body`, `ManifestDescription`, `Snippet`, `WriteMutation`, `SaveSnippet`, `Restore`, `ArchiveSnippet` — is never exercised by the standard `go test ./...` or the integration tier.

In particular, the `Snippet` silent-fallback path described above (MEDIUM finding) cannot be caught without tests.

**Suggested fix:**

Add a `skilladapters_test.go` with at least:
- A table test covering `Loader.Snippet` for: (a) absent skill, (b) non-snippet skill, (c) valid snippet, (d) snippet with broken language (exercises the silent-fallback path).
- A smoke test for `Writer.WriteMutation`, `Writer.SaveSnippet`, `Writer.Restore`, `Writer.ArchiveSnippet` using an in-memory or temp-dir fake `*skills.Writer` (or just the real one against a test dir without a DB, verifying the adapter routes the call correctly).

---

## What was checked and found clean

1. **Interface alignment** — `Loader` satisfies `tools.skillLoader` exactly (List/Body/ManifestDescription/Snippet match the consumer-declared seam). `Writer` satisfies `tools.skillWriter` exactly (WriteMutation/SaveSnippet/Restore/ArchiveSnippet). Signatures match the interface declarations at `internal/agent/tools/skill.go` and `skill_write.go`.

2. **Concurrency** — Both adapter structs are constructed once and all fields are immutable after `New*`. The underlying `*skills.Loader` carries its own `sync.Mutex`. No shared mutable state in the adapter layer.

3. **Error propagation** — All `w.*` calls propagate errors with `%w`; the one exception is the `Snippet`/`perr` discard (covered above). The `SaveSnippet` adapter extracts only `res.Status` from `SnippetSaveResult`, correctly ignoring the tier/language/needs_* (the adapter's job is to return the status string the tool prints).

4. **Actor labelling** — `modelActor` (actor_id="model") is applied consistently to all four write paths. `ApprovalCLI` on `Restore` and `ArchiveSnippet` matches the documented D-29 cli tuple and is intentional per the inline comments.

5. **Wiring** — Both `NewLoader` and `NewWriter` are used in production at `cmd/aura/serve_adapters.go:259-262` and in the eval registry at `internal/eval/skills_snippet_reuse_registry_cot_eval_test.go:46-58`. No exported symbol is dead.

6. **Nil-pointer safety** — `Loader.loader` and `Writer.w` are never nil post-construction; no nil-deref path exists in the adapter layer.

7. **exportDir empty** — `filepath.Join("", name, file)` would yield a relative path if `exportDir` is empty, but all production callers pass `cfg.SkillExportDir` which always has a non-empty default (`defaultSkillExportDir()`). Not flagged as a bug given the config-level guarantee.
