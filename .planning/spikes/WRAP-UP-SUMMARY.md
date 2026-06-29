# Spike Wrap-Up Summary

**Latest wrap-up:** 2026-06-29 (Session-21, spikes 078–081 — append mode).
**Cumulative:** 81 spikes wrapped into `./.claude/skills/spike-findings-Aura/` across sessions 1–21. The full per-spike record (all verdicts, tags, session narrative) lives in `.planning/spikes/MANIFEST.md` and the skill's `<metadata>` `processed_spikes` list; per-area implementation blueprints are in `references/*.md`.

## This run (Session-21, 078–081) — Multi-user per-identity isolation (Phases 36–37)

| # | Name | Type | Verdict |
|---|------|------|---------|
| 078 | per-identity-box-multiplexing | standard | VALIDATED ✓ (live) |
| 079 | agent-sandbox-api-contract | standard | VALIDATED ✓ (design) |
| 080 | garage-per-identity-isolation | standard | VALIDATED ✓ (live) |
| 081 | mcp-skills-per-identity-scoping | standard | VALIDATED ✓ (design) |

New skill artifacts: `references/multiuser-per-identity-isolation.md`, `sources/078..081-*/README.md`, feature-area index row + `processed_spikes` 078–081 + a binding Requirements bullet in `SKILL.md`.

## Key Findings (this run)

- **Per-identity full-capability sandbox over Docker** resolves audit F-001 by containment, not by removing the full-host terminal. Live: separate named volumes isolate data, no `docker.sock`/host-net/bind, ~1 MB idle/box. K8s + agent-sandbox = DGX future tier; Aura mirrors the Sandbox/Template/Claim pattern over the Docker SDK (Backend seam).
- **Garage per-identity = bucket-per-identity + scoped key.** Garage grants are **per-bucket, not per-prefix** (live-verified) → shared-bucket key-prefix would be a silent isolation hole. Per-identity keys close F-007.
- **MCP isolation is three classes** (operator caution: calendar, agent-memory, whatsapp): (a) stdio → in-box; (b) agent-memory → shared graph + mandatory identity scope key (fork-enforced); (c) **calendar/PIM + whatsapp → per-user-account sidecars needing a per-identity instance** (OAuth/pairing per identity — scope key insufficient). Class (c) has real per-identity resource/onboarding cost → Phase-36 decision.
- Per-identity MCP/skills config reuses the `~/.aura/agents/<id>/` (`Agent.md`) filesystem-rooting pattern; execution routes through the identity's box.

## Prior wrap-ups (history)

Sessions 1–6 (32 spikes, 2026-06-07): skills-self-extension, sandbox-runtime, mcp-live-servers, agui-gateway, telegram-channel, multimodal-9c. Sessions 7–20 (spikes 031–077) extended the blueprint across memory, ingestion, onboarding, adaptive-reasoning, tool-search/semindex, local-LLM, packaging, calendar/PIM, graph-DB eval, and RAG hardening. Full detail: `MANIFEST.md` + the per-area files under `references/`.

## Next-tier impl proofs (deferred to Phase 36–37 implementation)

- Live S3 PUT-as-A / GET-denied-as-B round-trip (080 grant-level boundary proven).
- 2-identity agent-memory recall isolation test (081 / 032 / 034).
- Per-identity concurrent-box cost benchmark on the real host (078 idle proven).
- calendar/whatsapp per-identity-instance vs multi-account fork decision (081 class (c)).
