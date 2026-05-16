# Phase01 Subphase Summary

Status: Phase01A locally implemented, verifier-repaired, and fully Go-tested.
Phase01B1 is closed with local gates and subagent verification. Phase01B2
allowlist backfill is implemented and Go-verified locally. Phase01B3 and
Phase01B4 are container-verified. Phase01B5, Phase01B6, Phase01B7, and the
Phase01B fail-closed repair are repo-verified and container-updated. The
Phase01B parent closure verifier passed local/live auth-boundary gates,
repaired Chat Hub actor persistence, fixed the DB-backed provider secret path,
removed runtime `.env`, and passed the live LLM marker probe. Phase01C is now
closed E2E after a live falsification repair: durable `chat_questions`,
question request/answer events, ask_user exclusive pause, stable web pipe
thread ids, restart-safe Telegram pending-question resume, repo-wide Go gates,
Telegram package/fixture tests, compose test-profile verification, and
production container DB/health probes all passed.

| Subphase | Canonical Folder | Planning Status | Verifier Status | Next Action |
| --- | --- | --- | --- | --- |
| Phase 1 - Stabilize the Map | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01_Stabilize_Map` | self-audited scaffold | not run | Verify package-map plan before more renames. |
| Phase 1A - Persist the Run/Event Foundation | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01A_Run_Event_Foundation` | storage foundation plus `chat.Hub` lifecycle/tool/usage persistence implemented and fully Go-tested locally | Codex verifier repair passed; subagent verifier not run | Optional separate verifier, otherwise proceed to Phase01B planning promotion. |
| Phase 1B - Establish Identity and Capability Grants | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01B_Identity_Capability_Grants` | Phase01B1 migration v7 plus `internal/identity` closed; Phase01B2 migration v8/auth backfill closed locally; Phase01B3 dashboard actor context container-verified; Phase01B4 tool capability checks container-verified; Phase01B5 Telegram identity parity locally verified and container-updated; Phase01B6 cron/swarm delegated actors locally verified and container-updated; Phase01B7 authorization denial run/audit events repo-verified and container-updated; fail-open authority paths removed and verified; parent closure verifier repaired Chat Hub actor persistence and the DB-backed provider secret path | goal verifier PASS 10/10 for B1; code-risk recheck PASS 9.5/10 with no B1 blockers; Phase01B2 full Go gates passed locally; Phase01B3/B4 repo and container gates passed; Phase01B5 auth/Telegram/full Go gates passed; Phase01B6 delegated actor SQL benchmarks plus full Go gates passed; Phase01B7 denial event SQL benchmarks plus full Go gates passed; fail-closed repair full repo gate passed; parent closure local/code, live auth-boundary, DB/secrets, and live chat-marker gates passed | Closed for Phase01B. Select the next bounded phase slice before editing more code. |
| Phase 1C - Add the Question Gate | `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01C_Question_Gate` | durable question gate closed E2E after falsification: `chat_questions`, `question_requested` / `question_answered`, exclusive ask_user pause, stable web pipe thread ids, restart-safe Telegram answer routing, and explicit late/duplicate/wrong-channel states | local repo gates, Telegram package/fixture tests, compose test-profile package gate, and production web-pipe DB probe passed | Closed for Phase01C. Select the next bounded phase slice before editing more code. |

## First Bounded Implementation Candidate

Phase01A local implementation is complete after verifier repair:

- canonical store: SQLite run/event tables through `internal/storage/runs`
- code areas changed: `internal/db/migrations`, `internal/storage/runs`,
  `internal/chat`
- verification: narrow tests, build, vet, and full `go test ./...` all green
- non-goals: no Telegram rendering change, no web API shape change, no cron or
  swarm behavior migration
- next bounded action: select the next phase slice from the canonical deep
  refactor index; Phase01B no longer owns the active blocker.

## Phase01B First Bounded Implementation Candidate

Phase01B is planned as a sequence instead of a single broad rewrite. Phase01B1
through Phase01B7 are closed locally, with Phase01B3-B7 container verification:

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
- fifth behavior: Telegram login/bootstrap/approval prepares deterministic
  identity records and grants before token issuance while preserving allowlist
  parity
- sixth behavior: cron manual/Hub agent jobs and swarm workers create delegated
  child actors with direct grants bounded by the parent actor authority
- seventh behavior: authorization denials are correlated into metadata-only
  `run_events` and `audit_events`
- fail-closed repair: registry, cron manual jobs, cron Hub jobs, and swarm
  delegated assignments no longer run on missing identity authority
- parent closure repair: Hub-backed runs/events now persist the actor ID from
  context into `runs.actor_id` and lifecycle `run_events.actor_id`
- verification: migration/identity targeted tests, targeted build/vet,
  `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, and subagent
  closure verification passed for B1; `go build ./...`, `go vet ./...`, and
  `go test ./...` passed again for B2-B7 and the fail-closed repair, with SQL
  benchmarks covering identity, cron, cron Hub, swarm, denial run/audit events,
  and composition wiring.
- final closure repair: the DB-backed provider secret path was repaired, runtime
  `.env` was removed, and the 2026-05-15 parent closure live bearer chat marker
  passed with exact `PHASE01B_CLOSE_OK`.
