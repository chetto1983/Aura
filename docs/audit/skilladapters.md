# Audit: internal/skilladapters

**Verdict:** needs-work — one silent-failure bug masks snippet degradation from the model and audit log; one attribution inconsistency in the audit record.

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][BUG] `Loader.Snippet()` silently swallows language-resolution errors, degrading snippet to instruction-skill

**Location:** `internal/skilladapters/skilladapters.go:79-82`

**Confidence:** high

**Detail:**

`Snippet()` calls `skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)` and, when that returns a non-nil `perr`, returns `"", "", "", false` silently — no log, no metric, no propagated error:

```go
path, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
if perr != nil {
    return "", "", "", false
}
```

`ok=false` causes `actionUse` in `skill_read.go:110` to fall through to `t.Loader.Body(name)` and deliver the snippet's docs body (the `renderSnippetDocs` frame: "Executable snippet (python). Run the python3 interpreter...") wrapped in the instruction authority-frame. The model receives text that says to run the snippet by path but receives no path and no interpreter. The call is silently degraded to an instruction skill without the model knowing why.

This can be triggered for operator-installed snippets that have an invalid or empty `language:` field: `validateStructure` in `loader.go:235-257` does NOT check the `Language` field for type:snippet skills (it only checks Name, Description, Type, and Body), so a snippet with `language: ruby` or `language:` (empty) passes the loader but fails the language enum check inside `SnippetHostInvocation`.

The `SaveSnippet` path (`skills.Writer.SaveSnippet`) always validates the language via `validSnippetLanguage`, so snippets written through the model tool cannot carry a bad language. However, operator-installed skills from disk (via `npx skills add` into the export root) bypass `SaveSnippet` entirely and land directly on disk. The loader's `validateStructure` gap means a bad-language snippet from the ecosystem gets loaded silently and then fails silently at use-time.

**Suggested fix:**

Log the language resolution error before returning `false` so operators see the degradation:

```go
path, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
if perr != nil {
    slog.Warn("skilladapters: snippet language resolution failed, falling back to instruction path",
        "name", s.Name, "language", s.Language, "err", perr)
    return "", "", "", false
}
```

Additionally, consider adding language validation to `validateStructure` for type:snippet so the loader rejects bad-language snippets at scan time (same skip-and-warn pattern the loader uses for other structural failures), which would prevent the fallback from being reached at all.

---

### [LOW][BUG] `ArchiveSnippet` audit record is misleading: model-initiated archive audits as `approval_source=cli`

**Location:** `internal/skilladapters/skilladapters.go:136-144`

**Confidence:** high

**Detail:**

Both `Restore()` (line 130) and `ArchiveSnippet()` (line 140) pass `skills.ApprovalCLI` as the approval source when calling the underlying `w.Restore` / `w.Archive`:

```go
// Restore
if err := a.w.Restore(ctx, name, skills.ApprovalCLI, modelActor); err != nil {

// ArchiveSnippet  
if err := a.w.Archive(ctx, name, skills.ApprovalCLI, modelActor); err != nil {
```

The `modelActor` carries `ActorID: "model"` but `ApprovalSource: "cli"`. The resulting audit rows in `aura.skill_audit` have `actor_id=model` and `approval_source=cli` — a contradiction, since `cli` is defined as the "operator CLI approve" source (audit_store.go:49). An operator reading the audit trail cannot distinguish a model-triggered archive/restore from a CLI-triggered one.

For `Restore`, the code comment on lines 126-128 acknowledges this as an intentional workaround: "restore audits as activate/cli, no new migration" (the 0010 CHECK does not include a `model` approval source). The constraint is real.

For `ArchiveSnippet`, the comment on line 137 says "labeling the actor 'model' with the cli ApprovalSource (the manual operator-source archive, distinct from the TTL sweep's auto source)" — but the "manual operator-source" framing is incorrect when the action is model-initiated.

This does not cause a runtime failure or DB coherence violation (the D-29 CHECK accepts `cli` for archive actions). It produces misleading audit records in a security-relevant ledger.

**Suggested fix:**

The constraint is the 0010 migration's `action` CHECK not including a `model` source. The correct fix is to add a `ApprovalModel = "model"` constant to `audit_store.go` and add it to the DB CHECK in a follow-up migration, then use it here. Until the migration ships, the comment should be updated on `ArchiveSnippet` to explicitly state the workaround: "audits as cli because the 0010 CHECK has no model source; tracked for amendment."

---

## What was checked and found clean

- **Nil-pointer safety**: `Loader.loader` and `Writer.w` are always non-nil (set in constructors, no code path sets them to nil after construction). `NewLoader` and `NewWriter` are called at the composition root only when the live `*skills.Loader` / `*skills.Writer` are available. No nil dereference risk.
- **Concurrency**: No shared mutable state in the adapter structs. `Loader.List()`, `Body()`, `ManifestDescription()`, and `Snippet()` all delegate to `skills.Loader` methods that are mutex-guarded. `Writer` methods delegate directly to `skills.Writer` which is not documented as concurrent-safe but is only called from the single tool dispatch goroutine. The package-level `var modelActor` is write-once at init, safe to read concurrently.
- **Error wrapping**: All errors are propagated via `return "", err` or `return "", "", "", false` — no %w incorrectness within this package (the adapter does not wrap errors itself; the underlying methods wrap them).
- **Dead code**: All exported symbols (`NewLoader`, `NewWriter`, `Loader`, `Writer`, and all their methods) have verified non-test callers in `cmd/aura/serve_adapters.go` and `internal/eval/skills_snippet_reuse_registry_cot_eval_test.go`.
- **Resource leaks**: No goroutines, no file handles, no DB rows opened in this package. Nothing to close.
- **Context propagation**: Every method that takes a `context.Context` forwards it correctly to the underlying `skills.Writer` calls.
- **`SaveSnippet` `Frontmatter` construction**: The adapter constructs a `Frontmatter` with no `Language` field (line 112-117), but `skills.Writer.SaveSnippet` immediately overwrites `fm.Language` at line 173 of `snippet.go` before any language-dependent logic runs. Not a bug.
- **`ArchiveSnippet` returns hardcoded `"archived"` string**: The string is only used as a display status in the tool result message. The `skills` package has no exported `StatusArchived` constant, so the hardcoded literal is the correct approach given the current package API. Minor style inconsistency (not a bug).
