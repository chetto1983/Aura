---
status: testing
phase: 32-quality-cleanup-dead-code-shared-helpers
source: [32-VERIFICATION.md]
started: "2026-06-30T12:49:15Z"
updated: "2026-06-30T12:49:15Z"
---

## Current Test

number: 1
name: focusTrap keyboard accessibility in the live cockpit
expected: |
  In the cockpit MCP governance view, open a RemoveDialog (McpLifecycleCluster). Pressing Tab
  cycles focus through ALL focusable elements in the dialog (not only <button>s), and Shift+Tab
  cycles backward, wrapping at the boundaries — i.e. focus never escapes the dialog. This exercises
  the canonical web/src/a11y/focusTrap.ts (trapTabKey/focusFirstDescendant) that replaced the inline
  button-only copy.
awaiting: user response

## Tests

### 1. focusTrap keyboard accessibility (McpLifecycleCluster RemoveDialog)
expected: Tab / Shift+Tab cycle through every focusable element in the RemoveDialog and wrap at the edges; focus stays trapped inside the dialog. The canonical trapTabKey (all focusables) is in effect, fixing the prior button-only query.
result: [pending]

### 2. Skeleton loading visuals (3 migrated consumers)
expected: The loading skeletons in ConversationSidebar, SearchPanel, and the Governance view render correctly using the rich SkeletonBlock CSS-wave system (no broken layout, sizing, or missing animation) after migrating off the retired shadcn `animate-pulse` ui/skeleton. Visually consistent with the rest of the cockpit.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
