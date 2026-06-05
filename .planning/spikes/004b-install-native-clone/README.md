---
spike: 004b
name: install-native-clone
type: comparison
validates: "Given the same source, when a Go harness shallow-clones + copies the skill dir + computes SHA-256, then the result is content-identical to 004a and the node/npx host dep is droppable"
verdict: VALIDATED (WINNER)
related: [004a, 003, 005, 006]
tags: [skills, install, git, phase-11]
---

# Spike 004b: install-native-clone

## What This Validates

The researcher hypothesis from `/gsd-discuss-phase 11`: "the skills CLI adds nothing beyond clone+copy+hash" — i.e. Aura's installer can be pure Go (git CLI subprocess, fixed argv, Phase-8 dockerCLI discipline) with **no node/npm host dependency**.

## Research

The CLI's hash algorithm read from its dist (`cli.mjs:719 computeSkillFolderHash`): sha256 over (relativePath-forward-slash, rawBytes) pairs **sorted by `localeCompare`**, skipping `.git`/`node_modules`. Reproduced in Go with byte-order sort.

## How to Run

```bash
# Windows footgun: the exe name contains "install" → UAC Installer-Detection demands elevation.
# Build with a neutral output name:
go build -o /d/tmp/spike-004b-harness.exe ./.planning/spikes/004b-install-native-clone && /d/tmp/spike-004b-harness.exe
# LF root-cause probe:
go build -o /d/tmp/spike-004b-lfprobe.exe ./.planning/spikes/004b-install-native-clone/lfprobe && /d/tmp/spike-004b-lfprobe.exe
```

## What to Expect

Harness: clone ~1.5s, copy ~30ms, `2/3 native tree content-identical`, a `[FINDING]` on the lockfile hash, `[SUMMARY] VALIDATED`, exit 0. lfprobe: `match = false` (exit 1 — that's the finding, not a failure).

## Investigation Trail

1. **First run blocked by Windows itself:** `go run` of a binary named `004b-install-native-clone.exe` → "requires elevation" (UAC Installer Detection keys off "install" in exe names). Fixed with `go build -o` neutral name. Planner note: never ship a binary with "install" in its filename on Windows.
2. Shallow clone (`--depth 1 --single-branch`, `GIT_TERMINAL_PROMPT=0`) = 1.46s; symlink-rejecting copy (spike 005 finding wired in) = 30ms. Total **1.49s vs npx 4.5s — 3× faster**.
3. **Tree fidelity: PERFECT.** Native tree hash == recomputed hash over 004a's CLI install (`1c22a27a…`, 54 files, 1,116,109 bytes). Bit-for-bit identical installs.
4. **Lockfile hash: NOT reproducible — three distinct hashes.** CRLF tree `1c22a27a…`, autocrlf=false LF re-clone `38d1187a…`, lockfile `cb7fba21…`. Root cause narrowed to the CLI's `localeCompare` sort (locale/ICU collation ≠ byte order for paths like `scripts/office/helpers/__init__.py`) and/or the bytes its internal clone saw pre-rewrite. Consequence: **`computedHash` is platform/locale-sensitive upstream — even the CLI's own `check` would mismatch a Windows checkout.** Chasing interop is worthless.

## Results

**VALIDATED — WINNER of the 004 comparison.**

- Aura's installer = native Go: `git clone --depth 1` (LookPath-gated fixed argv) + symlink-stripping copy + **Aura's own canonical hash** (byte-sorted (relPath, bytes) sha256 — deterministic, locale-independent) pinned in the audit row / install metadata. D-13's TOFU pinning never needed lockfile interop.
- Drops node/npm from Aura's host requirements for install (npx path stays documented as manual fallback; node still needed only if the operator uses MCP servers that are node-based).
- Clone with `-c core.autocrlf=false` in the installer for byte-stable, platform-independent content (LF everywhere = same hash on Windows dev and Linux prod).
- Symlink rejection at copy time wired and proven (0 in this repo; guard active).
