# Phase 14: Onboarding + Agent.md - Research

**Researched:** 2026-06-08
**Domain:** Filesystem profile store, prompt-prefix composition, Telegram profile onboarding, profile CLI, cache-invariant verification
**Confidence:** HIGH. Phase 14 was preceded by validated spikes 036-039, plus online prompt-cache/memory research and a local `D:/tmp` source audit.

> Scope note: this research intentionally uses the operator-approved spike artifacts instead of spawning a new researcher. No `CONTEXT.md` exists for Phase 14; the operator approved continuing without it and using the spike artifacts as the source of truth.

## Binding Verdicts

The locked design comes from `.planning/spikes/036-phase14-agentmd-source-truth/README.md`:

- `Agent.md` is stored per identity on disk at `~/.aura/agents/<identity>/Agent.md`.
- `Agent.md` is injected as protected user-role context at `messages[1]`.
- `Agent.md` is not a second system message. PRD wording that says "second system message" is stale.
- `messages[0]` remains Aura's byte-stable system prompt and must not include profile content.
- `preferences.json`, `metadata.json`, and `changelog.md` sit beside `Agent.md`.
- Telegram profile onboarding writes the profile after confirm/edit/skip handling, then normal chat resumes.

The best industrial reference is `D:/tmp/codex`:

- `codex-rs/core/src/agents_md.rs`: bounded instruction discovery, root-to-cwd order, override/fallback precedence, byte caps, invalid UTF-8 warnings.
- `codex-rs/core/src/context/fragment.rs`: typed marked user-context fragments, a good mental model for Aura's `messages[1]` profile block.
- `D:/tmp/codex/AGENTS.md`: model-visible context must be bounded and must avoid frequent cache misses.

`D:/tmp/nanobot` and `D:/tmp/picobot` are useful for profile/memory file layout and atomic write ideas, but their system-prompt context assembly is an anti-pattern for Aura because it would poison `messages[0]`.

## Phase Requirements

| ID | Requirement | Research Support |
|----|-------------|------------------|
| UX-05 | User onboarding + `Agent.md` profile per identity, filesystem `~/.aura/agents/<id>/Agent.md`, injected as `messages[1]` not `messages[0]` | Spikes 036-039; ROADMAP Phase 14 success criteria; current `internal/conversations/context.go` protected user-role injection seam; `cmd/aura/cache_audit.go` hash stream. |

## Current Aura Seams

### Profile Injection

Current code already has a protected synthetic user-role turn:

- `internal/runner/runner.go`: `Deps.AlwaysBlock func() string`, `contextConfig()` passes it to conversations.
- `internal/conversations/context.go`: `ContextConfig.AlwaysBlock` is injected as a protected user-role turn immediately after a leading system turn; `dropOldestPairs` preserves it like the system prompt.
- `cmd/aura/cache_audit.go`: hashes `messages[0]` and `messages[1]` when present.
- `internal/agent/prompt/hash.go`: `PrefixHash(msgs, indices)` is the stable content fingerprint.

Phase 14 should evolve this seam from "always-on skills only" to "profile-first context block plus always-on skills". The safest implementation is to make Runner resolve a context block per conversation/identity instead of keeping a context-free `func() string`.

Recommended shape:

```go
type ContextBlockProvider func(ctx context.Context, identityID string) string
```

Runner can read the conversation owner via `ConversationStore.Get(ctx, convID)` before loading managed history, render the block from that identity, then pass it through `ContextConfig.AlwaysBlock` until the field is renamed in a later cleanup. This keeps the context ladder and cache-audit machinery unchanged while giving the provider identity awareness.

### Profile Store

Spike 038 validated:

- Same-directory temp file.
- File fsync.
- Platform-aware replacement.
- Best-effort directory fsync.
- Identity path validation before `filepath.Join`.

Windows-specific finding:

- Do not rely on bare `os.Rename` for overwriting an existing destination.
- Use `MoveFileEx(..., MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)` on Windows.
- Directory fsync is denied on Windows and must be best-effort.

Recommended package:

```text
internal/profile/
  store.go
  render.go
  parser.go
  atomic_posix.go
  atomic_windows.go
  store_test.go
  render_test.go
```

### Telegram Onboarding

Phase 13 already has a setup-token onboarding flow:

- `internal/channels/telegram/onboarding.go`: consumes `/start <token>` and writes `telegram_accounts`.
- `internal/channels/telegram/bot_dispatch.go`: handles `/start <token>`, commands, HITL, media, and normal turns.

Phase 14 must not confuse this with profile onboarding. Setup onboarding links the Telegram account. Profile onboarding creates `Agent.md`.

Recommended routing:

1. `/start <token>` remains setup/account activation.
2. After setup activation succeeds, if no profile exists for the identity, send the first profile-onboarding question.
3. On ordinary first message from a linked identity with no profile, enter profile onboarding before normal LLM turn.
4. `/onboard` restarts profile onboarding explicitly.
5. `/profile` or `/whoami` can show a short profile summary, but the roadmap requires the CLI `aura profile show` path.

The onboarding workflow should be an `agent.Agent` composition:

```text
workflow.NewLoop("ProfileOnboardingLoop", maxIter, InterviewStepAgent)
```

Spike 039 validated confirm, skip, and edit terminal paths using `Actions.Escalate=true` and `StateDelta` carrying `agent_md`, `preferences_json`, `skipped`, and `resume_chat`.

### CLI Surface

Roadmap success criteria require:

- `aura profile show --identity local`
- `aura profile add-fact "I prefer Italian responses"`

The CLI should use the same `internal/profile.Store` as Telegram. It must not write directly from `cmd/aura`.

Recommended subcommands:

```text
aura profile show --identity local
aura profile add-fact --identity local "I prefer Italian responses"
aura profile set-preference --identity local response_length short
aura profile forget --identity local "fact text"
```

Only `show` and `add-fact` are required by ROADMAP. `forget` is low-cost and aligns with PRD memory-control language, but if schedule pressure appears it can be deferred after `show/add-fact`.

## Testing Strategy

Automated tests:

- `internal/profile`: path traversal, atomic replacement, read/write round trip, parser/render tree, Windows replacement via unit seams.
- `internal/runner`: profile provider loads identity-specific profile and leaves `messages[0]` unchanged.
- `internal/conversations`: protected profile/skills block survives L2.5 trimming.
- `cmd/aura`: profile CLI show/add-fact and cache-audit output lines.
- `internal/onboarding`: LoopAgent confirm/skip/edit state deltas.
- `internal/channels/telegram`: first-message routing, `/onboard`, profile callback confirm/edit/skip, no duplicate normal turn while onboarding consumes a message.

Live/manual:

- Real Telegram account after setup receives profile onboarding.
- Confirm writes `~/.aura/agents/local/Agent.md`.
- `aura profile show --identity local` renders sections.
- `aura profile add-fact "I prefer Italian responses"` atomically updates the file.
- Next conversation injects the updated profile at `messages[1]`.
- `scripts/cache_invariant_audit.sh` still passes.

## Open Risks

| Risk | Mitigation |
|------|------------|
| Profile content accidentally enters `messages[0]` | Extend `cache-audit` and runner tests to assert `Agent.md` is absent from `messages[0]` and present at `messages[1]`. |
| Always-on skills and profile share one `messages[1]` invalidation domain | Accept for Phase 14, but compose profile first and hash `messages[1]` in cache audit. A later cleanup can split stable tiers if needed. |
| Telegram setup onboarding confused with profile onboarding | Keep separate handlers and names: setup account activation vs profile onboarding. Tests must prove `/start <token>` does not drive the LLM and normal first message enters profile onboarding only after account setup. |
| Crash corrupts profile files | Same-directory temp write + file fsync + platform-aware replace; no bare Windows `os.Rename` overwrite. |
| User wants to skip onboarding | Skip path writes metadata/changelog and resumes chat without an `Agent.md` profile, or writes a minimal empty profile with `onboarding_skipped=true`; choose one in implementation, but make the next turn behavior explicit. |

## Recommended Plan Breakdown

1. Profile store and profile CLI.
2. Runner `messages[1]` profile injection and cache audit.
3. Onboarding workflow agent.
4. Telegram profile onboarding integration.
5. E2E/live sign-off and Phase 14 documentation closure.

## RESEARCH COMPLETE

This file is the Phase 14 research artifact synthesized from validated spikes 036-039 and current Aura code seams.
