---
spike: 012a
name: discovery-skill-driven
type: comparison
validates: "Given a find-skills-derived skill active (messages[1] always-block) + a terminal exec tool (NO catalog action), when the xlsx North-Star scenario runs live vs DeepSeek-V4, then the model autonomously completes find → add → use → artifact"
verdict: VALIDATED
related: [011, 012b, 013, 003, 004a, 004b]
tags: [skills, discovery, cot-eval, phase-11]
---

# Spike 012a: discovery-skill-driven

## What This Validates

The heart of the session-3 re-litigation: does find-skills-style SKILL CONTENT + a
terminal replace the bespoke `action=catalog` Go tool AND the amendment-#49 routing
teaching (6 iterations)? Comparison partner of 012b (the shipped tool-driven flow),
same harness, same action-aware capture, same natural Italian D-35 prompt.

**Re-scoped mid-spike by operator directive (2026-06-05): NO install-approval
ceremony.** Aura self-installs in the sandbox, Claude-Code-style. The endpoint moved
from "reaches an install-approval pause" to the FULL autonomous loop with artifact
ground truth (`docker exec find /workspace -name '*.xlsx' -newermt <run-start>`).

## Research

- Variant B mirrors the proposed end-state: the adapted skill body
  (`find-skills-aura/SKILL.md`, ~50 lines) rides as the messages[1] always-block
  (`RoleUser`, `RenderAlwaysBlock` wire shape); registry has NO skill tool —
  sandbox_exec is the transport.
- HARDER-than-end-state confound, accepted: messages[0] still ships the stock
  SystemPrompt whose §Skills section teaches `skill action=catalog/install` +
  approval doctrine. B won despite the contradicting teaching.
- **Prior art (post-hoc, D:/tmp first next time): nanobot ships EXACTLY this
  architecture in production** — `skills/clawhub/SKILL.md` (~55 lines) teaches
  `npx clawhub search/install/update/list`; the agent self-installs into the
  loader-scanned `workspace/skills/`; the entire skills system is a 242-LOC loader.
  No catalog client, no installer, no gate. (vs Aura's 7,590-LOC skills system.)

## How to Run

```bash
set -a; . ./.env; set +a
AURA_LOOP_MAX_STEPS=40 AURA_LOOP_MAX_WALLCLOCK_SEC=480 \
  go run ./.planning/spikes/012a-discovery-skill-driven -variant skill -n 1
```

Paid, live (DeepSeek-V4 Flash). Logs: `D:\tmp\spike-012-skill*.log`. Do NOT pipe
through bare `tail` on a leashed call (buffer-loss on timeout); `tee` or redirect.

## What to Expect

`PASS hops=1 ... selfInstall=true target=true artifactFresh=true`, the call trace
showing `npx skills find xlsx` → `npx skills add anthropics/skills --skill xlsx
--copy -y` → SKILL.md read → artifact build → model-initiated openpyxl read-back,
`[SUMMARY] VALIDATED`.

## Investigation Trail

1. First both-variant attempt lost to the 600s Bash leash + `| tail` buffering. Per-
   variant file-redirect runs after that.
2. **Pre-directive endpoint runs (approval-gated SKILL.md): 3/3 PASS, ONE hop each**
   (walls 16.8/35.2/45.2s), zero self-install violations, every approval question
   named target + installs + provenance. Run 1 used `npx skills add --list` repo
   enumeration UNPROMPTED (the iteration-5 multi-skill-selector behavior, for free).
3. Operator directive landed mid-spike: drop the ceremony ("stop acting like Aura is
   unsafe — Claude Code just does it"). SKILL.md rewritten to teach
   `npx skills add <owner/repo> --skill <name> --copy -y` + use-by-path.
4. **Full-loop run: PASS, single turn, 212s, 28.9k prompt tokens:** find → add →
   located the installed SKILL.md (checked both `.claude/skills` and
   `.agents/skills` — the CLI's agent-detection landed it in the latter) → read
   instructions → probed openpyxl → pulled live data via web tools → wrote the
   workbook → **re-opened it via load_workbook twice on its own** (the §Sandbox
   verify-your-artifacts teaching held). Harness ground-truth probe: fresh .xlsx
   confirmed.
5. One rerun lost to an OpenRouter 120s first-byte timeout (`AURA_LLM_TOTAL_TIMEOUT_SEC`
   default) — infra flake, excluded; the SAME hang explains the earlier zombie run
   that died silently at SETUP. Operator stopped further paid reruns: live 4/4
   model-behavior PASS (3 endpoint + 1 full-loop) + nanobot production prior art is
   sufficient evidence.
6. Token note: usage rides only terminal-turn StateDelta — pause-ended runs read 0;
   the full-loop run captured 28,931/551/0 (p/c/cached).

## Results

**VALIDATED — the skill-driven loop beats the shipped flow on every observable.**

| Observable | tool-driven (012b) | skill-driven |
|---|---|---|
| Reaches correct target | 2/3 (gate payload dropped — see 012b) | 4/4 (3 endpoint + 1 full-loop) |
| Discovery query quality | topic-ish multi-word despite retry-hint | bare format token, as taught |
| Multi-skill repo handling | iteration-5 hand-built selector | `add --list`, unprompted |
| Routing teaching | 6 amendment iterations, still failing E2E | ~50 lines of skill markdown |
| Go surface exercised | catalog client + installer + gate | sandbox_exec (already shipped) |

For the build (013): discovery+install go skill-driven via the skills CLI; the
deletable surface and the survival list (loader, 7a/7e, blocklist-at-host-activation,
ro `/skills`) are 013's table. Persistence beyond the session (nanobot's `--workdir`
equivalent) is the one open implementation decision.
