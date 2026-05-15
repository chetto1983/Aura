# Phase01 Subphase Summary

Status: Phase01A locally implemented, verifier-repaired, and fully Go-tested.
Phase01B1 is closed with local gates and subagent verification. Phase01B2
allowlist backfill is implemented and Go-verified locally. Phase01B3 and
Phase01B4 are container-verified. Phase01B parent remains open for B5-B7
integration slices.

| Subphase | Canonical Folder | Planning Status | Verifier Status | Next Action |
| --- | --- | --- | --- | --- |
| Phase 1 - Stabilize the Map | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01_Stabilize_Map` | self-audited scaffold | not run | Verify package-map plan before more renames. |
| Phase 1A - Persist the Run/Event Foundation | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01A_Run_Event_Foundation` | storage foundation plus `chat.Hub` lifecycle/tool/usage persistence implemented and fully Go-tested locally | Codex verifier repair passed; subagent verifier not run | Optional separate verifier, otherwise proceed to Phase01B planning promotion. |
| Phase 1B - Establish Identity and Capability Grants | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01B_Identity_Capability_Grants` | Phase01B1 migration v7 plus `internal/identity` closed; Phase01B2 migration v8/auth backfill closed locally; Phase01B3 dashboard actor context container-verified; Phase01B4 tool capability checks container-verified; B5-B7 planned | goal verifier PASS 10/10 for B1; code-risk recheck PASS 9.5/10 with no B1 blockers; Phase01B2 full Go gates passed locally; Phase01B3/B4 repo and container gates passed | Start Phase01B5 Telegram identity grant parity as a separate slice. |
| Phase 1C - Add the Question Gate | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01C_Question_Gate` | self-audited scaffold | not run | Keep queued after Phase01A/B unless question gate blocks work. |

## First Bounded Implementation Candidate

Phase01A local implementation is complete after verifier repair:

- canonical store: SQLite run/event tables through `internal/storage/runs`
- code areas changed: `internal/db/migrations`, `internal/storage/runs`,
  `internal/chat`
- verification: narrow tests, build, vet, and full `go test ./...` all green
- non-goals: no Telegram rendering change, no web API shape change, no cron or
  swarm behavior migration
- next bounded action: continue to Phase01B5 Telegram identity grant parity as
  a separate slice.

## Phase01B First Bounded Implementation Candidate

Phase01B is planned as a sequence instead of a single broad rewrite. Phase01B1
through Phase01B4 are closed locally, with Phase01B3-B4 container verification:

- canonical store: SQLite identity tables in the main Aura database
- first code area: `internal/db/migrations` plus new `internal/identity`
- first behavior: default-deny `Authorize` with active/revoked/expired grant
  tests, disabled-principal deny, delegated/parented actor direct-grant rules,
  channel-account/principal mismatch rejection, grant-subject validation, and
  durable `authz_decisions`
- second behavior: deterministic Telegram allowlist backfill from persisted
  `allowed_users` into principals, channel accounts, session actors, and
  owner/user grants while preserving `allowed_users`
- third behavior: dashboard bearer requests carry deterministic actor context,
  `/chat` rejects authenticated body `user_id` overrides, and `api.chat`
  authorization gates chat execution
- fourth behavior: registry tool execution authorizes `tool.execute` or a
  tool-specific capability before `Tool.Execute`, with disposable SQLite tests
  proving visible-tool fail-closed behavior
- deferred rewiring: Telegram env-only grant parity, cron delegated actors,
  swarm delegated actors, and denial run/audit events
- verification: migration/identity targeted tests, targeted build/vet,
  `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, and subagent
  closure verification passed for B1; `go build ./...`, `go vet ./...`, and
  `go test ./...` passed again for B2, B3, and B4.
