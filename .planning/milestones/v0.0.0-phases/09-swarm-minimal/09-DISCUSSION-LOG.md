# Phase 9: Swarm (Minimal) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-04
**Phase:** 9-swarm-minimal
**Areas discussed:** Tool surface & bus fate, Child pause propagation, Child prompt & KV discipline, Demo & validation surface, Report contract & spillover, Coordinator lifecycle, Child observability, Budget pre-flight, Nesting depth, Hung child, Child forensics, Amendment placement, Tool concurrency, Anti-over-spawn guidance, Property-based obligation, E2E live + MCP, MCP allowlist & placement, MCP server swap

**Research performed before/during discussion (user-requested, "panoramica completa senza bombe atomiche"):**
1. Local curated sources (`D:\tmp`): claude-code.md Agent tool spec, codex-rs multi_agents_v2, nanobot, picobot, paper.md, adk-go.
2. Online: Anthropic multi-agent research system, Claude Agent SDK subagents, OpenAI Agents SDK (incl. nested HITL), Swarm lineage, Google ADK.
3. Focused: subagent system-prompt construction + prompt-caching economics (incl. claude-code#29966, DeepSeek KV cache docs).
4. Live self-test: 2 subagents spawned on Claude Code itself (Explore + general) — confirmed no Agent tool in children, no AskUserQuestion, fresh context, base-prompt+overlay construction, `needs input:` report convention.
5. `ruvnet/ruflo` cloned to `D:\tmp` (user-requested) and studied — cautionary maximal reference.

---

## A. Tool surface & bus fate

| Option | Description | Selected |
|--------|-------------|----------|
| Array-of-goals | 1 deferred tool blocks, internal ParallelAgent fan-out, zero dispatcher changes | ✓ |
| N parallel calls (Claude Code idiom) | single-goal tool + parallel same-turn dispatch (touches llm_agent.go D-14) | |
| Pair spawn/join (PRD) | 2 tools + live handle registry | |

**Follow-ups:** Partial failure → per-child isolation (vs fail-fast / threshold) ✓. `tier` param → dropped from v1 schema (vs accepted-but-no-op) ✓.
**Notes:** swarm_talk/bus cut confirmed by research unanimity; PRD acceptance list flagged stale.

## B. Child pause propagation

| Option | Description | Selected |
|--------|-------------|----------|
| Pause-as-report | child terminates with needs_user_input entry; parent relays via own ask_user | ✓ |
| Park in-process + proxy (PRD OQ5) | live parked children + ResumeChild — Responder under another name | |
| Omit ask_user from children (Claude stance) | simplest, loses capability | |

**Follow-up:** proxied_* columns → optional args on ask_user (model fills from report ground truth) — user chose this over NULL-in-v1 recommendation.
**Notes:** OpenAI nested-interruption precedent validates the proxy semantic; live self-test confirmed Claude Code uses pause-as-report (`needs input:`).

## C. Child prompt & KV discipline

| Option | Description | Selected |
|--------|-------------|----------|
| Base parent + worker overlay | one source of truth, byte-stable across workers (what Claude Code does live) | ✓ |
| Identical to parent | max parent-cache kinship, wrong conversational directives in workers | |
| Dedicated worker prompt from scratch | literal Agent SDK pattern, second source of truth | |

**Notes:** user first rejected the question and asked for online industrial research; verdict was unanimous dedicated-per-type, refined by the live self-test showing base+overlay. DeepSeek cache rewards repetition, not lineage.

## D. Demo & validation surface

| Question | Selected |
|----------|----------|
| Operator demo | `aura swarm-demo` mock (dry-run 02-07 pattern) ✓ vs tests-only |
| E2E live | cot_eval gated tier ✓ vs manual UAT |
| Overflow | internal waves + AURA_SWARM_MAX_GOALS=8 cap ✓ vs reject |

## E/F/G. Report contract / Coordinator lifecycle / Child observability

Informed by the ruflo study (user-requested clone).

| Question | Selected |
|----------|----------|
| Report | compact ChildReport array, per-child cap only, NewResult spillover ✓ vs double cap |
| Lifecycle | ephemeral per-call runner, deterministic w1..wN ✓ vs persistent coordinator (PRD) |
| Observability | silent-until-done + 3 slog lines ✓ vs event-forwarding seam now |

## H/I/J/K. Budget guard / Nesting / Hung child / Forensics

| Question | Selected |
|----------|----------|
| Budget | pre-flight guard + ~3-step parent reserve ✓ vs no guard |
| Nesting | FLAT v1 (re-spec SC#2) ✓ vs 1-level nesting (PRD literal) |
| Hung child | AURA_SWARM_CHILD_TIMEOUT_SEC per-child deadline ✓ vs shared wallclock only |
| Forensics | always-on transcript dump to $AURA_RUN_DIR ✓ vs none |

## L/M/N/O. Amendment / Tool burst / Spawn policy / Property-based

| Question | Selected |
|----------|----------|
| Amendment | Wave-0 doc-only plan (05-01/08-01 precedent) — locked with recommendation |
| Tool burst ×N | accepted, no per-tool semaphores ✓ vs sandbox semaphore |
| Spawn policy | load-bearing literal Description + asserting test ✓ vs user-dictated text |
| Property-based | rapid invariants locked (order/length, budget tree, goleak, isolation) |

## P/Q/R. E2E with real agents + MCP

User-initiated: "abiliterei gli MCP AURA_MCP_CALENDAR_SERVER e AURA_MCP_WHATSAPP_SERVER e farei prompt naturali per testare con score >90%".

| Question | Selected |
|----------|----------|
| Scope | BOTH MCP servers mandatory in Gate 3 (user chose over env-gated-optional recommendation) |
| Scoring | judge ≥90% + ground-truth assertions (the fullest option) |
| Calendar backend | user pointed to MarimerLLC/calendar-mcp (verified: .NET 10, stdio, multi-backend incl. ICS/JSON fixtures) |
| WhatsApp | user's own account, messages to self ✓ |
| Allowlist | allowlist at Mount ✓ vs full mount |
| Placement | boot-level env-gated (daily-usable in aura chat) ✓ vs harness-only |

## Swap: mail-mcp replaces calendar-mcp

User proposed martinzarfl/mail-mcp "più semplice per i test". Verified: email-only (no calendar), Node, stdio, env-var config, no OAuth.

| Option | Description | Selected |
|--------|-------------|----------|
| Mail + WhatsApp | calendar deferred to Phase 16; both servers simple + read-back ground truth | ✓ |
| Mail + WhatsApp + Calendar | three infra to pair in Gate 3 | |
| Mail instead of WhatsApp | minimal setup, loses the WhatsApp scenario | |

## Claude's Discretion

- Worker-overlay text + structured-brief template (load-bearing-literal tested)
- errgroup/wave details; per-child error isolation around ParallelAgent's cancel semantics
- Judge rubric weights + control-prompt set (gate ≥90% fixed)
- WhatsApp bridge selection (whatsmeow, stdio, self-chat read-back)
- Mount allowlist signature; per-child summary cap knob (prefer reusing AURA_CONTEXT_PREVIEW_CAP_BYTES)
- D-09 reserve size tuning

## Deferred Ideas

- swarm_talk / bus / DM-by-ID → v2 SWARM-V2-01
- Tier-mapped models → v2 SWARM-V2-01
- spawn/join async pair; nested spawn enablement; event-forwarding seam (AG-UI Phase 12); {childId, phase} progress shape
- Calendar MCP scenario + recipes/doctor/risky-tool labeling → Phase 16
- Per-tool semaphores (only on observed contention); LLM-compressed child summaries
