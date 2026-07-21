---
phase: 39-idempotency-observability-pack
fixed_at: 2026-07-21T22:12:36Z
review_path: .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
iteration: 1
findings_in_scope: 10
fixed: 10
skipped: 0
status: all_fixed
---

# Phase 39: Code Review Fix Report

**Fixed at:** 2026-07-21T22:12:36Z  
**Source review:** `.planning/phases/39-idempotency-observability-pack/39-REVIEW.md`  
**Iteration:** 1

**Summary:**

- Findings in scope: 10
- Fixed: 10
- Skipped: 0

## Fixed Issues

### CR-01: HTTP mutation metadata is never installed

**Files modified:** `internal/agui/idempotency_http.go`, `internal/agui/server.go`, `internal/agent/llm_agent.go`, `internal/agent/mcptools/bridge.go`, `web/src/lib/http-mutation-idempotency.ts`, `web/src/main.tsx`  
**Commit:** `4c9038f76`  
**Applied fix:** Installed authenticated production middleware on inventoried AG-UI mutation routes. The middleware derives a canonical fingerprint, acquires before effects, buffers and persists the original bounded HTTP envelope, and projects replay/conflict/expiry decisions without rerunning handlers. Agent and MCP mutations now derive deterministic child operations in their declared scope from trusted HTTP, CLI, scheduler, or approval parents. The browser client attaches a per-request key to same-origin mutations while preserving explicit keys and excluding external uploads; real-mux and browser tests prove the behavior.  
**Verification status:** fixed: requires human verification

### CR-02: The CLI never acquires or completes the durable registry

**Files modified:** `cmd/aura/idempotency.go`, `cmd/aura/main.go`, `cmd/aura/idempotency_test.go`  
**Commits:** `27a94d15f`, `752397c87`  
**Applied fix:** Routed every inventoried CLI mutation through a common durable parent process that acquires the operation, dispatches the registered executable adapter as a marked child, captures and replays successful output, completes only after success, and marks nonzero, oversized, or ambiguous outcomes indeterminate. The inventory now binds commands to executable adapters rather than metadata alone.  
**Verification status:** fixed: requires human verification

### CR-03: Scheduler operations cannot authorize mutating agent tools

**Files modified:** `internal/cron/handlers/agent_job_test.go`, `internal/agent/llm_agent.go`, `internal/agent/mcptools/bridge.go`  
**Commit:** `21e7b69e5`  
**Applied fix:** Preserved the scheduler run as the trusted parent and derived deterministic child operations for each mutating tool using immutable run identity plus canonical tool intent and the tool's declared scope. A strict-profile AgentJobHandler integration test proves a harmless mutation executes once and replays on retry.  
**Verification status:** fixed: requires human verification

### CR-04: Export-delete can delete data outside an ephemeral archive

**Files modified:** `internal/agui/owner_export.go`, `internal/agui/retention_api.go`, `internal/agui/server.go`, `internal/agui/conversations_api.go`, `internal/runner/runner.go`, `internal/db/queries/conversations.sql`, `internal/db/sqlc/conversations.sql.go`, `internal/db/sqlc/querier.go`  
**Commit:** `387bb4d08`  
**Applied fix:** Held the conversation exclusion lock across snapshot acquisition, durable owner-scoped publication, reread verification, and version-conditional deletion. Production now publishes to object storage with a 30-day expiry and returns an export ID plus authenticated resumable download URL; concurrent-append, client-disconnect, and owner-isolation tests cover the destructive boundary.  
**Verification status:** fixed: requires human verification

### CR-05: Retention apply recomputes the live plan before persisted recovery

**Files modified:** `internal/retention/engine.go`, `internal/retention/engine_test.go`  
**Commit:** `974710bf6`  
**Applied fix:** Apply now resolves and claims the immutable persisted operation by token before inspecting live external state, then performs per-item version revalidation. A crash-recovery test resumes the same token after artifact bytes were already removed.  
**Verification status:** fixed: requires human verification

### CR-06: Local retention does not version-check the replacement at removal

**Files modified:** `internal/retention/local.go`, `internal/retention/local_test.go`  
**Commit:** `efad0e1a7`  
**Applied fix:** Local removal performs a second Lstat version comparison at the destructive boundary and returns a typed retryable conflict when the candidate changed. The race test swaps the candidate after revalidation and proves the replacement survives.  
**Verification status:** fixed: requires human verification

### CR-07: Expired and non-200 replays become fabricated HTTP 200 responses

**Files modified:** `internal/db/migrations/0046_idempotency_replay_envelope.up.sql`, `internal/db/migrations/0046_idempotency_replay_envelope.down.sql`, `internal/db/queries/idempotency_operations.sql`, `internal/db/sqlc/idempotency_operations.sql.go`, `internal/db/sqlc/models.go`, `internal/idempotency/store.go`, `internal/agui/idempotency_http.go`, `internal/gateway/reserve.go`  
**Commit:** `1c948f7f2`  
**Applied fix:** Persisted the original bounded HTTP status and allowlisted headers alongside replay data, added an explicit result-expired marker, projects expired results as terminal 410 responses, and rejects empty or expired gateway replays. Tests cover original-status/header replay and post-GC HTTP/tool retries.  
**Verification status:** fixed: requires human verification

### CR-08: Central log redaction skips error-valued attributes

**Files modified:** `internal/obs/init.go`, `internal/redact/string.go`, `internal/obs/init_test.go`, `internal/redact/string_test.go`  
**Commit:** `b246275f7`  
**Applied fix:** Central slog processing now sanitizes errors, Stringers, resolved LogValuers, and recursively nested groups. Credential patterns include common password and secret forms, with direct error and nested-group regression coverage.  
**Verification status:** fixed: requires human verification

### WR-01: Readiness can hang on a cancellation-ignoring probe

**Files modified:** `internal/agui/readiness.go`, `internal/agui/readiness_test.go`  
**Commit:** `6e9ac91f8`  
**Applied fix:** Readiness collects results with a select over the result channel and shared deadline, returning dependency_unavailable without waiting for non-cooperative probes. Result delivery cannot block, and a never-returning probe test proves the endpoint stays bounded.  
**Verification status:** fixed: requires human verification

### WR-02: First-page compaction starves later buckets

**Files modified:** `internal/learningretention/compactor.go`, `internal/learningretention/neo4j_store.go`, `internal/learningretention/compactor_test.go`  
**Commit:** `8fa2d2e40`  
**Applied fix:** Added stable cursor paging and deterministic rotation so successive compaction passes visit every bucket. A fixture beyond BucketLimit places an over-cap bucket on a later page and verifies eventual enforcement.  
**Verification status:** fixed: requires human verification

## Validation

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- WSL race tests for the changed AG-UI, CLI, scheduler, conversation, runner, retention, and learning packages
- `npm run typecheck`
- `npm run lint`
- `npm run build`
- `npm test -- --reporter=dot` — 184 files and 1,571 tests passed
- Targeted browser mutation idempotency tests — 3 tests passed

---

_Fixed: 2026-07-21T22:12:36Z_  
_Fixer: the agent (generic-agent workaround because the gsd-code-fixer custom agent type is unavailable)_  
_Iteration: 1_
