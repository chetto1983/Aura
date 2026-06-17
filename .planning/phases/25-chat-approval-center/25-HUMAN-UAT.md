---
status: partial
phase: 25-chat-approval-center
source: [25-VERIFICATION.md]
started: "2026-06-17T17:40:29Z"
updated: "2026-06-17T17:40:29Z"
---

## Current Test

[awaiting human testing — run against a live `aura serve` + browser]

## Tests

### 1. Live SSE streaming chat (CHAT-01)
expected: Type a prompt in the cockpit composer against a real `aura serve`; the assistant reply renders incrementally token-by-token; the Stop affordance cancels the in-flight turn promptly (ctx-cancel on the server).
result: [pending]

### 2. Reasoning drawer persistence (CHAT-03)
expected: The collapsible reasoning drawer's open/closed state persists across a page reload (localStorage) — verifiable only in a real browser, not jsdom.
result: [pending]

### 3. Branch tree navigation (CHAT-05 / D-09)
expected: With migration 0017 applied on a live DB + streaming backend: edit a user turn (or regenerate an assistant turn) forks a sibling branch; the BranchPicker navigates siblings (Previous/Next/Number/Count); the re-run continues over the selected branch path; messages[0] head stays byte-identical across the switch.
result: [pending]

### 4. Cross-thread HITL approval full flow (APRV-01/02/03)
expected: Against a live runner with an active `ask_user` pause: the cross-thread badge polls and shows the pending count; the inline approval card renders in-thread; Answer/Decline resolves it and the run resumes and completes in-thread (the continue-after-resume re-drive, CR-01 fix); terminal/auto-terminated interrupts render their explicit state.
result: [pending]

### 5. Runtime footer live update (CHAT-04 / D-10/D-12)
expected: After a real turn (live OpenRouter STATE_DELTA usage), the footer shows non-zero prompt/completion/cache tokens + cost + cache-hit ratio (golden replay uses synthetic values).
result: [pending]

### 6. Playwright E2E against a live stack (CHAT-01 / APRV-02 goal-backward proof)
expected: `npx playwright test chat.spec.ts` resolves the live `aura serve` stream source (not the golden-replay fallback) and proves prompt → stream → inline approval resolve → resume → footer end-to-end. (The golden-replay CI path is already automated-green; this is the definitive live proof.)
result: [pending]

## Summary

total: 6
passed: 0
issues: 0
pending: 6
skipped: 0
blocked: 0

## Gaps
