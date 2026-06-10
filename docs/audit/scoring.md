# Audit: internal/scoring

**Verdict:** needs-work — two low-severity issues found; logic is sound, no bugs or races.

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

---

### [MEDIUM][DEAD-CODE] `SkillInstall` enum value unreachable from any production dispatch path

**Location:** `internal/scoring/scoring.go:46`
**Confidence:** high

`SkillInstall SkillAction = "install"` is defined and handled in `ComputeSkillTier`'s case arm (line 131) and mapped in `internal/skills/writer.go:304` (`auditActionFor`). However, the only direct tool dispatcher that could invoke `writeAction(ctx, raw, scoring.SkillInstall)` is `internal/agent/tools/skill_write.go` — which only wires `scoring.SkillCreate`, `scoring.SkillUpdate`, and `scoring.SkillDelete` (lines 112, 120, 127). The test comment at `internal/agent/tools/skill_test.go:88` explicitly records that `"install"` was removed from the tool dispatch (amendment #51 / D-40).

The only remaining path is via `WriteMutationByName`/`WriteMutationCLI` with a raw string `"install"`, which coerces to `scoring.SkillAction("install")`. No CLI command, no test, and no non-test caller passes `"install"` to these helpers in production.

The `ComputeSkillTier` and `auditActionFor` case arms for `SkillInstall` are therefore dead from a production dispatch perspective; the constant itself is orphaned in the scoring package.

**Suggested fix:** If the install action is intentionally decommissioned (D-40 says so), remove `SkillInstall` from the `scoring` package enum and its case arms in `ComputeSkillTier` and `auditActionFor`, then delete the corresponding test row in `scoring_test.go` and `writer_test.go`. If it will be re-wired in a future slice, add a `// Future: wired in Slice X` comment to suppress confusion.

---

### [LOW][DEAD-CODE] `ComputeSkillTier` default case is unreachable given current enum design

**Location:** `internal/scoring/scoring.go:133-135`
**Confidence:** medium

```go
default:
    return Risky
```

The `default` arm in `ComputeSkillTier` returns `Risky` — identical to the explicit `case SkillCreate, SkillUpdate, SkillInstall` arm immediately above. Because `SkillAction` is a plain `string` type (not a Go enum), an unknown value can reach the default; the fallback to `Risky` is intentional and documented. However, the conservative choice is inconsistent with `rank`'s fallback (also `Risky`) but differs from `GateRecommended`, which would then return `true`. The code is not wrong, but the comment on line 123-125 says "action alone decides the tier" while not mentioning that an unrecognised action is silently tiered `Risky`.

The real risk is that if a new `SkillAction` value is added without updating this switch, it will silently tier as `Risky` (not `Destructive`) — a false-negative for safety-critical additions.

**Suggested fix:** Add a test case asserting the default-case behaviour (already partially covered by `scoring_test.go:65` with `SkillAction("mystery")` → `Risky`), and add a one-line comment to the `default` branch: `// Unknown actions are treated as Risky, not Safe; add explicit cases for new SkillAction values.` This makes the fail-safe intent visible so future additions don't silently slip through.

---

## What was checked and found clean

- **Nil-pointer / unchecked errors:** None. The package contains only pure functions with no IO, DB, or network calls. No error returns exist, so there is nothing to miss.
- **Races:** None. All state is immutable: `tierOrder` is a package-level slice never written after init; `destructiveKeyword` is a compiled regexp assigned once at init. No goroutines or shared mutation.
- **Resource leaks:** Not applicable — no files, connections, or goroutines opened.
- **`rank` infinite recursion:** Superficially suspicious (`rank` calls `rank(Risky)` in the fallback), but `Risky` is always in `tierOrder` (index 2), so the recursion terminates in one extra call. No stack overflow possible.
- **`for range taskModifierBumps(a)`:** Valid Go 1.22+ range-over-int. `taskModifierBumps` returns values in `{0,1,2,3}`; a negative return would silently range zero times (no panic), but the function cannot return negative values given its logic.
- **`bumpTier` saturation:** Correct. `rank(Destructive)` == 3 == `len(tierOrder)-1`, so `next >= len(tierOrder)` saturates to `Destructive` without out-of-bounds access.
- **`ComputeTaskTier` + `baseTaskTier`:** Logic matches documented intent. Destructive-keyword detection applies only to `agent_job` (not `reminder`/`backup_*`), which is correct per the test at line 31.
- **Wiring (production consumers):** `ComputeTaskTier` is called in `cmd/aura/task.go:107`, `internal/cron/dispatch.go:177`, and `internal/agent/tools/task.go:194`. `ComputeSkillTier` is called in `internal/skills/writer.go:89`, `internal/skills/snippet.go:183`, and `internal/agent/tools/skill_write.go:158`. `GateRecommended` is called in `cmd/aura/task.go:109`, `internal/skills/writer.go:90`, `internal/agent/tools/task.go:200`, and `internal/skills/snippet.go` (indirectly). `RequiresImmediateAlert` is called in `internal/cron/dispatch.go:153` and `internal/agent/tools/task.go:225`. All four exported functions are live.
