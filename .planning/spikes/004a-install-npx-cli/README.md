---
spike: 004a
name: install-npx-cli
type: comparison
validates: "Given owner/repo + skill name, when npx --yes skills add --skill <name> --copy -y runs into a scratch dir with sanitized env + timeout, then the skill dir lands hermetically + skills-lock.json computedHash recorded"
verdict: VALIDATED
related: [004b, 003, 006]
tags: [skills, install, npx, phase-11]
---

# Spike 004a: install-npx-cli

## What This Validates

The PRD's installer design: wrap the `skills` CLI as a node subprocess. Head-to-head partner of 004b (native Go clone).

## How to Run

```bash
mkdir -p /d/tmp/spike-004a && cd /d/tmp/spike-004a
time npx --yes skills add anthropics/skills --skill xlsx --agent claude-code --copy -y
```

## What to Expect

`Installed 1 skill → .claude\skills\xlsx (copied)` in ~4.5s; `skills-lock.json` with `computedHash` (captured here as `skills-lock.captured.json`).

## Investigation Trail

1. Non-interactive install works first try: `--yes` (npx) + `-y` (CLI) + explicit `--agent claude-code` + `--copy` → no prompts, exit 0, 4.55s wall.
2. Layout: `.claude/skills/xlsx/` = SKILL.md + LICENSE.txt + a deep `scripts/office/**` tree (Python helpers + 40 XSD schemas) — 54 files, 1.1 MiB. Real-world skills are NOT just markdown.
3. Lockfile written at CWD root: `{version:1, skills:{xlsx:{source, sourceType:github, skillPath, computedHash}}}`.
4. Files arrive CRLF on Windows (git autocrlf in the CLI's clone path) — see 004b's three-hash finding: the lockfile `computedHash` matches NEITHER the CRLF nor an LF re-clone tree (locale-collation sort + pre-rewrite bytes). **The CLI's own lockfile is not verifiable against its own Windows checkout.**
5. CLI v1.5.10 has no `--ignore-scripts` (PRD assumption obsolete — install is clone+copy, no npm lifecycle ever runs).

## Results

**VALIDATED** as a working install path — but see the comparison verdict in 004b: native clone is 3× faster, bit-identical, and drops the node/npx host dependency. npx remains the fallback documented path.
