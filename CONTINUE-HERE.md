# Continue Here

Fresh Aura sessions must start here.

Current state as of 2026-05-15:

- The old deep-refactor phase folders were intentionally deleted because they
  had become noisy. Clean planning scaffolds were recreated under
  `D:/Aura/.planning/deep-refactor/` from `D:/Aura/prd.md`.
- Current git HEAD observed:
  `e2ba8fad refactor(api): move web_chat from telegram/ to api/ (US-A17)`.
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
- Phase01B1 is implemented locally: migration v7 adds identity/capability
  tables plus non-breaking `runs.actor_id`, and new `internal/identity`
  provides a default-deny authorizer.
- Phase01B1 verification passed:
  `go test ./internal/db/migrations ./internal/identity`,
  targeted migration/identity runs, `go build ./...`, `go vet ./...`, and
  `go test ./...`.

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

Do not rely on chat history. Reconstruct the state from the files above before
planning or editing. For Phase01B, either run a fresh verifier for Phase01B1 or
start Phase01B2 allowlist backfill as a separate `$aura-implementation-loop`
slice. Do not bundle Telegram/API/cron/swarm rewiring into Phase01B1.
