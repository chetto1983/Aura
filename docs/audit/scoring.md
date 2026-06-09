# Audit: internal/scoring

**Verdict:** needs-work — one not-wired symbol pair (`ComputeSandboxTier` / `SandboxArgs`) has zero production callers; one latent infinite-recursion in `rank` for unknown tiers.

**Counts:** critical 0 / high 1 / medium 1 / low 0

## Findings

---

### [HIGH][NOT-WIRED] `ComputeSandboxTier` and `SandboxArgs` defined but never called in production

**Location:** `internal/scoring/scoring.go:33-113`
**Confidence:** high

`ComputeSandboxTier(a SandboxArgs) RiskTier` and the `SandboxArgs` struct (lines 33-35, 105-113) are documented as "the ONLY scoring path wired in Phase 8 — the execute tool consumes ComputeSandboxTier" (line 31). That claim is false. A repo-wide grep across all `*.go` files finds exactly two files referencing these symbols: `internal/scoring/scoring.go` (definitions) and `internal/scoring/scoring_test.go` (tests). No production caller exists.

Specifically:
- `internal/agent/tools/sandbox_exec.go` — the production `SandboxExec.Execute` — has no `network_allow` argument in `sandboxExecArgs`, no import of `internal/scoring`, and no call to `ComputeSandboxTier`. The tool's JSON schema (`Spec()`) likewise exposes no egress-allowlist field to the model.
- `cmd/aura/main.go` registers `SandboxExec` with no scoring wiring.

The scoring logic is therefore silently bypassed: a model-requested sandbox call with arbitrary egress hosts is never classified as `Risky` and never triggers a gate. The advisory the PRD calls D-12 is unexecuted.

**Suggested fix:** Wire `ComputeSandboxTier` into `SandboxExec.Execute` (or at the registry layer). Add a `network_allow []string` field to `sandboxExecArgs` so the model can express its egress intent, then call `scoring.ComputeSandboxTier(scoring.SandboxArgs{NetworkAllow: a.NetworkAllow})` and, if `scoring.GateRecommended(tier)`, return an advisory result or invoke the existing gate mechanism (matching the pattern in `internal/agent/tools/task.go:187-218`). Until then the D-12 guarantee is absent from the live binary.

---

### [MEDIUM][BUG] `rank` uses indirect recursion for unknown tiers — latent infinite loop if `Risky` is ever removed from `tierOrder`

**Location:** `internal/scoring/scoring.go:67-74`
**Confidence:** medium

```go
func rank(t RiskTier) int {
    for i, known := range tierOrder {
        if known == t {
            return i
        }
    }
    return rank(Risky)  // recurses
}
```

When `t` is not in `tierOrder`, the function calls itself with `Risky`. If `Risky` is present (as it is today), the recursion terminates in one extra iteration. But the correctness guarantee is that `Risky` must remain in `tierOrder`; there is no compile-time or runtime guard enforcing that invariant. Any future edit that renames `Risky`, reorders `tierOrder` incorrectly, or removes the constant would produce an infinite recursion and a stack overflow. Callers (`bumpTier`, `RequiresImmediateAlert`, `GateRecommended` indirectly) would all hang.

The recursion adds no expressive value — the intent is simply "unknown tier maps to rank 2 (Risky)".

**Suggested fix:** Replace the recursive fallback with a direct constant:

```go
func rank(t RiskTier) int {
    for i, known := range tierOrder {
        if known == t {
            return i
        }
    }
    // Unknown tiers sort at Risky (index 2) — conservative, never Safe.
    return 2
}
```

Alternatively, add a package-level `init` that panics if `tierOrder[2] != Risky`, making the invariant explicit and caught at startup rather than at call-time.

---

## What was checked

- All symbols in `scoring.go` (RiskTier, SandboxArgs, TaskArgs, SkillAction, all five constants, all eight functions/vars) cross-referenced against every `.go` file under `D:/Aura` using Grep.
- Logic correctness of `bumpTier` saturation, `onlyPyPI` lookalike rejection, `ComputeTaskTier`'s modifier loop, and `ComputeSkillTier`'s default-to-Risky fallback.
- No goroutines, no I/O, no DB, no mutex — the package is stateless; races do not apply.
- Dead-code: all other exported symbols (`ComputeTaskTier`, `ComputeSkillTier`, `GateRecommended`, `RequiresImmediateAlert`, `RiskTier` constants, `SkillAction` constants, `TaskArgs`) have confirmed non-test production callers.
- `for range taskModifierBumps(a)` is valid Go 1.22+ range-over-integer syntax; go.mod declares 1.26.4.
- `go vet ./internal/scoring/` is clean.
