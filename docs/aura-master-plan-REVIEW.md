# REVIEW: Aura Master Plan v1.1 — executability + risk reality check

**Date:** 2026-05-14
**Reviewer:** Independent — executability + risk
**Plan version reviewed:** v1.1 (synthesizer pass + Codex integration)
**Verdict:** DOWNSCOPE_RECOMMENDED

## Summary

Step 1 is **not executable today as written**: it claims to delete three "empty" packages (`orchestration/`, `tracing/`, `release/`), but `internal/release/` actually contains a 121-LOC `package release_test` file that guards against legacy desktop-runtime bundles re-appearing in `.goreleaser.yml` / release workflows. Deleting it silently removes a release-hygiene guard. Multiple verified-state numbers in §1 are stale (telegram 8957 not 8239 LOC, agent.Runner 554 not 520, agentloop 780 not 738, chathub 2601 not 2380) — small individually, but they reproduce REVIEW-1's F1 critique (an author who miscounts can't be trusted to inventory file moves). The 5-week / ~101h estimate is structurally optimistic for a solo dev — Step 8 alone is 32-40h and is **mislabeled "optional"** while Step 7's `AURA_USE_HUB=true` flag-flip pushes Telegram production through a brand-new path that the soak gate (48h) is the only line of defense for. The hard question — would killing `agent.Runner` alone deliver 70% of the value at 20% of the risk — is yes, and the plan never seriously considers it. Single most important fix: **re-anchor Step 1 to verified `ls` output and explicitly split the plan into a "must-do" core (Steps 1, 5, 6 — cleanup + governance + kill Runner) and an "if-budget-allows" tail (Steps 2, 3, 4, 7, 8)**, so partial completion at week 2 is a viable end state, not a halfway failure.

## Findings

### F1 — BLOCKER: `internal/release/` is NOT empty; deletion silently removes legacy-bundle guard
**Where:** master plan §4 Step 1 line 234 ("Delete `internal/orchestration/`, `internal/tracing/`, `internal/release/` (zero `.go`)") // `D:\Aura\internal\release\release_config_test.go` — 121 LOC `package release_test` with two active guards (`TestGoReleaserDoesNotArchiveLegacyRuntimeBundle`, `TestReleaseWorkflowDoesNotPrepareLegacyRuntimeBundle`)
**Issue:** The plan asserts the dir has zero `.go` files. Verified: it has one test file that actively prevents `install-pyo`+`dide-bundle` and `runtime/pyo`+`dide` from re-entering `.goreleaser.yml` or `.github/workflows/release.yml`. The tests `t.Skip()` when the files are absent, so they're currently no-ops on this Docker-first branch — but deletion removes the trap. If the user ever re-adds a desktop release path, the regression is silent until prod. The plan's §1 line 45 ("**49 directory** (incluse `orchestration/`, `tracing/`, `release/` **vuote** — nessun `.go`)") is wrong for `release/`.
**Fix for v1.2:** Either (a) keep `internal/release/` as-is and remove it from Step 1's delete list, or (b) explicitly state "delete release_config_test.go + its guards, accepting the regression risk if desktop bundling ever returns". Don't conflate the two.

### F2 — BLOCKER: State-table LOC numbers in §1 are stale across the board
**Where:** master plan §1 lines 44-50 // verified via `wc -l`
**Issue:** Plan says `internal/telegram/` = 8239 LOC, agent.Runner = 520, agentloop = 738, chathub = 2380. Verified actuals: 8957, 554, 780, 2601. Every number is wrong, every direction is "code grew under the plan". This is exactly the failure REVIEW-1 F1 called out on PRD v1 (Bot field count). The synthesizer inherited the stale numbers without re-running `wc -l`. If state-of-tree is wrong, the diff post-restructure cannot be measured against the goal (Step 8 acceptance "Somma `internal/telegram/{bot,commands,handlers}.go` ≤ 200").
**Fix for v1.2:** Run `wc -l` on every cited file. Update §1 + every §4 Step that cites a budget against current LOC. Add a one-line check to the v1.2 commit message: "numbers in §1 verified at SHA <head>".

### F3 — BLOCKER: §4 Step 6 "kill agent.Runner" misrepresents swarm's MaxToolCalls semantics
**Where:** master plan §4 Step 6 line 292 ("MaxToolCalls=0 → MaxIterations=0 (unlimited)") and §3 D1 line 84 ("swarm uses unbounded") // `internal/swarm/plan.go:98`, `internal/swarmtools/tools.go:211, 466-467` (clamp to `policy.ChildMaxIterations`) // `internal/agentloop/loop.go:230` (`MaxIterationsCeiling = 50`, `MaxIterations < 1 → MaxIterations = 1`)
**Issue:** Swarm does NOT use `MaxToolCalls=0`. `roleMaxToolCalls(role)` returns positive ints; `tools.go:466-467` explicitly clamps to `policy.ChildMaxIterations` when ≤0 or exceeded. So the migration premise "0 maps to unlimited" is wrong. Worse, even if a caller passes 0, `agentloop.MaxIterations < 1 → MaxIterations = 1` (hard floor), with ceiling `MaxIterationsCeiling = 50`. There is no path to "unlimited" through agentloop today. REVIEW-1 F9 named this; the master plan dismissed it in §6 row "Wave 3 Pack A SUPERSEDED" without re-doing the mapping. The mapping table promised inside Step 6 doesn't exist in the master plan text — it's referenced only as "field mapping table presa da PRD v2 Commit 2.1".
**Fix for v1.2:** Inline the full field-mapping table in §4 Step 6: every field of `agent.Task` (MaxToolCalls, MaxToolResultChars, FinalizationTimeout, CompleteOnDeadline, ToolAllowlist, plus the to-be-added MaxReturnChars/ArtifactPath) → exact destination in `agent.Invocation` or `agentloop.Options`, with semantic gotcha column. Don't defer to "PRD v2". Don't claim "0 means unlimited" — it doesn't.

### F4 — MAJOR: Step 8 is mislabeled "optional" but is load-bearing for half the acceptance criteria
**Where:** master plan §4 Step 8 line 309 ("opzionale ma pianificato") and §9 line 443 ("Step 8 è la rifinitura") // §7 acceptance lines 411, 414-415 (A1 ≤22 dirs, A4 telegram ≤200 LOC, A5 `cmd/aura/app.go` exists)
**Issue:** The plan claims Step 8 is optional and §9 says "chiudere a Step 7 è una scelta ragionevole". But A1 (≤22 dirs), A4 (telegram ≤200 LOC), A5 (`cmd/aura/app.go` ≤300 LOC each wire_*.go) **only land in Step 8**. Step 7 alone leaves: 28→ ~24 dirs (still above target), telegram still 8000+ LOC (god class intact), no composition root. If Step 8 is genuinely optional then A1/A4/A5 must move out of "vince quando" and into "stretch goals". If A1/A4/A5 are the win condition then Step 8 is mandatory and the 32-40h estimate is binding, pushing the realistic schedule to 6-7 weeks calendar at 4h/day — closer to PRD v2's 110h that REVIEW-1 flagged.
**Fix for v1.2:** Pick: either (a) move A1/A4/A5 to "stretch", keep Step 8 optional, and the win is at Step 7; or (b) make Step 8 mandatory and own the 6-7 week estimate honestly. Don't have it both ways.

### F5 — MAJOR: §4 Step 1's "8h" estimate omits cascading test/import friction across 6 merges
**Where:** master plan §4 Step 1 line 236 ("Effort: 8h (1.5h × 6 merge meccanici + 2h sed/test)") // `internal/mcp/` (433 + 303 + 75 LOC), `mcppolicy/` (151 + 52 LOC), `mcpwatch/` (180 + 182 LOC) — three distinct package declarations
**Issue:** Each merge has a per-merge tax the plan doesn't budget: (1) package declaration unification (mcp+mcppolicy+mcpwatch all declare different packages — sed needs to change `package mcppolicy` → `package mcp` in 3 files, then resolve any same-name exports); (2) test fixture relocation (test files reference relative paths to `testdata/`); (3) any `go:generate` directives; (4) the `goimports -w` pass after each sed; (5) re-running `go test ./...` per merge takes 2-4 minutes on first compile; (6) `auth + setup + health → api/` merges three packages with independent `init()` functions — collision risk. Realistic per-merge: 2-3h not 1.5h. Six merges = 12-18h, plus the empty-dir cleanup. The full Step 1 is probably 14-20h, not 8h. The Step 1 promise ("executable today by a subagent without further design") becomes shaky once a single sed produces a non-trivial error.
**Fix for v1.2:** Re-estimate Step 1 at 14-20h. Break it into 7 sub-commits (one per merge + one for empty-dir cleanup), each with its own validation gate and rollback. Move `auth + setup + health → api/` to a separate sub-step because `setup/` owns the first-run wizard (loopback HTTP server) — non-trivial.

### F6 — MAJOR: Scheduler→cron rename actively mislabels 4 of 5 sub-responsibilities
**Where:** master plan §4 Step 7 line 300 ("`git mv internal/scheduler internal/cron`. Sub-cartelle `cron/jobs/`, `cron/maintenance/`, `cron/tick/`") // `internal/scheduler/` files: scheduler.go (231), store.go (594), agent_job.go (209), issues.go (194), maintenance.go (240), wake.go (129)
**Issue:** REVIEW-1 F13 explicitly raised this: scheduler today owns 5 responsibilities (agent_job, issues queue, maintenance, scheduler tick, wake). The master plan's "Sub-cartelle `cron/jobs/`, `cron/maintenance/`, `cron/tick/`" omits `issues/` and `agent_job/` entirely. `agent_job.go` IS where the master plan's "scheduler agent_job as third caller of agent.Runner" lives — re-pointing it during Step 6 (kill Runner) is fine, but the rename in Step 7 doesn't have a target sub-package for it. Also `issues.go` (wiki-issue queue) is unrelated to cron timing — calling it cron is a domain lie.
**Fix for v1.2:** Either (a) keep `internal/scheduler/` name and add a `cron/` sub-package only for the tick mechanism, or (b) rename to `cron/` with sub-packages `cron/{tick,jobs,issues,maintenance,wake,store}/`. Don't pretend the package is mostly cron — it isn't.

### F7 — MAJOR: §4 Step 3 fixture harness ("8h") is itself novel infrastructure
**Where:** master plan §4 Step 3 line 254 ("test(channels/telegram): record-and-replay fixture per streaming edits") // no existing `mockBot.go` or `tele.API` mock in tree — verified
**Issue:** The plan promotes REVIEW-1 F12's concern to its own step (good), but `tele.API` doesn't have a recorded-test mock today. Building a shim that captures Send/Edit/SendDocument deterministically, supports `tele.Context` (concrete struct with unexported fields), and serializes 3 scenarios into snapshot JSON is non-trivial. The 600ms throttle in `streaming.go` is timing-sensitive — the mock must inject a fake clock. The plan budgets 8h; in practice 12-16h is realistic, and the harness only delivers value if Step 4's byte-comparison gate actually exercises representative paths (including the entity-fallback edge cases that streaming.go covers in 194 LOC). If the harness covers 3 scenarios but production hits a 4th, the gate is theater.
**Fix for v1.2:** Either (a) re-estimate Step 3 at 12-16h and explicitly list ≥6 scenarios (simple_reply, with_cot, tool_call, entity_table, document_send, very_long_message_split, cot_only, content_only), or (b) admit Step 3 is best-effort and the real gate is manual Telegram smoke at Step 4 acceptance.

### F8 — MAJOR: Rollback story for Step 6 (kill Runner) is theoretical, not practiced
**Where:** master plan §4 Step 6 line 294 ("Rollback: `git revert` clean — Runner torna in vita") // `agent.Runner` is imported from 10 files including `internal/health/server.go:77`, `internal/api/router.go:169`, `internal/agent/README.md`
**Issue:** The plan promises `git revert` is clean, but Step 6 DELETES `runner.go`, `runner_test.go`, README. If any code written between Step 6 and a rollback decision (e.g. Step 7 swarm-via-Hub bridge calls into the new envelope) depends on the absence of `agent.Runner`, restoring runner.go alone won't rebuild — Step 7 changes have to revert too. This is a one-way door in practice. The plan doesn't say "feature flag" for Step 6 — there's no `AURA_USE_AGENT_RUN=false` toggle. Step 4's `AURA_USE_HUB` is a safety net for Telegram regressions; Step 6 has none for swarm + chatPipe + agent_job regressions.
**Fix for v1.2:** Either (a) add a build-time tag or env flag that keeps `runner.go` compiled-in but dormant until verified, then a separate cleanup commit deletes it, or (b) acknowledge in §8 that Step 6 is a one-way door, and budget a 3-day fix-forward window before Step 7 commences.

### F9 — MAJOR: §7 acceptance criteria mix verifiable with hand-waved
**Where:** master plan §7 lines 411-426 (A1-A15)
**Issue:** Verifiability audit:
- A1 (`(Get-ChildItem internal -Directory).Count ≤ 22`) — concrete PowerShell, verifiable. ✓
- A2 (`Grep agent\.Runner|NewRunner\(`) — concrete. ✓
- A3 (probe_chat returns ChatReply shape) — concrete, but the "± 5 tolerance" on tokens is arbitrary; no fixture captures the baseline.
- A4 (telegram bot+commands+handlers ≤200 LOC) — concrete, but `commands.go` and `handlers.go` don't exist in `internal/telegram/` today; they're invented by Step 8.
- A5 (`cmd/aura/app.go` exists, wire_*.go ≤300) — concrete, depends on Step 8.
- A6 (agent.Run callers outside `internal/chat/*` = 0) — concrete. ✓
- A7 (swarm e2e test) — concrete, but the test file is invented at Step 7. ✓
- A8 (snapshot diff Step 3 vs 4 vs 7 = 0) — concrete. ✓
- A9 (entity rendering probe) — handwave: no test file path cited, no specific markdown fixture.
- A10 (`git diff master..HEAD -- go.mod` zero new deps) — concrete. ✓
- A11 (48h soak metrics) — concrete metrics, but "baseline" isn't captured anywhere.
- A12 (xlsx artifact verify) — concrete, leverages existing probe pattern. ✓
- A13 (`Grep temperature=0` ≥5 matches in wiki/sources) — concrete. ✓
- A14 (tool-arg privacy: send "TOPSECRET", grep logs) — concrete. ✓
- A15 (phantom_guard.go exists post-Step 6) — concrete. ✓
So 11/15 are verifiable today, 4 (A4, A5, A9, A11) depend on artifacts that don't exist yet. That's not bad, but the plan should mark which acceptance criteria are "verifiable now" vs "verifiable only after Step N completes".
**Fix for v1.2:** Add a column "verifiable-when" to the A1-A15 table (now / after-Step-3 / after-Step-7 / after-Step-8). Capture A11 baseline (error rate, p95, RSS slope) as a prereq commit before Step 1.

### F10 — MAJOR: User-abandonment risk not in §8 top-5
**Where:** master plan §8 lines 431-437 (top-5 risks)
**Issue:** Top-5 covers Telegram regression, swarm test break, race condition, planning waves losing context, Wave 3 Pack A obsolete-but-invested. Missing: **user abandons mid-restructure**. The user's MEMORY.md captures "abbiamo scritto troppo codice in maniera disordinata, serve partire dal core principale" (frustration today) and previous sessions show pattern of pivoting mid-plan when a side-quest emerges (Wave 2.9, Wave 2.10, Wave 2.10.b all shipped this session). At week 3 of an 8-step plan, what's the partial-completion story? Plan currently treats it as binary (all-or-nothing). If user stops at Step 4, the system is in a worse state than today (chathub renamed but Telegram still on legacy, web on flag-gated new path).
**Fix for v1.2:** Add R6 to §8: "User pivots mid-plan; partial completion leaves system worse than start". Mitigation: order steps so EACH STEP independently improves the system (Step 1 cleanup is fine to stop after; Step 2-3-4 are atomic together — either all or none; Step 5-6 are atomic together; Step 7-8 are the optional tail). Make this ordering explicit in §4.

### F11 — MAJOR: Tool reconciler + skill loader cache interaction with Step 1 / Step 7 unaddressed
**Where:** master plan §4 Step 1 (no mention) and §4 Step 7c (tools merge) // commits `2367f502` (toolindex reconciler hash chain), `a1347a46` (skill loader Invalidate)
**Issue:** Step 1 merges `mcp + mcppolicy + mcpwatch → mcp/`. The mcpwatch package is the fsnotify trigger for the tool reconciler. After the merge, the reconciler's content-hash chain (Wave 2.10.b, just shipped) must continue to debounce 500ms — if the merge accidentally re-orders init() calls or changes the package boundary the watcher monitors, the reconciler can stop working silently (it'll log "no change" forever). Step 7c merges `tools + toolindex + toolsets + swarmtools → agent/tools/*` — toolindex IS the reconciler's home. Moving it without re-verifying the hash chain risks a regression where a new MCP server adds a tool, the embedding doesn't get computed, and tool discovery degrades silently. The plan doesn't mention either risk.
**Fix for v1.2:** Add to Step 1 acceptance: "Tool reconciler triggers fire on mcp.json edit post-merge (test: append a no-op server, verify reconciler logs hash diff)". Add to Step 7c acceptance: "Toolindex reconciler hash chain unchanged — `Grep -n 'BuildVectorIndex\|Reconciler is sole writer' internal/agent/tools/toolindex/` ≥1 match preserving the Wave 2.10.b invariant".

### F12 — MINOR: `/api/chat` consumer drift — `cmd/probe_chat` shape coupling unstated
**Where:** master plan §7 A3 line 413 (`cmd/probe_chat` verifies shape) // `cmd/probe_chat/main.go:57` (`ChatReply struct` mirrors api.ChatReply)
**Issue:** probe_chat has its own ChatReply struct that mirrors `api.ChatReply` "just enough to deserialize the JSON". If Step 4's collapse rule (`tokens = Prompt + Completion`) changes the production shape, probe_chat MAY still deserialize (Go JSON ignores unknown fields) but its assertions on `ToolCalls` and `LLMCalls` could see different values from the new event-driven runtime. The plan's A3 says "± 5 tolerance" on tokens but doesn't say what tolerance applies to ToolCalls/LLMCalls. The 19 probe cases in `cmd/probe_chat/main.go` (lines 455-1184) have `Verify` callbacks that check `r.ToolCalls > 0` etc. — these may regress under the new runtime without anyone noticing until manual run.
**Fix for v1.2:** Capture a baseline run of `cmd/probe_chat` (all 19 cases) on master HEAD BEFORE Step 1 starts; commit the JSON outputs as `cmd/probe_chat/testdata/baseline-pre-restructure.json`. Use the baseline as the diff target for A3 post-Step-4 and post-Step-6.

### F13 — MINOR: `release_config_test.go` is one example; full empty-dir verification not run
**Where:** master plan §1 line 45 // `Get-ChildItem -Recurse` on `internal/orchestration`, `internal/tracing`, `internal/release` shows the first two are truly empty but release/ has 1 file
**Issue:** Plan does NOT enumerate the rest of the suspect single-file packages (REVIEW-1 F6 raised this on `tracing/`, `debugguard/`, `dbrecovery/`, `install/`, `release/`). If `release/` was misclassified once, others may be too. `install/` recently grew via Wave 2.10 (`eb7e61ad`). `dbrecovery/` and `debugguard/` are listed for Step 1 merge — verify each is non-trivial first.
**Fix for v1.2:** Run a script: `Get-ChildItem internal -Directory | ForEach-Object { @{Name=$_.Name; GoFiles=(Get-ChildItem $_.FullName -Filter *.go -Recurse).Count; LOC=(Get-ChildItem $_.FullName -Filter *.go -Recurse | Get-Content | Measure-Object -Line).Lines} }` and inline the output in §1.

### F14 — MINOR: Codex Jan 2026 citation is fine but cache-discipline lock is hand-waved
**Where:** master plan §3 D13 lines 180-195, §4 Step 6 mentions Context-Eng Fase 1 as prereq // no test file or commit hook enforces the invariants
**Issue:** D13 lists 3 invariants (static-first prefix, tool order stability, append-not-mutate runtime ctx). It says "ordering test in `internal/conversation/`" but no such test exists today. Without an enforcing test, the lock is a comment in a doc — the next refactor can violate it and nothing fails. Codex PR #2611 fixed exactly the tool-order bug (cited correctly). Aura needs the equivalent: a unit test that asserts `tools/registry.go` ordering is stable across calls.
**Fix for v1.2:** Before Step 6 lands, ship a small test commit: `internal/tools/registry_order_test.go` asserts `Registry.List()` returns tools in lexicographic-or-deterministic order on N consecutive calls. Same for the system prompt: snapshot system-prompt-bytes for a fixed input and assert equality across two consecutive calls.

### F15 — MINOR: §6 disposition row "Wave 3 Pack A SUPERSEDED" promises an equivalence without showing it
**Where:** master plan §6 line 384 ("phase si traduce come `agent.Event` + `chat.OutboundEvent{Type: phase}`")
**Issue:** Wave 3 Pack A's value was 5 typed phase constants emitting OTel spans (`gen_ai.aura.agent.phase`) with progressive Telegram surface. The master plan says "post-Step 7 these emerge gratis from the Hub". Verified: `agentruntime.Event` does have `EventLLMDelta`, `EventToolStart`, `EventToolEnd`, `EventFinal` etc. — but no `phase` field. Mapping Pack A's 5 phases (e.g. "planning", "delegating", "synthesizing") onto these events is NOT free — somebody must emit the phase semantically. The plan says "swarm emits chat.OutboundEvent{Type: phase}" but doesn't say who decides the phase string or where the enum lives.
**Fix for v1.2:** In §4 Step 7a, add explicit deliverables: "Define `chat.PhaseEvent struct{Name string, RunID string, ParentRunID string}`. Swarm Manager emits phase transitions at 5 fixed lifecycle points (assignment_created, worker_started, worker_finished, synthesis_started, synthesis_finished). Telegram outbound subscribes and edits the running message with a phase indicator (e.g. emoji + text)." Then Pack A is genuinely SUPERSEDED instead of "deferred under a different name".

## The downscope option

**The 70%-value / 20%-risk alternative:**

A 4-commit plan, ~2 weeks calendar at 4h/day (~32-40h work), executed sequentially on master:

1. **`chore(cleanup): delete empty internal/{orchestration,tracing} + verify release` (4h).** Truly empty dirs go. `release/` stays untouched (see F1). No merges. No renames. `go build ./... && go test ./...` green.

2. **`refactor(agent): extract governance + merge agentloop/agentruntime → internal/agent` (12h).** This is the master plan's Step 5. Zero feature change, big clarity win. Governance package is testable independently. Sets up Step 3 below.

3. **`refactor(agent): kill agent.Runner — route swarm + chatPipe + scheduler agent_job to agent.Run` (10h).** Master plan's Step 6. The single most consequential commit in the entire plan — kills 554 LOC of duplicate runtime, unblocks every future agent feature, and is fully revertable in isolation because nothing else has moved. Field-mapping table done in-commit. `MaxToolCalls` → `MaxIterations` with explicit clamp documentation.

4. **`chore(tools): merge internal/{toolindex,toolsets,swarmtools} → internal/tools/{index,sets,swarm}` (6-8h).** Mechanical merge of related packages, no logic changes. Wave 2.10.b reconciler hash chain verified. Wave 1 tasks 1,2,4,5 can now ship on the merged surface.

**Total: ~32-40h, ~2 weeks at 4h/d.** Deliverables:
- One canonical agent runtime (the master plan's #1 goal).
- Governance testable in isolation.
- Tool surface flat (4 dirs → 1).
- No Telegram changes. No chathub adoption. No composition-root move. No new feature flag. No 48h soak.

**What we DON'T do (vs master plan):**
- chathub stays parked. The Telegram regression in `chathub/adapters/telegram/outbound.go` is irrelevant because zero callers in production hit it. The 2601 LOC sit dormant. They can be deleted later or revived later.
- `internal/telegram/setup.go` 984 LOC stays. God class is a code smell, not a runtime bug. Live with it.
- 49 dirs → ~42, not 22. Acceptable: domain clarity > arithmetic.
- Wave 3 Pack A status: unchanged. The user CAN ship it later on the post-restructure agent.Run path or not — their choice.

**Why this beats the master plan:**
- Each commit is independently valuable. User can stop after commit 1, 2, 3, or 4 and the system is monotonically better.
- Rollback per commit is straightforward (no flag-flips, no soak gates).
- 2 weeks is realistic at 4h/day with interruption. 5 weeks is not.
- The master plan's biggest claims (single runtime, REUSABLE-CODE invariant, agent.Runner dies) are delivered by commits 2+3. The rest (Hub adoption, composition root, channels/) is **organizational tidiness, not technical necessity**.

**When the master plan beats this downscope:**
- If the user genuinely wants chathub to become the single chat surface (Web SSE later, etc.) — then Steps 2-4 of the master plan are required. But §3 non-goal #8 says SSE is out of scope, so this isn't on the table.
- If the user is committing to a 6-week campaign with no detours — but MEMORY.md and recent commit history (Wave 2.9, 2.10, 2.10.b shipped in sequence as opportunities arose) suggest pivot-driven solo work, not waterfall.

## Verdict justification

**DOWNSCOPE_RECOMMENDED.** The master plan honors the hard invariants (verified across CLAUDE.md, MEMORY.md, REVIEW-1, REVIEW-2) and the 9-document supersession is technically warranted in terms of consolidating planning state. But three problems converge: (1) Step 1 is not executable today as written — `release/` is not empty, state-table LOC numbers are stale (F1, F2, F13); (2) the 5-week claim is optimistic — Step 8 alone is 32-40h and "optional" is a label that hides the fact that A1/A4/A5 only pass after Step 8 (F4); (3) the plan treats partial completion as failure — but the user's session pattern is pivot-driven, so a plan that delivers value monotonically per commit is strictly better than one that requires 7 sequential steps to ship a coherent improvement (F10). Killing `agent.Runner` (Step 6) is the single highest-leverage commit in the entire plan; everything else is either prerequisite cleanup or post-kill organization. **Recommend: execute the 4-commit downscope above, then revisit whether the remaining Steps 2/3/4/7/8 still feel necessary against the cleaner post-kill codebase. If they do, plan them then with current numbers and a tighter scope.** The master plan is **not salvageable as-is with v1.2 fixes** — the v1.2 patches would address each finding individually, but the underlying mismatch (multi-step waterfall vs solo pivot-driven work pattern) requires a structural rethink, not patches.

— END REVIEW —
