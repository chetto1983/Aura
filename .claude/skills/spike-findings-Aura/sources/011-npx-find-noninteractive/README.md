---
spike: 011
name: npx-find-noninteractive
type: standard
validates: "Given sanitized env + piped stdout (no TTY) + timeout, when `npx skills find <query>` runs, then ranked results (owner/repo/skill IDs) are machine-readable from stdout; cold/warm latency + npm egress measured"
verdict: VALIDATED
related: [003, 004a, 004b, 012a, 013]
tags: [skills, npx, discovery, phase-11]
---

# Spike 011: npx-find-noninteractive

## What This Validates

The kill-shot question for the operator's "agent-with-terminal" re-litigation of the discovery
leg: the skills CLI documents `find` as *interactive* — if it has no parseable non-TTY mode, the
CLI cannot replace `internal/skills/catalog.go` as Aura's discovery transport and the idea
collapses back to the HTTP API. Session-3 partner of 012a/b (orchestration) and 013 (gate parity).

## Research

CLI surface read from `npx --yes skills --help` (v1.x live, no docs consulted — ground truth
only). Relevant legs: `find [query]` ("Search for skills interactively"), `add <repo> --list`
("List available skills in the repository without installing"), `use <pkg>@<skill>` (prompt-only,
untested — adjacent future surface for read-without-install).

## How to Run

```bash
go run ./.planning/spikes/011-npx-find-noninteractive
```

Requires `npx` on PATH; P6 needs the compose stack up (`aura-sandbox-agent` running) and
WARN-skips otherwise. Scratch: `D:\tmp\spike-011*` (cold-cache measurement only).

## What to Expect

6 PASS lines (find-parse + North-Star, CLI/API parity 5/5, empty-query no-hang, add --list
18 skills, warm latency ~3s avg, in-sandbox parity), `[SUMMARY] VALIDATED`, exit 0.

## Investigation Trail

1. **"Interactively" is TTY-conditional.** Piped (no TTY, closed stdin) `find <query>` degrades
   to a plain ranked listing — one entry per skill: `owner[/repo]@skill N installs` + a
   `https://skills.sh/...` line. Exit 0. No TUI, no hang.
2. **Empty query does NOT hang** — prints usage plus *agent-targeted teaching*: "Tip: if running
   in a coding agent, follow these steps: 1) npx skills find [query] 2) npx skills add
   <owner/repo@skill>". Upstream explicitly designs for the agent loop this spike proposes.
3. **NO_COLOR is ignored** — ANSI codes always emitted. Parser must strip (`\x1b\[[0-9;?]*[a-zA-Z]`
   covers colors, cursor-hide, and the `add` spinner repaints). Box-drawing `└`/`│` arrive UTF-8.
4. **Agent detection is built in:** with a coding-agent env present, `add` prints
   "claude-code_…_agent Agent detected — installing non-interactively" and skips all prompts.
5. **`add <repo> --list` = clone + enumerate without install:** 18 skills for anthropics/skills
   in 3.5s, each with its FULL frontmatter description — much richer than `find` output. This is
   the multi-skill-selector surface amendment #49 iteration 5 hand-built (catalog format-boost +
   per-skill selector).
6. **First P2 FAILED honestly — marketplace re-ranking, not CLI fault.** Spike 003 (same morning)
   recorded "find skills" → vercel-labs/skills@find-skills top (1.86M installs). Hours later BOTH
   the CLI and the raw `/api/search` return `agentspace-so/skills@find-skills` (76K) top for that
   semantic query; the hyphenated exact-name `find-skills` query returns vercel-labs at 1.9M, rank
   1, `searchType:fuzzy`. P2 was rewritten from a ranking-stability assertion (wrong invariant —
   it tested the volatile marketplace) to a transport-parity assertion: CLI top-5 vs API top-5 for
   the same query = **5/5 overlap**. The CLI is a faithful `/api/search` client.
7. **Name-squatting is live:** `agentspace-so/skills@find-skills` (76K installs) is a name-clone
   of the vercel-labs skill that now OUTRANKS it on the semantic query. Direct evidence for 013:
   the D-13 red-flag gate and provenance pinning must survive any transport pivot.
8. **Latency:** warm avg 3.09s/query (3 runs), cold with a virgin npm cache 3.9s (npx package
   fetch ≈ +1s — npm registry egress is part of the prod posture). Spike-003 Go HTTP client:
   0.25-0.7s. The CLI costs ~2.5s more per discovery — noise inside an LLM turn.
9. **The sandbox already ships the runtime:** `aura-sandbox-agent` has node v22.22.2 + npx + git
   baked. In-sandbox `find` returns identical parseable results (1.6-2.3s — faster than host:
   no Windows process-spawn tax). The 004b "drops node dep" argument is moot in-sandbox; the ro
   `/skills` mount makes in-sandbox discovery read-only by construction.

## Results

**VALIDATED** — `npx skills find` and `npx skills add --list` are non-interactive, machine-
parseable discovery transports, runnable both host-side and inside the existing sandbox image
with zero image changes.

Contract for 012a/013:
- Parse after ANSI-strip; entry shape `owner[/repo]@skill N[K|M] installs` (bare registry hosts
  like `modelscope.cn@skill` occur — owner/repo optional).
- Closed stdin + timeout is safe for every probed leg; empty/garbage queries exit 0 with usage.
- Single-token (format/name) queries rank dramatically better than topic phrases — the
  amendment-#49 iteration-4 "query by format keyword" teaching applies UNCHANGED to this
  transport; the find-skills SKILL.md "use specific keywords" tip says the same thing.
- Ranking is volatile server-side and name-squatting exists: discovery output is UNTRUSTED INPUT;
  install decisions still need the D-13 red-flag gate + pinned provenance (013's subject).
- npm-registry egress (npx resolution) joins skills.sh + github in the native-Linux allowlist
  conversation (spike 009).
