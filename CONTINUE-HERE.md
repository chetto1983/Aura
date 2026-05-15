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
- Phase01B3 is implemented locally: dashboard bearer requests now carry the
  deterministic Telegram session actor ID, `/chat` rejects authenticated body
  `user_id` impersonation, and `api.chat` authorization is enforced before
  chat execution.
- Phase01B3 verification passed:
  `go test ./internal/api ./internal/api/auth`, `go test ./internal/...`,
  `go build ./...`, `go vet ./...`, and `go test ./...`.
- Phase01B3 container E2E verification passed:
  `docker compose build aura`, `docker compose up -d --no-deps aura`,
  `aura-aura-1` health `healthy`, `/health=200`, unauthenticated
  `/api/health=401`, bearer `/api/health=200`, unauthenticated `/api/chat=401`,
  bearer mismatched `user_id` `/api/chat=403`, bearer `/api/chat` reply
  `AURA_E2E_PHASE01B3_OK`, and live SQLite `authz_decisions` recorded
  `allow|api.chat|api|chat|active_grant`.
- Phase01B4 is implemented locally and container-verified: registry tool
  execution now authorizes `tool.execute` or tool-specific capabilities before
  `Tool.Execute`, dashboard chat carries the identity authorizer into the agent
  path, and local plus containerized registry tests prove visible-tool
  fail-closed behavior with disposable SQLite authz evidence.
- Phase01B4 verification passed:
  baseline Phase01B packages, `go test ./internal/identity`,
  `go test ./internal/agent/tools/registry`,
  `go test ./internal/api ./internal/api/auth`,
  `go test ./internal/agent ./internal/telegram ./internal/channels/telegram`,
  `go test ./internal/...`, `go build ./...`, `go vet ./...`,
  `go test ./...`, `docker compose build aura`,
  `docker compose up -d --no-deps aura`, live health/API/chat probes with
  `AURA_E2E_PHASE01B4_OK`, and containerized registry fail-closed tests.
- Phase01B5 is implemented and verified: Telegram `/start` and `/login` now
  ensure deterministic identity/grants before dashboard token issuance;
  configured allowlist users remain config-only and are not copied into
  `allowed_users`; persisted bootstrap/approved users repair identity from
  stored source before tokens are minted.
- Phase01B5 verification passed:
  `go test ./internal/api/auth -run "TestEnsureTelegramAllowlistedIdentity|TestBackfillAllowedUserIdentitiesMigratesExistingRows|TestBootstrapUser_ClaimsEmptyAllowlist" -count=1`,
  `go test ./internal/telegram -run "TestOnLogin|TestApproveAccessCreatesIdentityBeforeSendingToken|TestIsAllowlisted|TestCollectOwnerIDs" -count=1`,
  `go test ./internal/identity ./internal/api/auth ./internal/telegram -count=1`,
  the Phase01B baseline package gate, `go test ./internal/... -count=1`,
  `go build ./...`, `go vet ./...`, and `go test ./... -count=1`.
- Phase01B5 container update passed:
  `docker compose build aura`, `docker compose up -d --no-deps aura`,
  `aura-aura-1` health `healthy`, `/health=200`,
  unauthenticated `/api/health=401`, and unauthenticated `/api/chat=401`.
  Live Telegram fixture and bearer chat marker were not available in this shell
  because `AURA_E2E_TOKEN` is not set.
- Registered `D:/tmp/cli-printing-press` as an Aura reference map in
  `D:/Aura/docs/cli-printing-press-study.md`.
- Do not mark the Phase01B parent complete; B6-B7 remain planned integration
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
planning or editing. For Phase01B, start the next bounded implementation
slice, likely Phase01B6 cron/swarm delegated actor creation. Do not bundle
denial-event wiring into that next slice unless the Phase01B plan is explicitly
narrowed first.
