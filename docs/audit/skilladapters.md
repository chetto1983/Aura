# Audit: internal/skilladapters

**Verdict:** needs-work — one silent-error swallow that produces wrong behavior on a corrupt-but-loaded snippet skill; one audit-attribution mismatch on the model-path restore/archive; no races, no dead code.

**Counts:** critical 0 / high 1 / medium 1 / low 0

---

## Findings

### [HIGH][BUG] Snippet: silent error swallow causes wrong fallthrough to instruction-skill path

**Location:** `internal/skilladapters/skilladapters.go:79-82`

**Confidence:** high

**Detail:**

```go
path, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
if perr != nil {
    return "", "", "", false  // ok=false — caller falls through to Body()
}
```

When a snippet skill is loaded (passes the loader's structural validation) but `SnippetHostInvocation` returns an error — which happens if the `Language` field in the stored SKILL.md is an unrecognised value (possible after a manual edit or a migration that introduced a new alias not yet in `snippetMetaByLang`) — the adapter silently returns `ok=false` with no log. The tool layer (`actionUse`, `skill_read.go:107-114`) then falls through to `Loader.Body()`, which succeeds because the skill is loaded. The model receives the snippet's rendered docs body (`"Executable snippet (python). Run the python3 interpreter…"`) wrapped in the authority-frame as if it were an instruction skill — no path, no interpreter. The model is misled: it sees instructions that reference a by-path invocation but has no concrete path to call. The operator has no log entry to diagnose the failure.

The condition cannot occur in normal write paths (the `Writer.SaveSnippet` and `ValidateForWrite` gates reject unknown languages before the skill is ever stored). However, it can occur if:
1. A skill was hand-edited on disk to use an alias not normalised by `validSnippetLanguage` (e.g. `language: bash` is valid at write-time but would survive a hypothetical future refactor that broke alias normalisation).
2. A skill was installed directly by `npx skills add` into the self-install root and its SKILL.md used a spelling that passes the loader's structural `TypeSnippet` check but is rejected by `SnippetHostInvocation`.

In both cases the operator gets no signal and the model silently degrades.

**Suggested fix:**

Log the error at `slog.Warn` before returning false so the operator can diagnose the corrupt skill:

```go
path, interp, perr := skills.SnippetHostInvocation(s.Name, s.Language, a.exportDir)
if perr != nil {
    slog.Warn("skilladapters: snippet host invocation failed, falling back to instruction path",
        "skill", name, "language", s.Language, "err", perr)
    return "", "", "", false
}
```

Alternatively (stronger): return an error from `Snippet` so the caller can surface it as a tool error rather than serving wrong content. That requires a signature change propagated to the `skillLoader` interface; the log approach is the zero-footprint fix within this package.

---

### [MEDIUM][BUG] Restore adapter passes ApprovalCLI for a model-originated restore, misattributing the audit source

**Location:** `internal/skilladapters/skilladapters.go:126-133`

**Confidence:** medium

**Detail:**

```go
// Restore maps the tool's restore call onto the live Writer.Restore (the inverse of
// Archive), labeling the actor "model" with the cli ApprovalSource (the D-29 cli tuple
// the 0010 CHECK accepts — restore audits as activate/cli, no new migration).
func (a *Writer) Restore(ctx context.Context, name string) (string, error) {
    if err := a.w.Restore(ctx, name, skills.ApprovalCLI, modelActor); err != nil {
```

The model triggers `action=restore` from the skill tool. The adapter labels the audit row with `approval_source = "cli"` (the `ApprovalCLI` constant). The comment acknowledges this is a deliberate choice to avoid a new migration (D-19 / the forbidden-to-create list), but the effect is that the D-29 audit ledger cannot distinguish a model-initiated restore from an operator-CLI restore. Any forensic query over `skill_audit WHERE action = 'activate' AND approval_source = 'cli'` conflates both channels. The `actor_id = "model"` does survive as the attribution field, so the information is not entirely lost — but it contradicts the `approval_source` naming.

The same comment and pattern applies to `ArchiveSnippet` (line 140), which also passes `ApprovalCLI`.

This is a minor audit-trail quality issue, not a correctness or security defect: the D-29 CHECK accepts the cli tuple, the gate IS taken (gate_taken=true, which is correct for an immediate SAFE-tier action), and the `actor_id` field does record "model". The mismatch only affects forensic reporting.

**Suggested fix:**

If a future migration is added (outside the frozen Phase-7e window), introduce `ApprovalModel` as a new approval_source constant and use it here. Until then, document the constraint inline and ensure any `actor_id`-based forensics use `actor_id = 'model'` to detect model-originated restores/archives rather than relying on `approval_source`.

---

## What was checked (no further findings)

- **Nil-pointer risk:** `Loader.loader` and `Writer.w` fields are always set by the constructors (`NewLoader`/`NewWriter`) and never mutated after construction. No nil-dereference risk.
- **Concurrency:** `modelActor` is a package-level `var` of a pure value type (`skills.AuditActor` with two string fields), initialised once at package init and read by value in all call sites — no mutation, no race. The `Loader` and `Writer` adapters are stateless wrappers that delegate all synchronisation to `*skills.Loader` (mutex-guarded TTL cache) and `*skills.Writer` (FS+DB ops, no shared mutable state in the adapter layer). No goroutines are spawned by this package.
- **Unchecked errors:** Every method in the adapter checks and propagates errors from the delegate. The `Snippet` error swallow is the only instance, reported above.
- **Resource leaks:** No files, rows, or connections are opened in this package; all I/O is delegated to the `skills` package.
- **Dead code:** All exported symbols (`Loader`, `Writer`, `NewLoader`, `NewWriter`, and all methods) are referenced in production (`cmd/aura/serve_adapters.go`) and in the eval registry (`internal/eval/skills_snippet_reuse_registry_cot_eval_test.go`). `modelActor` is package-private and referenced on lines 103, 118, 130, 140 of the same file.
- **Not-wired code:** No handler/route/flag in this package goes unregistered. Every method corresponds to a method in the `skillLoader` / `skillWriter` interfaces consumed by `tools.SkillTool`, which is registered in the live tool registry.
- **Context propagation:** All methods that accept a `context.Context` pass it through to the delegate without modification or cancellation, which is correct for an adapter layer.
- **`%w` wrapping:** Not applicable — this file contains no `fmt.Errorf` calls; errors are returned as-is or not at all.
