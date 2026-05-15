# Continue Here

Fresh Aura sessions must start here.

Current state as of 2026-05-15:

- The old deep-refactor phase folders were intentionally deleted because they
  had become noisy. Clean planning scaffolds were recreated under
  `D:/Aura/.planning/deep-refactor/` from `D:/Aura/prd.md`.
- Current git HEAD observed:
  `20d36196 chore(telegram): delete sandbox_integration_test.go placeholder tests`.
- `internal/cron` now uses `package cron`; P1-D1 is closed.
- The active route is `D:/Aura/prd.md` plus
  `D:/Aura/.planning/aura-deep-refactor-decisions.json`.
- Old `.planning/` wave files are evidence only, not executable queues
  (ADR-036).
- The active Phase 1 parent planning container is
  `D:/Aura/.planning/deep-refactor/Phase01/`.
- Phase01A local implementation is in place after verifier repair:
  migration v6 plus `internal/storage/runs`, and
  `internal/chat.Hub` durable lifecycle/tool/usage/final-output metadata
  persistence with default payload redaction.
- Verification passed:
  `go test ./internal/db/migrations ./internal/chat`,
  `go test ./internal/db/migrations ./internal/storage/runs ./internal/chat`,
  `go build ./...`, `go vet ./...`, and `go test ./...`.
- A fresh Codex verifier pass was run and repaired one missing Phase01A PRD
  path. A separate subagent verifier was not spawned in this turn.
- Phase01B1 is closed locally: migration v7 adds identity/capability tables
  plus non-breaking `runs.actor_id`, and new `internal/identity` provides a
  default-deny authorizer with disabled-principal denial, delegated/parented
  actor direct-grant rules, channel-account/principal mismatch rejection, and
  grant-subject validation.
- Phase01B1 verification passed:
  `go test ./internal/db/migrations ./internal/identity`,
  targeted migration/identity runs, `go build ./...`, `go vet ./...`, and
  `go test -count=1 ./...`.
- Phase01B1 subagent closure passed: goal verifier PASS 10/10 for B1 only;
  code-risk recheck PASS 9.5/10 with no B1 blockers.
- Phase01B2 is implemented locally: migration v8 backfills persisted
  `allowed_users` rows into deterministic Telegram principals, channel
  accounts, session actors, and owner/user grants; `internal/api/auth`
  bootstrap/approval creates the same identity records while preserving
  `allowed_users`.
- Phase01B2 verification passed:
  `go test ./internal/db/migrations ./internal/api/auth ./internal/api ./internal/telegram ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/agent/tools/swarm ./internal/cron`,
  `go test ./internal/db/migrations ./internal/identity ./internal/api/auth`,
  `go build ./...`, `go vet ./...`, and `go test ./...`.
- Do not mark the Phase01B parent complete; B3-B7 remain planned integration
  slices.
- The Phase01A/Phase01B1 implementation work is present in commit
  `d5747eb2 feat(deep-refactor): Phase01 - run/event foundation + identity
  authority`; the latest observed HEAD adds Ralph queue archival/opening.

Required first reads:

1. `D:/Aura/.planning/HANDOFF.json`
2. `D:/Aura/.planning/deep-refactor/.continue-here.md`
3. `D:/Aura/AGENTS.md`
4. `D:/Aura/prd.md`
5. `D:/Aura/.planning/deep-refactor/INDEX.md`
6. `D:/Aura/.planning/deep-refactor/Phase01/subphase-summary.md`
7. `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01B_Identity_Capability_Grants/`
8. `D:/Aura/internal/db/migrations/migrations.go`
9. `D:/Aura/internal/db/migrations/migrations_test.go`
10. `D:/Aura/internal/api/auth/store.go`
11. `D:/Aura/internal/identity/store.go`

Do not rely on chat history. Reconstruct the state from the files above before
planning or editing. For Phase01B, start Phase01B3 dashboard bearer actor
context as a separate `$aura-implementation-loop` slice. Do not bundle tool
capability checks, cron delegation, swarm delegation, or denial-event wiring
into Phase01B3.
