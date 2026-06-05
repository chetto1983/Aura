---
spike: 013
name: thin-surface-gate-parity
type: standard
validates: "Given the 012 winner shape, when the deletable surface is enumerated and the controls re-anchored, then a parity table + true LOC delta shows what MUST remain"
verdict: VALIDATED
related: [012a, 012b, 011, 005, 004b]
tags: [skills, security, architecture, phase-11]
---

# Spike 013: thin-surface-gate-parity

## What This Validates

What the 012a winner (skill-driven self-extension, no-ceremony directive) deletes,
what survives, and where each surviving control re-anchors. Evidence: live probes
(011/012 runs, the rw-mount probe below) + LOC measurement + nanobot as the
production reference shape.

## Reference shape (how professional coders do it — D:\tmp\nanobot)

- `nanobot/agent/skills.py` — **242 LOC**: a loader scanning `workspace/skills/` +
  builtin dir, frontmatter parse, always-skills, context summary. THE WHOLE SYSTEM.
- `nanobot/skills/clawhub/SKILL.md` — **~55 lines**: teaches
  `npx clawhub search/install/update/list --workdir ~/.nanobot/workspace`. The agent
  self-installs into the loader-scanned dir; "start a new session to load it".
- No catalog client, no installer, no gate, no audit store, no pending/approve.
- Aura today: **7,590 LOC** for the skills system. nanobot: 242 + markdown.

## Probes

1. **rw `/skills` confirmed live** (`docker exec touch /skills/... → exit 0`): NOT a
   regression — amendment #50 / D-15c deliberately inverted spike 005's ro mount
   (full-terminal home, `--no-token`; single-trusted-operator posture; per-identity
   gating arrives with capability_grants, Slice 1.7).
2. **In-sandbox install landing spots** (012a full-loop): the CLI's agent detection
   lands skills under `<cwd>/.agents/skills/` (and/or `.claude/skills/`). Session
   workspace installs land in `/workspace/.agents/skills/`; a PERSISTENT install
   needs the cwd to be the host-visible mount: `cd /skills && npx skills add …` —
   the nanobot `--workdir` analog. The loader-root wiring (scan
   `/skills/.agents/skills` host-side, or teach a flat target) is the ONE open
   implementation decision.

## Delete table (model-facing discovery+install complex)

| Surface | LOC | Fate |
|---|---|---|
| `internal/skills/catalog.go` + test | 326 | DELETE — discovery = `npx skills find` |
| `internal/agent/tools/skill_install.go` + test (catalog+install actions, retry-hint, gate render) | 457 | DELETE — both model-facing legs |
| `internal/skills/installer.go` + 3 tests | 1,027 | DELETE — npx materializes; 004b's native-clone win is moot when the model drives the CLI itself |
| `cmd/aura/skills.go` catalog leg + install/approve legs | ~150 | DELETE (operator keeps `aura skills list/info/audit`) |
| serve/eval catalog+installer adapters | ~90 | DELETE |
| §Skills system-prompt 5-step REQUIRED workflow (amendment #49) | — | SHRINKS to a pointer; teaching = find-skills skill content (messages[1]) |
| **Total deletable** | **~2,050 (≈1,200 non-test)** | plus the #49 routing-churn surface — the real cost |
| find-skills-aura SKILL.md builtin | +~50 lines md | ADD (MaterializeBuiltins) |

## Survival list (controls re-anchored, not dropped)

| Control | Old anchor | New anchor |
|---|---|---|
| Injection blocklist (`AURA_SKILL_INJECTION_BLOCKLIST`) | Writer/Installer at staging | **MUST move to Loader scan** — self-installed bodies never pass the Writer. The one hard security keep. |
| Skill authoring (7a create/update/delete + pending/approve) | Writer | UNCHANGED — authoring is Aura-generated content, separate path |
| Snippets (7e) + TTL + audit of authored skills | Writer/audit store | UNCHANGED |
| Loader (manifest, always-block, BM25 overflow) | Loader | UNCHANGED — and gains a root (the persistent-install dir) |
| Sandbox containment | ro mount (005) | superseded by #50: full-terminal home; capability_grants (1.7) is the future gate |
| Name-squat / provenance prudence (011 finding: 76K-install find-skills clone) | renderInstallGate red flags (dead code per 012b) | the skill's "how to choose" teaching — which the model FOLLOWED in all 4 live runs (named installs + provenance unprompted) |

## Dead-code findings that die with the deletion (from 012b)

The name-gated pause machinery (`pauseCalls`), the dropped `ErrAwaitingUserInput`
payload ("error: awaiting user input"), and `renderInstallGate`'s unreachable D-13
red flags — all in the DELETE column above. No fix needed; the surface goes away.
The shipped eval capture blindness (toolNames can't see skill actions) still needs
fixing for whatever D-35 scenario replaces the approval-sequence assertion
(artifact + self-install evidence, as the 012 harness does).

## Results

**VALIDATED.** The thin shape is real and reference-proven: delete ~2,050 LOC of
model-facing discovery/install Go, add ~50 lines of skill markdown + a loader root +
a loader-level blocklist scan. PRD amendment required (supersedes D-03/D-13-gate/
D-35-install-prudence + parts of #49; aligns with #50's full-terminal posture)
before implementation lands.
