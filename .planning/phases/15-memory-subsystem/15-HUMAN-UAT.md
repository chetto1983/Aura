---
status: complete
phase: 15-memory-subsystem
source: [15-VERIFICATION.md]
started: 2026-06-12T09:45:00Z
updated: 2026-06-15T09:50:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. CR-01 product decision — default-on memory vs MCP profiles
expected: Decide the intended semantics of `injectDefaultOnMemory` for non-default MCP profiles (Phase 16 feature). Either (a) memory is always-on across profiles → remove the misleading "explicit entry wins" docstring claim, or (b) profile exclusion / custom URL must win → apply the CR-01 fix (gate inject on unfiltered `managed.MCPServers["memory"]` existing, not the profile-filtered policies map). See 15-REVIEW.md CR-01.
result: resolved — option (b) applied 2026-06-12 (commit 4d9b6b35): inject now gates on the unfiltered managed doc; any explicit memory entry (enabled/disabled/blocked/profile-excluded) blocks injection. Regression test TestMemoryDefaultOn_RespectsProfileExclusion green.

### 2. CI memory-integration-test job green on GitHub Actions
expected: The new `memory-integration-test` job in .github/workflows/ci.yml runs the live tier (not skip — runtimes ≥0.28s per test) and is green on the pushed branch. Local evidence is committed; remote Actions history needs human confirmation after push.
result: resolved - confirmed 2026-06-15 via GitHub Actions run 27536453185 on branch tabula-rasa, head 5f70703f9b417985ee0af818afdb7ca3c80e206d. Job "Memory MCP (memory_integration tier, live agent-memory sidecar)" completed successfully; step "Live memory_integration tier (16-tool mount + CLI + recall + dedup)" also completed successfully.

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
