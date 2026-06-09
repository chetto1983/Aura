# Audit: internal/scoring

**Verdict:** needs-work — two not-wired symbols scheduled for deletion are still present; one logic gap in body-content gating.

**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][NOT-WIRED] `ComputeSandboxTier`, `SandboxArgs`, `onlyPyPI` have zero production callers

**Location:** `internal/scoring/scoring.go:33-36, 91-113`

**Confidence:** high

**Detail:**
`SandboxArgs` (struct, lines 33-36), `onlyPyPI` (unexported func, lines 91-100), and `ComputeSandboxTier` (exported func, lines 105-113) are defined but have no callers outside the `internal/scoring` package itself.

Verification: `grep -r "ComputeSandboxTier\|SandboxArgs" --include="*.go" D:/Aura` returns only `internal/scoring/scoring.go` and `internal/scoring/scoring_test.go`. No production package imports or calls these symbols.

`docs/sandbox-removal-plan.md` documents that these three symbols are explicitly earmarked for deletion as part of the sandbox removal ("Delete those 3 symbols; KEEP everything else" — line 42). The plan has not been executed, leaving dead exported API surface that silently misleads readers into thinking the sandbox advisory path is wired.

**Suggested fix:** Execute step 4 of `docs/sandbox-removal-plan.md`: delete `type SandboxArgs struct { … }`, `func onlyPyPI(…)`, and `func ComputeSandboxTier(…)` from `scoring.go`, and delete the `ComputeSandboxTier` subtest block (lines 16-42) from `scoring_test.go`. No other package is affected.

---

### [MEDIUM][BUG] `ComputeSkillTier` silently discards `body`; destructive-keyword content in skill bodies is not escalated

**Location:** `internal/scoring/scoring.go:164`

**Confidence:** medium

**Detail:**
```go
func ComputeSkillTier(action SkillAction, body string) RiskTier {
    _ = body   // ← body is ignored
    ...
}
```

`ComputeTaskTier` scans `TaskArgs.Payload` for `destructiveKeyword` and jumps straight to `Destructive` when a payload contains `rm`, `delete`, `drop`, `purge`, or `truncate`. `ComputeSkillTier` accepts an analogous `body` parameter (callers pass real skill code: `internal/skills/writer.go:89`, `internal/skills/snippet.go:183`) but unconditionally ignores it.

A skill body containing `rm -rf /` or `DROP TABLE` passes through as merely `Risky` (same tier as an empty body), while an equivalent scheduler payload would correctly escalate to `Destructive`. This is an asymmetry in the risk model that the PRD's UP-only invariant was designed to prevent.

The comment on line 161 ("The body is reserved for future content-based escalation; today the action alone decides the tier") documents this as a deliberate deferral, not an oversight — so the severity is medium rather than high. However, the asymmetry is a security-relevant gap: callers pass real executable code and receive a tier that does not reflect it.

**Suggested fix:** Apply `destructiveKeyword.Match([]byte(body))` check inside `ComputeSkillTier`, mirroring `baseTaskTier`. If matched, bump the result to `Destructive` (or at minimum `Risky` → `Destructive` for `SkillCreate`/`SkillInstall`). Update `scoring_test.go` with a corresponding table row. Alternatively, if deferral is intentional, document it with a `TODO(slice-N):` and a reference to the PRD item that tracks it.

---

### [LOW][BUG] `rank()` uses recursion for the unknown-tier fallback; a mutable `tierOrder` would cause infinite recursion

**Location:** `internal/scoring/scoring.go:67-74`

**Confidence:** low

**Detail:**
```go
func rank(t RiskTier) int {
    for i, known := range tierOrder {
        if known == t {
            return i
        }
    }
    return rank(Risky)  // ← recursive call
}
```

In the current codebase this is safe: `tierOrder` is a package-level `var` initialized with the four known tiers, `Risky` is always found in the linear scan, and there is no mutation path. However, the recursive fallback is fragile by construction: if `tierOrder` were ever modified to omit `Risky` (e.g., during a future refactor that renames the tier), the function would recurse infinitely and stack-overflow. The comment says "An unknown tier sorts at Risky" but the implementation couples the fallback to the presence of `Risky` in `tierOrder` rather than its index.

**Suggested fix:** Replace the recursive fallback with an explicit constant:
```go
func rank(t RiskTier) int {
    for i, known := range tierOrder {
        if known == t {
            return i
        }
    }
    return 2 // conservative fallback: Risky (index 2 in Safe<Normal<Risky<Destructive)
}
```
Or add a `const riskyRank = 2` to make the intent explicit. This decouples the fallback from the scan, makes the function total and non-recursive, and survives `tierOrder` refactors.

---

## What was checked and found clean

- **Nil-pointer / unchecked errors:** all functions are pure transforms with no I/O, no pointer receivers, no error returns. Not applicable.
- **Data races:** no goroutines, no shared mutable state. `tierOrder` and `destructiveKeyword` are package-level vars initialized once at startup (no writes after init). Clean.
- **Resource leaks:** no file handles, DB rows, HTTP bodies, tickers, or goroutines. Clean.
- **Context propagation:** no I/O, no context needed. Clean.
- **Integer overflow:** `rank()` returns an `int` from a 4-element slice; `bumpTier` adds 1 before bounds-checking. No overflow possible.
- **Incorrect `%w` wrapping:** no error values in this package.
- **Slice aliasing:** `NetworkAllow []string` is read-only in `onlyPyPI`/`ComputeSandboxTier`. Clean.
- **`for range taskModifierBumps(a)` when base is already `Destructive`:** wasted iterations but `bumpTier(Destructive)` saturates correctly. Not a bug.
- **Dead exported symbols (other than the sandbox triad):** `RiskTier`, `Safe`/`Normal`/`Risky`/`Destructive`, `TaskArgs`, `SkillAction`, `SkillCreate`/`SkillUpdate`/`SkillInstall`/`SkillDelete`, `ComputeTaskTier`, `ComputeSkillTier`, `GateRecommended`, `RequiresImmediateAlert` — all confirmed used in production callers (`internal/cron`, `internal/skills`, `internal/agent/tools`, `cmd/aura`).
