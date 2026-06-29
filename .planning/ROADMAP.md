# Roadmap: Aura

## Milestones

- ✅ **v0.0.0 Substrate** — Phases 0–21 (shipped 2026-06-15) — full details in [`milestones/v0.0.0-ROADMAP.md`](milestones/v0.0.0-ROADMAP.md)
- ✅ **v1.0.0 Aura Deep Search Web Cockpit** — Phases 22–30 (shipped 2026-06-29) — full details in [`milestones/v1.0.0-ROADMAP.md`](milestones/v1.0.0-ROADMAP.md)

## Phases

<details>
<summary>✅ v0.0.0 Substrate (Phases 0–21) — SHIPPED 2026-06-15</summary>

- [x] Phase 0: PRD Amendments (6/6) — 2026-05-29
- [x] Phase 1: Infra DB + Knowledge — 2026-05-30
- [x] Phase 2: Agent Cornerstone (8/8) — 2026-05-30
- [x] Phase 3: LLM Client + ToolResult (5/5) — 2026-05-30
- [x] Phase 4: HITL + Identity + Conversations (5/5) — 2026-05-30
- [x] Phase 6: KV Cache Builder (5/5) — 2026-06-02
- [x] Phase 7: Web Tools (4/4) — 2026-06-02
- [x] Phase 07.1: Agent-Loop Forced Finalization (INSERTED) — 2026-06-03
- [x] Phase 8: Sandbox via sandbox-agent (replaces bespoke 2a/2b) — 2026-06-03
- [x] Phase 08.1: Tool Search hardening — defer_loading parity (INSERTED) — 2026-06-03
- [x] Phase 08.2: Semantic tool_search + unified semindex (INSERTED) — 2026-06-05
- [x] Phase 9: Swarm (Minimal) (6/6) — 2026-06-04
- [x] Phase 10: Scheduler (6/6) — 2026-06-04
- [x] Phase 11: Skills (10/10) — 2026-06-06
- [x] Phase 12: AG-UI Gateway (6/6) — 2026-06-07
- [x] Phase 13: Channels + Telegram + Multimodal (10/10) — 2026-06-08
- [x] Phase 14: Onboarding + Agent.md (5/5) — 2026-06-14
- [x] Phase 15: Memory Subsystem (5/5) — 2026-06-12
- [x] Phase 16: MCP Sidecar Manager + Third-Party Trust (8/8) — 2026-06-04
- [x] Phase 17: Packaging & Distribution (8/8) — 2026-06-14
- [x] Phase 18: Slice 7e Executable Snippet Reuse — 2026-06-08
- [x] Phase 19: Audit Bug Resolution + E2E Live Test — 2026-06-10
- [x] Phase 20: Scheduler Hardening Full Implementation — 2026-06-11
- [x] Phase 21: Plugins — Hooks (Slice EXT-1) — 2026-06-15

> Phase 5 (bespoke Sandbox 2a) was superseded by the Phase 8 sandbox-agent pivot (D-15) and is not counted.

</details>

<details>
<summary>✅ v1.0.0 Aura Deep Search Web Cockpit (Phases 22–30) — SHIPPED 2026-06-29</summary>

Embedded Vite + React + assistant-ui operator cockpit over the AG-UI/SSE gateway, per `docs/design/aura-deep-search-figma/ux-spec.md`. Hardened the agent perimeter first, then built the research-locked industrial frontend foundation, the serve/auth/health web host, the Core-Value chat+approval loop, the typed-display spine + router, the read-only graph explorer, read-only governance + web onboarding, and finally the governance WRITE surfaces (MCP config + skills install/lifecycle, landing last after auth + approval center + read-only boards were proven). The `ui_control` operator-OS shell and scheduler write surfaces were deferred to a follow-up milestone.

- [x] Phase 22: Agent Perimeter Hardening (5/5) — 2026-06-15 (Gate-3 closed w/ operator live sign-off 2026-06-16)
- [x] Phase 23: Frontend Infrastructure & Industrial Foundation (3/3) — 2026-06-16
- [x] Phase 24: Web Foundation — Serve + Auth + Health (4/4) — 2026-06-16
- [x] Phase 25: Chat + Approval Center (7/7) — 2026-06-17
- [x] Phase 26: Typed-Display Protocol + Router (6/6) — 2026-06-19
- [x] Phase 27: Neo4j Graph Explorer (4/4) — 2026-06-19
- [x] Phase 28: Governance Boards + Web Onboarding (6/6) — 2026-06-20
- [x] Phase 29: Governance Write — MCP Configuration + Skills Install (5/5) — 2026-06-21
- [x] Phase 30: Retrieval & Memory Pipeline Hardening — Rerank + Full-Docs E2E (5/5) — 2026-06-28

> **Cockpit Overhaul (post-Phase-25, not a formal phase).** A premium-bar overhaul reworked the
> Phase-23/24/25 surfaces in place: a logo-matched **blue** design system (operator-accepted
> 2026-06-18, fonts + WCAG AA gate), a responsive shell (svh grid, drawers, edge-swipe,
> intent-restore, 380px floor), chat/footer enrichment, **Authula** embedded auth (flag-gated),
> an `aura.settings` settings page, and calendar/PIM + WhatsApp connect. Specs + adversarial
> validation + per-spec ledgers: `docs/cockpit-overhaul/` (`00-VALIDATION.md` = umbrella).
>
> **Closeout:** `override_closeout` — audit PASSED (56/56), 6 deferred-by-design verification items
> (GPU-host live tiers unrunnable on this 4GB-GPU host + live-CI-only tiers + Phase-25
> carried-forward UAT). See `.planning/STATE.md` → Deferred Items and `milestones/v1.0.0-MILESTONE-AUDIT.md`.

</details>

## Progress

| Milestone | Phases | Plans | Status | Completed |
| --------- | ------ | ----- | ------ | --------- |
| v0.0.0 Substrate | 0–21 (24 phases) | 144/144 | ✅ Shipped | 2026-06-15 |
| v1.0.0 Aura Deep Search Web Cockpit | 22–30 (9 phases) | 45/45 | ✅ Shipped | 2026-06-29 |
