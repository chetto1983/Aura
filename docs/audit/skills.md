# Audit: internal/skills

**Verdict:** needs-work — two writer methods missing the mandatory `SanitizeName` path-traversal guard; three lower-severity issues.

**Counts:** critical 0 / high 1 / medium 0 / low 3

---

## Findings

### [HIGH][BUG] `Activate` and `DiscardPending` skip the `SanitizeName` path-traversal guard

**Location:** `internal/skills/writer_activate.go:24-56` (`Activate`), `internal/skills/resume.go:66-80` (`DiscardPending`)

**Confidence:** high

**Detail:**

Every other writer method that joins `name` into a filesystem path calls `SanitizeName(name, name)` as its first step:
- `Archive` — `writer_activate.go:68`
- `Delete` — `writer_activate.go:143`
- `Restore` — `writer_activate.go:110`
- `SetAlways` — `writer_activate.go:219`
- `UseSnippet` — `snippet.go:307`
- `snippetIsStale` (sweep) — `snippet_usage.go:180`

`Activate` and `DiscardPending` are the only two that do not. Both call `filepath.Join(w.pendingDir, name)` (or `w.activeDir`) and then:
- `Activate`: `os.RemoveAll(dst)` + `os.Rename(src, dst)` via `promoteDir`
- `DiscardPending`: `os.RemoveAll(pendingDir)`

The `name` value arrives from the runner's resume hook (`serve_adapters.go:289`):

```go
return h.Resume(ctx, resp.Action, rc.SkillName, pending.Token, skills.AuditActor{ActorID: "local"})
```

`rc.SkillName` is JSON-decoded from `pending.ResumeContext`, which is authored by the **model** via the `ask_user` `resume_context` field. The tool description at `ask_user.go:110` tells the model to populate `{"type":"skill_approval","skill_name":"<name>"}` — the model fully controls the value. A prompt-injected or misbehaving model can supply `skill_name: "../active/legit-skill"` causing:
- `DiscardPending` → `os.RemoveAll("<pendingDir>/../active/legit-skill")` — deletes an active skill dir the user never intended to remove.
- `Activate` → `promoteDir` renames `<pendingDir>/../active/…` to `<activeDir>/../active/…` — can overwrite or relocate directories outside the pending tree.

The skill tool enforces the grammar via `validWriteName` when the model calls the skill tool itself (step 1 of the gate), but the `resume_context` injected in the separate `ask_user` call (step 2) is never validated at the skills boundary.

**Suggested fix:**

Add `SanitizeName(name, name)` as the first statement in both methods, matching every other path-joined writer method:

```go
// In writer_activate.go Activate:
func (w *Writer) Activate(ctx context.Context, name string, src ApprovalSource, pausedToken string, actor AuditActor) error {
    if err := SanitizeName(name, name); err != nil {
        return fmt.Errorf("activate %q: %w", name, err)
    }
    srcDir := filepath.Join(w.pendingDir, name)
    ...
}

// In resume.go DiscardPending:
func (w *Writer) DiscardPending(ctx context.Context, name string, src ApprovalSource, pausedToken string, actor AuditActor) error {
    if err := SanitizeName(name, name); err != nil {
        return fmt.Errorf("discard pending %q: %w", name, err)
    }
    pendingDir := filepath.Join(w.pendingDir, name)
    ...
}
```

Tests to add: mirror `TestRestoreRejectsBadNameMessage` / `TestWriterDeleteRejectsBadName` (writer_activate_mutation_test.go) for both methods.

---

### [LOW][BUG] `Delete` returns `StatusActive` after successful deletion

**Location:** `internal/skills/writer_activate.go:166`

**Confidence:** high

**Detail:**

`Delete` returns the constant `StatusActive = "active"` when the skill has been successfully removed:

```go
return StatusActive, nil  // line 166
```

A deleted skill is not active. The caller `WriteMutation` (writer.go:100) propagates this value directly to its callers. Currently all callers discard it (`_ = status` at skill_write.go:177; `_, err := env.w.Delete(...)` at cmd/aura/skills.go:189), so there is no observable runtime misbehavior. However the value is misleading: a future caller that inspects the status string to branch on the outcome will silently receive `"active"` for a deleted skill.

**Suggested fix:**

Add a `StatusDeleted = "deleted"` constant (mirroring `StatusPendingApproval`/`StatusActive`) and return it from `Delete`. Alternatively, return `StatusPendingApproval` to stay consistent with the "gated write was submitted" semantics (delete is still gated at the WriteMutation level), but `"deleted"` is clearer.

---

### [LOW][DEAD-CODE] `AuditStore.InsertAudit` is never called in production code

**Location:** `internal/skills/audit_store.go:147-157`

**Confidence:** high

**Detail:**

`InsertAudit` (the non-transactional, pool-level audit insert) is referenced only from integration tests (`audit_store_integration_test.go:96`, `142`, `159`, `174`). No production code path calls it. All production auditing goes through `InsertAuditTx` (the tx-bound variant used inside `db.WithTx` closures in the writer). The export is part of the original plan's API surface ("InsertAudit (pool) + InsertAuditTx") but the transactional writer pattern made the pool-level call redundant.

If there is no foreseen non-tx caller (e.g. a future health-check or recovery tool that inserts outside a tx), this method can be unexported or removed.

**Suggested fix:**

Either unexport (`insertAudit`) or remove the method. If a future non-tx insertion path is anticipated, document the intended consumer in the godoc rather than leaving it silently orphaned.

---

### [LOW][BUG] `RenderManifest` cap not enforced for the first skill

**Location:** `internal/skills/manifest.go:33`

**Confidence:** high

**Detail:**

The overflow guard is:

```go
if rendered > 0 && b.Len()+len(line) > capBytes {
    break
}
```

The `rendered > 0` condition means the **first skill always renders** regardless of how large its line is relative to `capBytes`. For the production defaults (`capBytes = 8192`, max description 1024 B, max name 64 B, line overhead ~4 B) a single skill line is at most ~1094 B, well inside 8192 B — so the overflow never fires for the first entry in practice. But calling `RenderManifest(skills, 1)` (or any cap below ~1100 B) produces output that exceeds the stated budget, contradicting the doc ("once the running byte count would exceed capBytes the render stops").

**Suggested fix:**

Remove the `rendered > 0` guard so the cap applies uniformly to all entries:

```go
if b.Len()+len(line) > capBytes {
    break
}
```

This changes the edge-case behavior for very small `capBytes` values (the first skill would be omitted and only the overflow tail emitted), which is the correct behavior per the spec. The default 8192 B cap is unaffected for any realistic skill set.

---

## Coverage of audit scope

Files read (all non-test .go sources in `internal/skills`):
- `audit_store.go` — AuditStore, InsertAuditTx, classifyAuditErr
- `builtin.go` — MaterializeBuiltins, writeIfChanged
- `contenthash.go` — HashSkillFiles, HashSkillDir, collectFilesNoSymlinks
- `frontmatter.go` — parseFrontmatter, splitFrontmatter, indexClosingFence
- `loader.go` — Loader, NewLoader, List, Get, refreshLocked, scan, scanRoot, loadSkillDir, validateStructure
- `manifest.go` — RenderManifest, BM25Corpus
- `materialize.go` — Materialize, copyTreeNoSymlinks, copyRegularFile, Dematerialize
- `messages.go` — RenderAlwaysBlock
- `resume.go` — ResumeHandler, NewResumeHandler, Resume, DiscardPending
- `snippet.go` — SnippetLanguage, validSnippetLanguage, SnippetCodeFile, SnippetSandboxPath, SnippetHostPath, SnippetInvocation, SnippetHostInvocation, SaveSnippet, writePendingSnippet, renderSnippetDocs, UseSnippet
- `snippet_usage.go` — UsageSidecar, readUsageSidecar, ReadUsage, writeUsageAtomic, writeUsageAtomicInDir, setUsageStatusInRoot, StampUsage, SetUsageStatus, SweepExpiredSnippets, snippetIsStale
- `validator.go` — SanitizeName, violatesBlocklist, ValidateForWrite, ValidateNameAgainstDir
- `writer.go` — Writer, NewWriter, WriteMutation, WriteInstallPending, WriteMutationByName, WriteMutationCLI, writePending, skillFileBytes, yamlScalar, auditActionFor
- `writer_activate.go` — Activate, Archive, Restore, Delete, auditRejection, auditActivationLike, SetAlways, promoteDir

Grep-confirmed no goroutine spawns; mutex coverage correct (Loader uses single sync.Mutex, no background goroutines). No ticker/ticker leaks. Loop-variable capture is moot (go 1.26.4). No integer overflow in hot paths (int32 cast in AuditStore.List is theoretical — all callers use default 100). No JSON mishandling. No unchecked errors that matter (best-effort hashes are explicitly discarded with `hash, _`).
