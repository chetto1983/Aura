# Phase 11: Skills - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-05
**Phase:** 11-skills
**Areas discussed:** Tool surface shape, Skill discovery + prompt injection, Catalog 7b keep/cut, 7e snippets on sandbox-agent, E2E + eval strategy, Validator + fuzz scope, Sub-slice commit mapping, Starter skill content, Headless mutation surface, Audit constraint redesign, Env-var catalog

**Method note:** user mandated research-first (initial freeform answer: "we must search best 2026 pattern online and on d:/tmp and use also https://www.skills.sh/ as repository to make easy install for end user"). 4 researcher passes ran (2026 industrial patterns + agentskills.io; D:/tmp curated scan; skills CLI/registry live probe; always-on injection-point deep-dive). The user repeatedly answered "look claude code how work (self test on you)" — those decisions record the live Claude Code harness behavior as the verdict.

---

## Tool surface shape

| Option | Description | Selected |
|--------|-------------|----------|
| ONE `skill` tool | Action enum via shipped ActionRouter (Phase-10 `task` precedent) | ✓ |
| Two tools: read vs mutate | `skill` + `skill_admin` risk split | |
| Many tools per PRD | 7-11 separate skill_* files | |

**User's choice:** ONE `skill` tool (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Full PRD set, gated | All mutations model-facing, pending/ + ask_user gate | ✓ |
| Trim delete to CLI-only | DESTRUCTIVE delete off the model surface | |
| Read/run only for model | Peer-parity, kills self-extension smokes | |

**User's choice:** Full PRD set, gated (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Non-deferred | Always visible, CC parity | ✓ (via self-test) |
| Deferred behind tool_search | Leanest manifest, two-hop discovery | |
| Split: read hot, rest deferred | Reopens Q1 | |

**User's choice:** "look claude code how work (self test on you)" → self-test verdict: non-deferred, minimal-friction, list out-of-prefix, bodies on demand.

| Option | Description | Selected |
|--------|-------------|----------|
| Inside `skill` tool | action=run with metadata stamping | |
| Separate `skill_run` tool | Dedicated schema | |
| Via sandbox_exec directly | CC pattern: body/path returned, agent executes | ✓ (via self-test) |

**User's choice:** "look claude code how work (self test on you)" → self-test verdict: by-PATH execution via the generic execution tool; no bespoke run machinery; CLI exec stamps metadata; argv-prefix usage hook = planner discretion.

| Option | Description | Selected |
|--------|-------------|----------|
| No model approve | Activation only via ask_user resume or CLI | ✓ (via self-test) |
| Keep, but ask_user-locked | Approve as verified bookkeeping | |
| Keep per PRD | Model self-approves, audit-only control | |

**User's choice:** "look claude code how do (test your self)" → self-test verdict: the model cannot approve its own actions; permission is harness-level. skill.approve cut.

| Option | Description | Selected |
|--------|-------------|----------|
| Mechanism yes, content minimal | embed.FS + fingerprint, 1-2 starters | ✓ |
| No builtins in v1 | Single root only | |
| Curated starter pack | Real set embedded now | |

**User's choice:** Mechanism yes, content minimal (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Global FS, identity in audit | One tree; identity via actor_id columns | ✓ |
| Per-identity dirs now | OQ2 literal shape | |

**User's choice:** Global FS, identity in audit (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Diff in gate, reject discards | Unified diff in approval; old serves until swap | ✓ |
| Content-hash only in gate | Approve blind | |

**User's choice:** Diff in gate, reject discards (Recommended)

---

## Skill discovery + prompt injection

| Option | Description | Selected |
|--------|-------------|----------|
| In skill tool Description | 08.1 D-09 pattern, turn-stable, messages[0] untouched | ✓ |
| On-demand via action=list | Weak triggering | |
| Appended context turn | New Runner machinery | |

**User's choice:** In skill tool Description (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Two-tier, messages[1] block | always:true bodies in user-role block at messages[1] | ✓ (research-backed) |
| Defer to Phase 14 / Agent.md | On-demand only v1 | |
| Rebuild system prompt per PRD | Violates CAP-04 | |

**User's choice:** "search claude code and 2026 industrial pattern" → dedicated researcher pass; convergent verdict (CC CLAUDE.md = user msg after system; Codex user-role fragments; nanobot system-concat = outlier anti-pattern) confirmed the messages[1] two-tier option.

| Option | Description | Selected |
|--------|-------------|----------|
| action=use, framed as instructions | Authority frame, CC ADOPT semantics | ✓ |
| info only, body as plain result | PRD literal | |

**User's choice:** action=use, framed as instructions (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Cap + BM25 overflow | ~8k cap + "N more — search" + BM25 list | ✓ |
| Always list everything | PRD literal, degradation past ~50 | |
| Auto-shorten to fit | Codex literal, loses trigger phrases | |

**User's choice:** Cap + BM25 overflow (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Gate shows it loud; installs strip it | ⚠ ALWAYS-ON in gate; install strips flag | ✓ |
| CLI-only flag | Strongest, kills haiku magic | |
| Normal gate suffices | No distinction | |

**User's choice:** Gate shows it loud; installs strip it (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Count + byte cap, env-tunable | AURA_SKILL_ALWAYS_MAX=3 | |
| Byte cap only | | |
| No cap v1 | Human gate is the governor | ✓ (via self-test) |

**User's choice:** "look claude code (test your self on installed skills)" → self-test verdict: no enforced cap on always-loaded content (CLAUDE.md); structural bounding + visibility; gate is the governor.

---

## Catalog 7b keep/cut

| Option | Description | Selected |
|--------|-------------|----------|
| JSON API + CLI fallback | GET /api/search + npx skills find fallback | (provisional) |
| CLI subprocess only | | |
| JSON API only | | |

**User's choice:** "we must do a /gsd-spike on this" → transport decision delegated to a MANDATORY pre-planning spike (Phase 9 spikes precedent). Provisional lean recorded.

| Option | Description | Selected |
|--------|-------------|----------|
| Flip: browse ON, install gated | Amendment #14 superseded; gate moves to install | ✓ |
| Keep opt-in per amendment #14 | Default-deny stands | |

**User's choice:** Flip: browse ON, install gated (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Tolerate, surface in gate | Spec-interop frontmatter; red flags surfaced | ✓ |
| Strip everything non-core | Lobotomizes corpus | |
| Reject on red flags | Rejects real ecosystem | |

**User's choice:** Tolerate, surface in gate (Recommended)

---

## 7e snippets on sandbox-agent

| Option | Description | Selected |
|--------|-------------|----------|
| Scheduler TaskKind | skill_ttl_sweep seeded like backups | ✓ |
| Bespoke goroutine per PRD | ttl_sweeper.go | |
| Lazy sweep on access | | |

**User's choice:** Scheduler TaskKind (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Read-only skills mount | host export dir → /skills ro | ✓ |
| Copy into /workspace on use | | |
| Inline at exec time | | |

**User's choice:** Read-only skills mount (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Docs-only, tier-relevant kept | language+description enforced; needs_* in gate | ✓ |
| Full PRD contract | Enforced inputs_schema | |
| Minimal: language only | | |

**User's choice:** Docs-only, tier-relevant kept (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Sidecar JSON + runs table | One live-state source + forensics table | ✓ |
| DB-only state | | |
| Full PRD (all three) | Guaranteed drift | |

**User's choice:** Sidecar JSON + runs table (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Ratify: implicit via audit | content_hash recovery | ✓ |
| Keep last-N bodies | rollback machinery | |

**User's choice:** Ratify: implicit via audit (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Ratify both | Conversation-scoped outputs + active-only materialization | ✓ |
| Outputs need own retention | | |

**User's choice:** Ratify both (Recommended)

---

## E2E + eval strategy

**User-authored North-Star scenario (freeform):** install vercel-labs/skills/find-skills via the real flow; then Aura must autonomously find + gate-install anthropics/skills/xlsx from skills.sh and produce an Excel file of today's Yahoo Finance market — "must find your self then ask".

| Option | Description | Selected |
|--------|-------------|----------|
| Dual gate, Phase-9 style | Ground-truth floor + judge ≥90%, cot_eval operator-run | ✓ |
| Ground-truth only | No judge | |
| Operator UAT only | Manual checklist | |

**User's choice:** Dual gate, Phase-9 style (Recommended)

---

## Validator + fuzz scope

| Option | Description | Selected |
|--------|-------------|----------|
| Hard for model, override for operator | Model hard-reject; CLI --allow-blocklisted with shown matches | ✓ |
| Hard-reject everywhere (PRD literal) | ChatML-documenting skills uninstallable | |
| Neutralize instead of reject | Render-time defanging | |

**User's choice:** Hard for model, override for operator (Recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Parse + structure only | Blocklist at write boundaries only; disk operator-trusted | ✓ |
| Full validation incl. blocklist | Override-marker machinery | |

**User's choice:** Parse + structure only (Recommended)

---

## Sub-slice commit mapping

| Option | Description | Selected |
|--------|-------------|----------|
| Keep lettering, amended content | Wave-0 amendment → 7a→7b→7c→7d→7e | ✓ |
| Re-slice by architecture | | |
| Planner decides | | |

**User's choice:** Keep lettering, amended content (Recommended)

---

## Starter skill content

| Option | Description | Selected |
|--------|-------------|----------|
| skill-creator only | One meta-skill embedded; find-skills via E2E install | ✓ |
| skill-creator + find-skills | Pre-empts the E2E | |
| Zero builtins | | |

**User's choice:** skill-creator only (Recommended)

---

## Headless mutation surface

| Option | Description | Selected |
|--------|-------------|----------|
| Allow — pending + alert path | Can never self-activate; overnight-proposal as feature | ✓ |
| Strip mutations from children | Per-context action filtering | |

**User's choice:** Allow — pending + alert path (Recommended)

---

## Audit constraint redesign

| Option | Description | Selected |
|--------|-------------|----------|
| Ratify matrix | 5-row coherence matrix (NULLable approval_source + blocklist_override) | ✓ |
| Planner re-derives | | |

**User's choice:** Ratify matrix (Recommended)

---

## Env-var catalog

| Option | Description | Selected |
|--------|-------------|----------|
| Ratify set | 8 vars locked, planner refines in convention | ✓ |
| Planner owns it fully | | |

**User's choice:** Ratify set (Recommended)

---

## Claude's Discretion

- ActionRouter handler file split, per-action description wording, `use` authority-frame literal (+ asserting test)
- sandbox_exec argv `/skills`-prefix usage-stamping hook design
- BM25 reuse shape for skill list
- Migration split (0010 vs 0010+0011), sqlc queries, store adapter
- messages[1] block rendering + L2.5 evictor protection mechanics
- skill-creator builtin content authoring
- `aura skills` CLI switch rendering
- Notifier route reuse for gate-skipped alerts

## Deferred Ideas

- 7f pattern-analyzer auto-suggest (SKILL-V2-01, v1.x)
- Neo4j HNSW semantic skill discovery (Phase 15 era)
- Skill versioning/rollback machinery
- Snippet-output retention policy beyond conversation lifetime
- Runtime inputs_schema enforcement
- `/skill-name` slash invocation in channels (Phase 13)
- skills.sh public API adoption when vercel-labs/skills#426 ships
- Edit-on-approve (AG-UI era)
