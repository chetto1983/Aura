---
spike: 038
name: profile-store-atomic-contract
type: standard
validates: Disk-backed Agent.md/preferences/metadata/changelog profile writes are path-safe, atomic, audited, and readable.
verdict: VALIDATED
related:
  - prd.md
  - .planning/ROADMAP.md
tags:
  - phase-14
  - agent-md
  - profile-store
  - atomic-write
  - windows
---

# Profile Store Atomic Contract

## What Validates

Given a profile root shaped like `~/.aura/agents/<identity>/`, when onboarding and CLI updates write profile state, then:

- `Agent.md`, `preferences.json`, `metadata.json`, and `changelog.md` are written together in the same identity directory.
- Updates replace existing files through a same-directory temp file.
- Unsafe identity names cannot escape the profile root.
- Reads reconstruct the profile and preferences.
- Changelog entries preserve the audit trail across updates.

## Research

This spike uses Phase 14's PRD file layout and borrows the atomic-write posture observed in `D:/tmp/nanobot/nanobot/agent/memory.py`: write temp file, fsync file, replace, then attempt directory sync. It adds a Windows-specific replace implementation because POSIX `rename(2)` semantics do not map cleanly to Windows overwrite behavior.

## How to Run

```powershell
go run ./.planning/spikes/038-profile-store-atomic-contract
```

## What to Expect

The harness writes an initial `local` profile under `%TEMP%/aura-spike-038-profile-store`, updates it, reads it back, checks that no temp files remain, and rejects traversal identities such as `../evil` and `a\b`.

## Observability

Key output from the run on 2026-06-08:

```text
[CHECK] initial Agent.md/preferences/metadata/changelog write OK
[CHECK] replacement update preserved files and left no temp files
[CHECK] identity path traversal rejected
[INFO] scratch root: C:\Users\Davide\AppData\Local\Temp\aura-spike-038-profile-store
[SUMMARY] VALIDATED phase14 profile store atomic contract
```

Windows finding:

```text
[INFO] directory sync skipped: sync ... Access is denied.
```

File sync and replacement were validated. Directory fsync should be best-effort on Windows, while POSIX should keep the stronger directory sync path.

## Investigation Trail

The harness implements `replaceFile` with platform split files:

- Windows: `MoveFileEx(..., MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)`.
- Non-Windows: `os.Rename` after writing a same-directory temp file.

This is the minimum production obligation if Phase 14 promises atomic profile rewrites on the operator's Windows machine.

## Results

VERDICT: VALIDATED.

Phase 14 should create a real `internal/profile` or equivalent package using this contract. Do not rely on bare `os.Rename` overwriting an existing destination on Windows.
