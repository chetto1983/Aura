---
phase: 14
slug: onboarding-agent-md
status: planned
nyquist_compliant: pending_execution
created: 2026-06-08
validated_by: gsd-plan-phase 14
---

# Phase 14 - Validation Strategy

Per-phase validation contract for Phase 14 execution. This is planned coverage; execution must update statuses and evidence paths as tasks land.

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing`, package-level goleak where goroutines are spawned, build tags for live Telegram checks |
| Quick run command | `go test ./internal/profile/ ./internal/onboarding/ ./internal/conversations/ ./internal/runner/ ./cmd/aura/ -count=1` |
| Race command | `go test -race ./internal/profile/ ./internal/onboarding/ ./internal/channels/telegram/ ./internal/runner/ ./cmd/aura/` |
| Full suite command | `go test ./... -count=1` |
| Live tag tier | `telegram_integration` or manual operator evidence for first-run profile onboarding |

## Per-Task Verification Map

| Req | Behavior | Test Type | Automated Command | Evidence Files | Status |
|-----|----------|-----------|-------------------|----------------|--------|
| UX-05 | Profile files write/read atomically under `~/.aura/agents/<id>/` | unit + Windows seam | `go test ./internal/profile/ -run 'Store|Atomic|Path|Profile'` | `internal/profile/store_test.go`, `internal/profile/atomic_*_test.go` | PLANNED |
| UX-05 | `aura profile show --identity local` renders Agent.md sections as a tree | unit + CLI | `go test ./cmd/aura/ -run Profile` | `cmd/aura/profile_test.go` | PLANNED |
| UX-05 | `aura profile add-fact` rewrites Agent.md and changelog atomically | unit + CLI | `go test ./cmd/aura/ -run Profile` | `cmd/aura/profile_test.go`, `internal/profile/store_test.go` | PLANNED |
| UX-05 | Profile is injected at `messages[1]`, never `messages[0]` | unit + cache audit | `go test ./internal/runner/ ./cmd/aura/ -run 'Profile|CacheAudit'` | `internal/runner/profile_context_test.go`, `cmd/aura/cache_test.go` | PLANNED |
| UX-05 | Context ladder protects profile/skills block through L2.5 trimming | unit | `go test ./internal/conversations/ -run 'AlwaysBlock|Profile'` | `internal/conversations/context_profile_test.go` | PLANNED |
| UX-05 | Onboarding LoopAgent confirm/skip/edit emits terminal state deltas | unit | `go test ./internal/onboarding/ -run 'Loop|Confirm|Skip|Edit'` | `internal/onboarding/interview_test.go` | PLANNED |
| UX-05 | Telegram first-run profile onboarding intercepts before normal LLM turn | unit + integration | `go test ./internal/channels/telegram/ -run 'ProfileOnboarding|Onboard'` | `internal/channels/telegram/profile_onboarding_test.go` | PLANNED |
| UX-05 | Live Telegram first user confirms profile and sees normal chat resume | manual/operator | `go test -tags telegram_integration ./internal/channels/telegram/ -run ProfileOnboarding` when available | `docs/aura-quality-snapshot.md` | MANUAL |

## Sampling Rate

- After every task commit: package-level tests plus `go vet`/build or hooks.
- After every wave: quick run command.
- Before Phase 14 closure: race command, full suite, `scripts/cache_invariant_audit.sh`, live Telegram profile onboarding sign-off.

## Phase Closure Criteria

- [ ] Profile store unit/race tests pass.
- [ ] CLI `profile show` and `profile add-fact` tests pass.
- [ ] Cache audit proves `messages[0]` constant and profile at `messages[1]`.
- [ ] Telegram profile onboarding confirm/skip/edit tests pass.
- [ ] Live Telegram operator sign-off recorded.
- [ ] ROADMAP/REQUIREMENTS/docs updated after live sign-off.
