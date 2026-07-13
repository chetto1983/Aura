---
phase: 42-llm-conversation-compaction
plan: 03
subsystem: conversations
tags: [go, compaction, structured-summary, prompt-injection, recovery, agui]
requires:
  - phase: 42-02
    provides: durable claims, immutable checkpoints, restore and pointer CAS
provides:
  - versioned structured summaries with deterministic reduction and injection validation
  - typed source-addressed unresolved-instruction and revocation ledger
  - non-authoritative encoded internal-context projection
  - common shadow-only trigger coordinator with owner-gated preview and safe-point restore APIs
affects: [42-04, 42-05, compaction-rollout, conversation-api]
tech-stack:
  added: []
  patterns: [strict JSON boundary, source-addressed authority ledger, bounded outcome vocabulary, safe-point restore]
key-files:
  created: [internal/conversations/compaction_schema.go, internal/conversations/compaction_authority.go, internal/conversations/compaction_summarize.go, internal/llm/internal_context.go, internal/runner/runner_compact.go, testdata/compaction/adversarial.jsonl]
  modified: [internal/agui/conversations_api.go, internal/agui/server.go]
key-decisions:
  - "Summary quotations are base64 data inside a fixed non-authoritative envelope; validation rejects role, delimiter, encoded, fake-summary, and authority-promotion payloads."
  - "All trigger kinds share one stable coordinator and sanitized outcome vocabulary; preview never activates and restore requires an explicit safe point."
  - "Manual APIs owner-gate the conversation before dispatch and return bounded status values without backend errors."
requirements-completed: [IC-06, IC-07, IC-11, IC-13, IC-14]
duration: 17min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 03: Safe Structured Summarization and Manual Recovery Summary

**Strict source-grounded summary validation and authority preservation with a common shadow-only preview/restore coordinator**

## Performance

- **Duration:** 17 min
- **Completed:** 2026-07-13
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Added a versioned structured-summary schema with strict JSON decoding, exact L0 manifest retention, reduction/token limits, source grounding, and deterministic poison checks.
- Added a typed unresolved-instruction ledger preserving source sequence, original authority, quoted-data encoding, active/revoked state, and explicit revocation links.
- Rendered validated summaries only through a fixed base64 non-authoritative internal-context envelope.
- Added one coordinator for manual, proactive, boundary, idle, model-safe-point, and overflow triggers, with stable operation IDs and bounded sanitized outcomes.
- Added owner-gated manual preview and restore endpoints; preview remains shadow-only and restore rejects mid-stream calls without an explicit safe point.

## Task Commits

1. **Task 1: Implement structured summarizer, authority ledger, and injection validator** - `249783594`
2. **Task 2: Wire manual coordinator, preview, restore, and safe trigger semantics** - `542d6c80b`

## Decisions Made

- System authority is parseable solely to make attempted promotion explicitly rejectable; accepted ledger entries remain user/tool data.
- Backend failures collapse to `timeout` or `unavailable`, preventing DSN or internal error disclosure.
- Semantic activation remains disabled; even a successful preview reports `activated: false`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Wire contract] Added explicit lower-camel JSON projection**
- **Found during:** Task 2 WSL race verification
- **Issue:** The first DTO encoded exported Go field names, violating the intended API schema.
- **Fix:** Added explicit JSON tags and retained omission only for optional checkpoint fields.
- **Verification:** Focused WSL race and native package suites pass.
- **Committed in:** `542d6c80b`

**Total deviations:** 1 auto-fixed bug. **Impact:** No scope change; the public response contract is deterministic.

## Issues Encountered

- Gate retry: Task 1 revive rejected undocumented exported authority/state constants; block documentation was added and all hooks passed.
- Gate retry: native Windows rejected `-race` without CGO; exact race suites ran under WSL/CGO and native non-race tests also passed.
- Gate retry: Task 2 race verification caught incorrect JSON field casing; tags were added and the full gate reran green.
- Gate retry: a pre-commit HEAD assertion rejected an incorrectly expanded expected SHA before Git ran; the authoritative full SHA was verified and used.
- Gate retry: Task 2 revive rejected undocumented trigger/status constants; documentation was added and all hooks passed.
- The repository file-size hook took approximately 101-120 seconds per commit; each commit remained single-flight and was never bypassed.

## Known Stubs

None. Semantic activation is intentionally disabled by this plan's shadow-only requirement.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: privileged_restore_endpoint | internal/agui/conversations_api.go | New owner-gated restore surface changes an active checkpoint pointer only when the request declares a safe point; capability protection must remain present at the composition-root mount. |

## Verification

- `CGO_ENABLED=1 go test -race ./internal/conversations ./internal/llm ./internal/runner ./internal/agui -run 'Compact|Summary|Authority|Restore' -count=1` — PASS under WSL.
- Focused runner/AGUI race and native non-race suites — PASS.
- Normal pre-commit gofmt, vet, lint, duplicate scan, and 600-LOC gates — PASS.

## Self-Check: PASSED

- All key created files exist.
- Task commits `249783594` and `542d6c80b` exist in history.
- Unrelated `.planning/graphs/` changes remain unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
