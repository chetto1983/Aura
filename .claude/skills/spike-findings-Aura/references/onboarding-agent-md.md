# onboarding agent md

Phase 14: per-identity `Agent.md` filesystem profile + first-run Telegram onboarding. This feature SHIPPED to the codebase (`internal/profile`, `internal/onboarding`, `internal/channels/telegram/onboarding.go` + `profile_onboarding.go`, `internal/conversations/context.go`). The four spikes below are the proven design contract behind it. Use this blueprint to extend/repair without re-spiking.

## Requirements

These are the binding non-negotiables from `MANIFEST.md` Session-9 (spikes 036-039). A future build MUST honor each.

- **`Agent.md` is injected as protected user-role `messages[1]`, never a second system message.** `messages[0]` stays the byte-stable Aura system prompt. PRD Slice 10 wording "second system message" is STALE/SUPERSEDED — do not implement it. Mechanism: `internal/conversations/context.go` `injectAlwaysBlock` (seq=2 / messages[1], protected by ladder layers L1/L2.5 exactly like the system L0 turn).
- **`messages[1]` composition order is profile first, then always-on skills.** Profile updates may change `messages[1]`, but must NEVER mutate `messages[0]`. Bounded block (spike cap 32 KiB). Verified by `internal/agent/prompt/PrefixHash` over `[]int{0}` and `[]int{1}`.
- **`Agent.md` is a filesystem profile at `~/.aura/agents/<identity>/Agent.md`.** Neo4j is OUT of scope for this file — graph memory is Phase 15. The four profile files live together in that one identity dir: `Agent.md`, `preferences.json`, `metadata.json`, `changelog.md`.
- **Profile writes are atomic: same-directory temp file → file `Sync()` → platform-aware replacement.** On Windows bare `os.Rename` is INSUFFICIENT for overwrite; use `MoveFileEx(..., MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)`. Directory fsync is best-effort on Windows (Access-denied is tolerated; POSIX keeps the stronger dir-sync path). Mechanism: `internal/profile/store.go` + `atomic_windows.go` / `atomic_posix.go`.
- **Identity path validation rejects traversal and separators before joining under the profile root.** Regex `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$` plus an explicit `strings.Contains(id, "..")` guard plus a `filepath.Rel` escape check. Mechanism: `internal/profile/store.go` `profileDir` + `ErrInvalidIdentity`.
- **Telegram onboarding is a workflow agent: first message (or `/onboard`) enters `LoopAgent[InterviewStepAgent]`; confirm/edit/skip are terminal state paths; profile writes happen only AFTER `Actions.Escalate=true`, then normal chat resumes.** Mechanism: `workflow.NewLoop` + terminal `Event{Actions.Escalate=true}` carrying the final `StateDelta`.

## How to Build It

### 1. Profile store (`internal/profile`) — spike 038, SHIPPED

Layout under `~/.aura/agents/<identity>/`: `Agent.md`, `preferences.json`, `metadata.json`, `changelog.md`. Struct shapes proven in the spike:

```go
type preferences struct {
    Lang, Timezone, TonePreference, ResponseLength string
    VoiceMode, CanProactiveMessage                 bool   // json tags: lang, timezone, voice_mode, can_proactive_message, tone_preference, response_length
}
type metadata struct {
    Version             int    // json: version
    OnboardingCompleted bool   // json: onboarding_completed
    GeneratedAt         string // RFC3339
    LastUpdatedAt       string // RFC3339
}
```

`WriteProfile(identity, p)`: `MkdirAll(dir, 0o700)`, then `atomicWrite` each of `Agent.md` / `preferences.json` / `metadata.json` (both JSON via `json.MarshalIndent(..., "", "  ")` + trailing `\n`). `changelog.md` is APPEND-only: read old bytes (tolerate `os.ErrNotExist`), prepend a `## <RFC3339>\n\n<change>\n\n` entry, atomic-write the concatenation. A blank `Change` writes NO changelog entry (shipped refinement).

`atomicWrite(path, data)` — the exact proven recipe:
```go
tmp, _ := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")  // same-dir temp
tmp.Write(data); tmp.Sync(); tmp.Close()
replaceFile(tmpName, path)        // platform split
syncDirBestEffort(dir)           // logs+ignores on Windows
// defer os.Remove(tmpName) guarded by a `cleanup` bool flipped false after success
```

Platform-split `replaceFile`:
- `atomic_windows.go` (`//go:build windows`): `MoveFileEx(srcPtr, dstPtr, MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`. The spike used `golang.org/x/sys/windows`; the SHIPPED version calls `kernel32.dll!MoveFileExW` via `syscall.NewLazyDLL` + `unsafe.Pointer` with `#nosec G103` annotations (either is acceptable).
- `atomic_posix.go` (`//go:build !windows`): plain `os.Rename(src, dst)`.

`profileDir(identity)` validation (defense in depth, all three checks):
```go
if !identityPattern.MatchString(identity) || strings.Contains(identity, "..") { return "", ErrInvalidIdentity }
root, _ := filepath.Abs(s.root); dir, _ := filepath.Abs(filepath.Join(root, identity))
rel, _ := filepath.Rel(root, dir)
if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) { return "", err }  // escapes root
```
Rejected identities proven in the spike: `""`, `"../evil"`, ``..\evil``, `"a/b"`, `` `a\b` ``, `".hidden"`, `"local..evil"`. `ReadProfile` reconstructs all four files and unmarshals the two JSON files (round-trips lang/voice_mode and metadata.version).

### 2. messages[1] cache-invariant injection (`internal/conversations/context.go`) — spikes 036/037, SHIPPED

The seam already existed for always-on skills: a protected synthetic user-role turn at seq=2 (messages[1]) injected right after the system L0 turn, protected by ladder layers L1/L2.5. Phase 14 EXTENDS the provider of that block to render profile-first, then skills. Render order inside `messages[1]` (spike 037 `renderMessages1`):
```
<profile:Agent.md>
<trimmed Agent.md>
</profile:Agent.md>

<always-on:skills>
<trimmed always-on skill block>
</always-on:skills>
```
Shape assertions the build must keep: `messages[0].Role == system` AND does NOT contain `"Agent.md"`; `messages[1].Role == user`; `len(messages[1]) <= 32*1024`; profile marker index < always-on marker index; volatile budget/workspace tail lands on the LAST message (`<budget>...`), never disturbing `messages[0/1]`.

Verify with the real prompt path:
```go
h0, _ := prompt.PrefixHash(req.Messages, []int{0})
h1, _ := prompt.PrefixHash(req.Messages, []int{1})
```
Spike 037 ran a 20-turn replay with volatile `prompt.Budget{Used,Remaining,Workspace}` tails: `messages[0]` AND `messages[1]` hashes stayed byte-stable; a profile update changed `messages[1]` only (`messages[0]` unchanged). Reproduce: `go run ./.planning/spikes/037-agentmd-messages1-cache-invariant`. Extend the hidden cache audit (`cmd/aura/cache_audit.go`) to expose: messages[0] hash (stable), messages[1] hash (stable until profile update), profile-before-skill order assertion, bounded messages[1] size, plus `aura profile show --identity <id>` section tree and a `changelog.md` view.

### 3. Telegram onboarding LoopAgent (`internal/onboarding` + `internal/channels/telegram/onboarding.go`) — spike 039, SHIPPED

Run onboarding as `workflow.NewLoop("TelegramOnboardingLoop", maxIters, interviewStepAgent)` over Aura's real `agent.Agent`/`agent.Event`/`agent.Actions`. The InterviewStepAgent yields natural-language onboarding events as NON-tool events; the loop terminates on `Event{Actions.Escalate=true}`. KEY insight: the interview itself needs NO tool-call steps. The Telegram adapter keeps all UI/HITL (buttons, `/onboard`, first-message detection, confirm/edit/skip mapping) OUTSIDE the workflow and maps the final `StateDelta` into the profile store.

Three terminal paths proven (deterministic scenarios):
- **confirm**: name question → preferences question → draft → `Escalate=true` with StateDelta `{onboarding_completed:true, agent_md:<md>, preferences_json:<json>, resume_chat:true}`.
- **skip**: a SINGLE event, immediately `Escalate=true` with `{skipped:true, resume_chat:true}` — no profile write.
- **edit**: draft → revised draft → `Escalate=true` with the edited `agent_md`.

`InvocationContext` wiring from the spike: `agent.NewBudget(BudgetOptions{MaxSteps:&n})`, `Branch:"telegram.onboarding"`, `RequestID: uuid.Must(uuid.NewV7())`. Reproduce: `go run ./.planning/spikes/039-telegram-onboarding-loopagent-prototype`. The adapter, after escalation, calls `profile.Store.WriteProfile(identity, ...)` (confirm/edit) or just resumes chat (skip), keyed by `resume_chat`/`skipped`/`onboarding_completed` in the StateDelta.

## What to Avoid

- **Do NOT add a second system message for `Agent.md`.** The PRD Slice 10 "second system message" wording is stale/superseded by spike 036 — it breaks the KV-cache invariant because `messages[0]` is the static cache anchor. This is the single biggest landmine; the whole feature exists to avoid it.
- **Do NOT put profile/memory into the system prompt** the way `nanobot/agent/context.py` and `picobot/internal/agent/context.go` do (they assemble AGENTS/SOUL/USER/memory into the system prompt). Studied in spike 036 as the WRONG cache posture for Aura — every profile edit would be a cache miss on the provider prefix.
- **Do NOT rely on bare `os.Rename` to overwrite an existing file on Windows.** Spike 038 confirmed POSIX `rename(2)` overwrite semantics do not map to Windows; you need `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`. Without it, atomic profile rewrites silently fail on the operator's Windows machine.
- **Do NOT treat directory fsync as mandatory on Windows.** Spike 038 observed `directory sync skipped: ... Access is denied.` — make it best-effort (log + continue) on Windows; keep the stronger dir-sync only on POSIX.
- **Do NOT skip identity validation before joining the path.** `../evil`, `a\b`, `a/b`, `.hidden`, `local..evil`, empty string all must be rejected. Regex alone is not enough — keep the explicit `..` check AND the `filepath.Rel` escape check.
- **Do NOT model the interview as tool-call steps.** Spike 039 proved the interview is natural-language non-tool events; the only structural signal is the terminal escalate event. Over-engineering it with per-question tool calls is unnecessary.
- **Do NOT let always-on skills render before the profile in `messages[1]`.** Order is fixed: profile first, skills second. The shape assertion fails otherwise and the order itself is part of the cache contract.

## Constraints

- **`messages[1]` byte cap: 32 KiB** (`maxMsg1Bytes = 32 * 1024` in spike 037). Bounded fragment, profile-first.
- **Profile root**: `~/.aura/agents/<identity>/`. Files: `Agent.md`, `preferences.json`, `metadata.json`, `changelog.md`. Dir perms `0o700`.
- **Identity regex**: `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$` (max 64 chars, no leading dot, no separators, no `..`).
- **Windows replace flags**: `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH`. Windows API `MoveFileExW` (kernel32.dll). Spike used `golang.org/x/sys/windows`; shipped uses `syscall.NewLazyDLL` + `unsafe` + `#nosec G103`.
- **Timestamps**: RFC3339 UTC (`time.Now().UTC().Format(time.RFC3339)`) for `generated_at`, `last_updated_at`, and changelog headers.
- **Temp file pattern**: `os.CreateTemp(dir, "."+base+".tmp-*")` — same directory as target so the rename/MoveFileEx is intra-volume. Post-write assertion: no residual `.tmp-` files in the identity dir.
- **Workflow types**: `workflow.NewLoop(name, maxIters, agent)`, terminal signal `agent.Event{Actions.Escalate=true}`, payload in `Actions.StateDelta map[string]any`. Budget via `agent.NewBudget(agent.BudgetOptions{MaxSteps:&n})`. Branch tag `telegram.onboarding`.
- **StateDelta keys** the adapter reads: `onboarding_completed`, `agent_md`, `preferences_json`, `skipped`, `resume_chat`, `profile_draft`, `onboarding_step`.
- **Hash API**: `prompt.PrefixHash(messages, []int{idx})` for cache-invariant verification.
- **Out of scope**: Neo4j / graph memory (Phase 15). `Agent.md` is filesystem-only.

## Origin

Synthesized from spikes 036, 037, 038, 039. Source files in: `sources/036-phase14-agentmd-source-truth/` (README only — research/source-audit spike), `sources/037-agentmd-messages1-cache-invariant/` (README + main.go), `sources/038-profile-store-atomic-contract/` (README + main.go + replace_windows.go + replace_posix.go), `sources/039-telegram-onboarding-loopagent-prototype/` (README + main.go). Verdicts: 036 VALIDATED, 037 VALIDATED, 038 VALIDATED, 039 VALIDATED. All four SHIPPED to `internal/profile`, `internal/onboarding`, `internal/conversations/context.go`, and `internal/channels/telegram/{onboarding,profile_onboarding,bot_dispatch_onboarding}.go`. Phase 14 status per CLAUDE.md: implemented + automated-green; only the live Telegram operator sign-off + ROADMAP flip remain (`.planning/phases/14-*/14-VALIDATION.md`, `automated_passed_manual_pending`).
