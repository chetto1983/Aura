---
phase: 14
slug: onboarding-agent-md
status: passed
nyquist_compliant: true
live_signoff: 2026-06-14
created: 2026-06-08
validated_by: gsd-plan-phase 14
---

# Phase 14 - Validation Strategy

Per-phase validation contract for Phase 14 execution. This is planned coverage; execution must update statuses and evidence paths as tasks land.

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing`, package-level goleak where goroutines are spawned, build tags for live Telegram checks |
| Quick run command | `go test ./internal/profile/ ./internal/onboarding/ ./internal/conversations/ ./internal/runner/ ./internal/channels/telegram/ ./cmd/aura/ -count=1` |
| Race command | `go test -race ./internal/profile/ ./internal/onboarding/ ./internal/channels/telegram/ ./internal/runner/ ./cmd/aura/` |
| Full suite command | `go test ./... -count=1` |
| Live tag tier | `telegram_integration` or manual operator evidence for first-run profile onboarding |

## Per-Task Verification Map

| Req | Behavior | Test Type | Automated Command | Evidence Files | Status |
|-----|----------|-----------|-------------------|----------------|--------|
| UX-05 | Profile files write/read atomically under `~/.aura/agents/<id>/` | unit + Windows seam | `go test ./internal/profile/ -run 'Store|Atomic|Path|Profile'` | `internal/profile/store_test.go`, `internal/profile/atomic_*_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | `aura profile show --identity local` renders Agent.md sections as a tree | unit + CLI | `go test ./cmd/aura/ -run Profile` | `cmd/aura/profile_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | `aura profile add-fact` rewrites Agent.md and changelog atomically | unit + CLI | `go test ./cmd/aura/ -run Profile` | `cmd/aura/profile_test.go`, `internal/profile/store_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | Profile is injected at `messages[1]`, never `messages[0]` | unit + cache audit | `go test ./internal/runner/ ./cmd/aura/ -run 'Profile|CacheAudit'` | `internal/runner/profile_context_test.go`, `cmd/aura/cache_test.go`, `scripts/cache_invariant_audit.sh` | PASS - cache gate stable |
| UX-05 | Context ladder protects profile/skills block through L2.5 trimming | unit | `go test ./internal/conversations/ -run 'AlwaysBlock|Profile'` | `internal/conversations/context_profile_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | Onboarding LoopAgent confirm/skip/edit emits terminal state deltas | unit | `go test ./internal/onboarding/ -run 'Loop|Confirm|Skip|Edit'` | `internal/onboarding/interview_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | Telegram first-run profile onboarding intercepts before normal LLM turn | unit + integration | `go test ./internal/channels/telegram/ -run 'ProfileOnboarding|Onboard|StartPayload|Command|HITL'` | `internal/channels/telegram/profile_onboarding_test.go`, `internal/channels/telegram/bot_dispatch_test.go` | PASS - covered by 2026-06-11 integrated run |
| UX-05 | Live Telegram first user confirms profile and sees normal chat resume | manual/operator | `go test -tags telegram_integration ./internal/channels/telegram/ -run ProfileOnboarding` when available | `docs/aura-quality-snapshot.md` | PASS - live operator sign-off 2026-06-14 (Davide, /onboard → Conferma → Agent.md written, onboarding_completed:true) |

## Automated Evidence - 2026-06-11

- `go test ./internal/profile/ ./internal/onboarding/ ./internal/conversations/ ./internal/runner/ ./internal/channels/telegram/ ./cmd/aura/ -count=1`
- `scripts/cache_invariant_audit.sh`
- `go test -race ./internal/profile/ ./internal/onboarding/ ./internal/channels/telegram/ ./internal/runner/ ./cmd/aura/`
- Cache invariant hashes:
  - `messages[0]`: `5c72f20c50c6ea5890ba06c4c21f15fdd06e1d09f95ec085296d83e0dd372517`
  - `messages[1]`: `da26df0f36d67df75de9ddaf3ae782ff3d5ad4a3627bdffda65d236cf63f2378`
  - `skill manifest`: `461e720f5341d73b54c6756036466487809326ac50ac674943818e33efa0d4cf`

## Sampling Rate

- After every task commit: package-level tests plus `go vet`/build or hooks.
- After every wave: quick run command.
- Before Phase 14 closure: race command, full suite, `scripts/cache_invariant_audit.sh`, live Telegram profile onboarding sign-off.

## Phase Closure Criteria

- [x] Profile store unit/race tests pass.
- [x] CLI `profile show` and `profile add-fact` tests pass.
- [x] Cache audit proves `messages[0]` constant and profile at `messages[1]`.
- [x] Telegram profile onboarding confirm/skip/edit tests pass.
- [x] Live Telegram operator sign-off recorded.
- [x] ROADMAP/REQUIREMENTS/docs updated after live sign-off.

## Live Sign-off - 2026-06-14

**Operator:** Davide (identity `local`, @DavMar1983_Bot)
**Action:** Ran `/onboard` in Telegram, completed the 5-step interview, tapped **Conferma**. Bot replied "Profilo salvato. Riprendo la chat normale."

**Ground-truth evidence (disk, 2026-06-14 12:40):**
- `~/.aura/agents/00000000-0000-0000-0000-000000000001/Agent.md` written with populated sections: Identity (Name: Davide, Role, Location), Expertise & Tools (Domains, Stack), People, Style (Tone: friendly, Response length: normal, Voice mode: true).
- `metadata.json` → `"onboarding_completed": true`
- `preferences.json` → `tone_preference: friendly, response_length: normal, voice_mode: true, location`

**Onboarding enrichment (shipped this session, beyond original skeleton):**
- 5-step interview flow: Identity / Work / Projects / Social / Style
- LLM-based answer extraction into a standardized 8-section Agent.md (Identity / Expertise & Tools / Projects & Goals / Interests / People / Style / Vetoes / Custom Instructions)
- Misspell-correction + normalization in extraction; Modifica (edit) flow re-uses the extractor
- Design spec: `docs/superpowers/specs/2026-06-14-personal-onboarding-agentmd-design.md`
- Plan: `docs/superpowers/plans/2026-06-14-personal-onboarding-agentmd.md`

**Coverage:** `internal/profile` 89.5%, `internal/onboarding` 90.4%; full untagged suite green; golangci-lint 0 issues.
