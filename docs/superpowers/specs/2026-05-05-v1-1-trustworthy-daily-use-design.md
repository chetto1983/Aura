# v1.1 Trustworthy Daily Use Design

Date: 2026-05-05
Status: approved for planning
Source of truth: `.planning/codebase/CONCERNS.md`

## Purpose

v1.1 makes Aura feel reliable during everyday use after the v1.0 production
readiness gate. It is a hardening milestone, not a feature milestone.

The milestone removes avoidable process-kill risks, makes quiet runtime
failures observable, and documents the highest-risk dependency/platform
watchpoints. Aura should fail loudly enough for an operator to diagnose the
problem, while preserving happy-path behavior for Telegram, the dashboard,
scheduler, and sandbox workflows.

## Milestone Boundary

v1.1 is in scope when the work improves daily operational trust without adding
new product surface area.

In scope:

- replace or contain the `MustResolveProfiles` bare panic path;
- log or return ignored production-path errors in shutdown, tray browser-open,
  Telegram placeholder cleanup, and auth token audit updates;
- validate dashboard URLs before shell handoff from the tray;
- add focused tests where the relevant failure path can be faked cleanly;
- document telebot v4 beta monitoring and smoke expectations;
- run a focused v1.1 release gate.

Out of scope:

- new user-facing features;
- broad package restructuring or arbitrary large-file splitting;
- package-wide coverage targets;
- memory quality upgrades and wiki proposal intelligence;
- settings at-rest encryption, unless it is explicitly promoted into a
  security-focused milestone.

## Requirements

### PANIC-01: Toolset Profile Panic Removal

Invalid or stale toolset profile names must not cause an unstructured process
panic in production paths.

Acceptance:

- callers that resolve toolset profiles can receive and handle an error;
- invalid profile behavior is covered by focused tests;
- any intentionally fatal startup path logs the invalid profile before exiting.

### OBS-01: Shutdown Close Observability

Shutdown close failures for long-lived services are logged with enough context
to diagnose DB/client close problems.

Acceptance:

- Telegram bot shutdown logs close errors for archiver, scheduler, auth, and
  Telegram client shutdown paths where those failures can occur;
- successful shutdown behavior remains unchanged.

### OBS-02: Tray Browser-Open Observability

Tray dashboard-open failures are visible to the operator, and invalid dashboard
URLs are rejected before shell handoff.

Acceptance:

- the Windows tray browser-open path validates URL scheme and host expectations;
- browser-open start failures are logged at warning or error level;
- non-Windows tray behavior stays harmless and explicit.

### OBS-03: Telegram Cleanup Observability

Cosmetic Telegram cleanup failures, such as placeholder deletion during
streaming, are observable at low severity.

Acceptance:

- failed placeholder deletion logs context without failing the user-facing
  conversation;
- duplicate-send and streaming behavior from v1.0 tests stays intact.

### AUDIT-01: Token Audit Update Observability

Auth token `last_used` write failures are observable without denying an
otherwise valid dashboard request.

Acceptance:

- token lookup still authenticates valid, unexpired tokens when the audit write
  fails;
- the failed audit write is logged with enough context to diagnose DB lock or
  schema issues.

### DEP-01: Telebot Beta Monitoring

Aura tracks the risk of `gopkg.in/telebot.v4` beta usage intentionally.

Acceptance:

- dependency notes document the pinned version, upgrade watchpoint, smoke
  checklist, and rollback expectation;
- no library replacement is attempted in this milestone.

### REL-02: Focused v1.1 Release Gate

The milestone closes with verification proportional to the changes.

Acceptance:

- focused package tests pass for changed areas;
- `go test ./...` and `go build ./...` pass before completion;
- Windows/manual smoke is run only if tray/browser behavior changes in a way
  that cannot be covered hermetically.

## Architecture

v1.1 keeps the existing Go package layout. It favors narrow interfaces and
small injection points only where they make error paths testable. It does not
introduce an ORM, a new logging system, or a new process supervisor.

Toolset profile resolution should prefer explicit errors over panic-style helper
APIs. If a `Must*` helper remains for internal startup invariants, it must not
be used from paths that can be influenced by configuration, persisted jobs,
skills, MCP input, or user-triggered tool execution.

Runtime observability should use the existing structured logging conventions.
Error logs should identify the subsystem and operation, but must not include
raw tokens, API keys, tool arguments, base64 payloads, or full OCR text.

## Data Flow

Toolset resolution flow:

1. Caller provides profile names from config, persisted jobs, or code.
2. Resolver validates names against the known registry.
3. Resolver returns either resolved profiles or a contextual error.
4. Caller chooses the appropriate failure mode: reject user/config input,
   skip an unsafe scheduled job, or log-and-exit for an impossible startup
   invariant.

Runtime cleanup flow:

1. Normal operation proceeds as before.
2. Cleanup or audit side effects may fail independently.
3. Side-effect failures are logged with context.
4. Non-critical failures do not convert a successful user interaction into a
   failed interaction.

Tray open flow:

1. The tray receives the configured dashboard URL.
2. The URL is parsed and validated before shell handoff.
3. Unsupported URLs are logged and rejected.
4. Shell launch errors are logged for operator diagnosis.

## Phases

### 1. Panic Removal Gate

Replace the `MustResolveProfiles` production panic path with explicit error
handling. Update callers to preserve their current behavior while avoiding
unstructured process crashes.

Success means invalid profile names cannot crash Aura through production paths,
and focused tests prove the failure behavior.

### 2. Production Error Observability

Surface ignored errors in real runtime paths: shutdown closes, tray browser
open, placeholder deletion, and token `last_used` audit updates.

Success means each known swallowed error has either a log path or a deliberate
comment explaining why it is unobservable and safe.

### 3. Platform And Dependency Hygiene

Tighten tray/headless behavior and document telebot beta monitoring. This phase
is deliberately documentation-light and code-light unless the tray changes need
small tests or URL validation helpers.

Success means future telebot upgrades and tray regressions have explicit smoke
expectations.

### 4. Release Gate Lite

Run targeted tests for changed packages, followed by broad Go verification and
any needed Windows tray smoke.

Success means v1.1 can be tagged without repeating the full v1.0 release
marathon.

## Testing Strategy

Use focused tests instead of broad coverage targets.

- Toolset tests cover invalid profile names and non-panic behavior.
- Telegram tests cover preserved streaming behavior if placeholder deletion
  logging touches the streaming path.
- Auth tests cover valid token lookup when `last_used` update fails, if the DB
  failure can be induced without brittle timing.
- Tray tests cover URL validation in pure functions where possible; manual
  Windows smoke covers shell handoff if needed.

## Release Gate

Minimum verification:

- `go test ./internal/toolsets ./internal/telegram ./internal/auth ./internal/tray -count=1`
- `go test ./...`
- `go build ./...`

Manual verification:

- Windows tray "Open Dashboard" smoke only if the browser-open path changes
  beyond pure validation/logging.
- Telegram smoke only if conversation cleanup changes alter message delivery.

## Out Of Scope

v1.1 does not add new dashboard panels, new Telegram commands, new memory
promotion flows, broad file splits, dependency swaps, installers, auto-update,
or settings encryption. Those are valid future milestones, but they would blur
the trust-focused boundary of this one.

## Open Decisions Resolved

- Milestone direction: choose Hardening Polish with a daily-trust framing.
- Memory quality: defer to a later milestone after the remaining sharp edges
  are quiet.
- Settings at-rest encryption: defer unless promoted into a security-focused
  milestone.
- Broad refactors: defer unless a small extraction is required to make a v1.1
  failure path testable.
