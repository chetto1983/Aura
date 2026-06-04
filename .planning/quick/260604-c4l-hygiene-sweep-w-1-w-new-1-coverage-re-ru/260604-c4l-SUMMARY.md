---
quick_id: 260604-c4l
slug: hygiene-sweep-w-1-w-new-1-coverage-re-ru
description: hygiene sweep (W-1 + W-NEW-1 + coverage re-run)
status: complete
date: 2026-06-04
commit: 649b0520
---

# Quick Task 260604-c4l Summary

## Result

Completed the hygiene sweep for W-1, W-NEW-1, and the full coverage re-run.

## Changes

- Removed orphaned `scripts/sandbox_escape_bench.sh`; it targeted the deleted bespoke sandbox sidecar and nonexistent sandbox workflow.
- Repointed the active quality snapshot from bespoke sandbox paths to the current sandbox-agent evidence surface.
- Fixed `internal/web/main_test.go` so the comment mirrors the existing `internal/agent/tools` split `goleak` harness.
- Updated the milestone audit to mark W-1/W-NEW-1 closed and record the coverage aggregate.
- Refreshed `cover_gate.out.testlog` with the successful `make coverage` package output.

## Verification

- `make coverage` passed: owned surface `87.4% >= 85%`.
- `git diff --check` passed.
- `rg` confirmed the targeted stale strings only remain in historical audit/quick-task context.
- Pre-commit hook on commit `649b0520` passed `gofmt`, `go vet`, and file-size checks.

## Commit

Deliverable commit: `649b0520` (`chore(hygiene): close sandbox audit sweep`).
