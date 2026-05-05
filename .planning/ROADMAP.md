# Roadmap: Aura v1.1 - Trustworthy Daily Use

**Created:** 2026-05-05
**Milestone:** v1.1 Trustworthy Daily Use
**Total phases:** 4

## Milestone Goal

Make Aura safer and calmer for daily use by removing avoidable panics, surfacing quiet runtime failures, suppressing the packaged Windows console, and documenting dependency/platform watchpoints.

## Boundary

In scope:
- Toolset profile panic removal
- Shutdown, tray/browser, Telegram cleanup, and token-audit observability
- Packaged Windows GUI/tray-first binary behavior
- Telebot v4 beta monitoring docs
- Focused v1.1 release gate

Out of scope:
- New user-facing features
- Broad large-file refactors
- Memory-quality upgrades
- Settings at-rest encryption
- A separate `aura-console.exe`

## Phases

### Phase 1: Panic Removal Gate

**Addresses:** PANIC-01
**Depends on:** -
**Success criteria:**
- No production path calls `MustResolveProfiles`.
- Invalid profile names return contextual errors instead of panicking.
- Focused tests cover invalid profile behavior.

### Phase 2: Production Error Observability

**Addresses:** OBS-01, OBS-02, OBS-03, AUDIT-01
**Depends on:** Phase 1
**Success criteria:**
- Shutdown close errors are logged.
- Tray browser-open failures are logged and unsafe URLs are rejected.
- Placeholder deletion failures are logged at low severity.
- Token `last_used` audit write failures are observable without denying valid tokens.

### Phase 3: Platform And Dependency Hygiene

**Addresses:** DEP-01, UX-01
**Depends on:** Phase 2
**Success criteria:**
- Packaged Windows `aura.exe` uses the Windows GUI subsystem.
- Development and debug commands keep console output.
- Telebot v4 beta monitoring docs define upgrade and rollback checks.

### Phase 4: Release Gate Lite

**Addresses:** REL-02
**Depends on:** Phases 1-3
**Success criteria:**
- Focused package tests pass.
- `go test ./...` and `go build ./...` pass.
- Snapshot Windows artifact passes GUI-subsystem inspection.
- Manual Windows smoke runs only where hermetic checks cannot prove behavior.
