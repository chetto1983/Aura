---
phase: 14-onboarding-agent-md
plan: 02
subsystem: prompt-context
tags: [agent-md, profile-context, messages-1, cache-audit, runner]

requires:
  - phase: 14-onboarding-agent-md
    plan: 01
    provides: filesystem Agent.md profile store and config knobs
provides:
  - Identity-aware Runner context block provider
  - Profile-first Agent.md rendering into the protected messages[1] context block
  - Context ladder regressions for profile/skills block survival through L1 and L2.5
  - Profile-aware cache-audit replay and shell gate
affects: [phase-14, prompt-cache, scheduler, channel-onboarding]

tech-stack:
  added: []
  patterns: [identity-resolved context provider, profile-first messages1 composition, cache-audit stable hash streams]

key-files:
  created:
    - internal/runner/profile_context_test.go
    - cmd/aura/profile_context_test.go
    - internal/conversations/context_profile_test.go
  modified:
    - internal/runner/interfaces.go
    - internal/runner/runner.go
    - internal/runner/fakes_test.go
    - internal/identity/store.go
    - internal/profile/render.go
    - internal/profile/render_test.go
    - cmd/aura/chat.go
    - cmd/aura/serve_adapters.go
    - cmd/aura/cache_audit.go
    - cmd/aura/cache_test.go
    - cmd/aura/cachefakes.go
    - cmd/aura/cmdfakes_test.go
    - scripts/cache_invariant_audit.sh

key-decisions:
  - "The Runner resolves the conversation owner identity and passes the full identity to the context provider instead of relying on process-global `local`."
  - "Agent.md is wrapped in profile markers and composed before always-on skill instructions inside the existing protected messages[1] user-role block."
  - "The cache audit seeds a deterministic `local` profile and hashes messages[1] as a profile/skills stream alongside messages[0] and the skill manifest."

patterns-established:
  - "Profile lookup prefers identity name and falls back to identity UUID, matching CLI-created `local` profiles while preserving ID-addressable profiles."
  - "The in-memory cache-audit store honors ContextConfig.AlwaysBlock so the audit exercises the same messages[1] shape the runner requests from production stores."

requirements-completed: [UX-05, CAP-04]

duration: ~35 min
completed: 2026-06-11
---

# Phase 14 Plan 02: Profile Context Injection Summary

**Agent.md now enters the prompt as a profile-first, identity-aware, cache-audited messages[1] block.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-11T11:25:00+02:00
- **Completed:** 2026-06-11T18:22:00+02:00
- **Tasks:** 4
- **Files modified:** 16

## Accomplishments

- Added `runner.ContextBlockProvider` and made the Runner resolve the conversation identity before rendering profile context.
- Added `profile.RenderContextBlock` markers and bounded Agent.md wrapping.
- Wired `aura chat` and serve composition roots so Agent.md is rendered before always-on skill instructions.
- Added context ladder tests proving profile/skills context survives L1 tool eviction and L2.5 hard-drop reduction.
- Made `aura cache-audit` seed a deterministic `local` profile, include it in messages[1], hash it, and fail clearly on profile/skills drift.
- Updated the shell cache gate to count and report profile-aware `messages1 NN:` lines.

## Task Commits

1. **Task 1: runner context block identity-aware** - `18e31d3d`
2. **Task 2: compose profile-first messages[1] block and wire chat/serve** - `4961f66a`
3. **Task 3: context ladder profile/skills regressions** - `6de13ca3`
4. **Task 4: profile-aware cache audit** - `9f4db4e1`

## Files Created/Modified

- `internal/runner/interfaces.go` - Added the identity-aware context block provider seam.
- `internal/runner/runner.go` - Composes profile context before always-on skill context.
- `internal/identity/store.go` - Added identity lookup by UUID for conversation owner resolution.
- `internal/profile/render.go` - Added profile block markers and Agent.md context block rendering.
- `cmd/aura/serve_adapters.go` - Added `profileContextProvider`.
- `cmd/aura/chat.go` - Wires profile context into the chat Runner.
- `internal/conversations/context_profile_test.go` - Protects profile/skills context through L1 and L2.5.
- `cmd/aura/cache_audit.go` - Seeds deterministic Agent.md and hashes profile-aware messages[1].
- `cmd/aura/cachefakes.go` - Applies the rendered context block in the in-memory audit store.
- `cmd/aura/cache_test.go` - Adds profile shape and profile-mutation negative tests.
- `scripts/cache_invariant_audit.sh` - Updates count/diff diagnostics for the profile/skills stream.

## Decisions Made

- Kept the existing `ContextConfig.AlwaysBlock` field name to avoid churn in the ladder; the Runner now supplies a combined profile-plus-skills block.
- Resolved profiles by identity name first so `aura profile --identity local` is immediately consumed by the default conversation identity.
- Kept missing or invalid profiles non-fatal: the provider returns an empty block, while unexpected read errors are warned and omitted.

## Deviations from Plan

### Operational Deviations

**1. Concurrent Phase 20 edits interleaved with verification**
- **Found during:** Task 4 shell cache gate
- **Issue:** The first `bash scripts/cache_invariant_audit.sh` run saw a transient compile mismatch in `cmd/aura/serve.go` while Phase 20 edits were landing.
- **Fix:** Did not revert or fold Phase 20 work into this plan. Re-ran verification after the worktree settled.
- **Verification:** `go test ./cmd/aura -run CacheAudit -count=1`, `bash scripts/cache_invariant_audit.sh`, `go build ./...`
- **Committed in:** N/A

---

**Total deviations:** 1 operational deviation, 0 product-code deviations.
**Impact on plan:** No behavior change; verification passed after the concurrent edit settled.

## Verification

- `go test ./internal/runner/ -run Profile -count=1`
- `go test ./internal/profile/ ./cmd/aura/ -run 'Profile|AlwaysBlock|Serve|Chat' -count=1`
- `go test ./internal/conversations/ -run 'AlwaysBlock|Profile' -count=1`
- `go test ./cmd/aura -run CacheAudit -count=1`
- `bash scripts/cache_invariant_audit.sh`
- `go test ./internal/profile/ ./internal/conversations/ ./internal/runner/ ./cmd/aura/ -run 'Profile|AlwaysBlock|CacheAudit' -count=1`
- `go build ./...`

## User Setup Required

None.

## Next Phase Readiness

Plan 14-03 can now rely on an identity-aware Agent.md context block that is already profile-first, protected by the context ladder, and covered by cache-invariant gates.

---
*Phase: 14-onboarding-agent-md*
*Completed: 2026-06-11*
