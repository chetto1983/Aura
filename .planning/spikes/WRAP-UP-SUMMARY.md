# Spike Wrap-Up Summary

**Latest wrap-up:** 2026-07-04 (Session-22, spikes 082–085 — append mode).
**Cumulative:** 85 spikes wrapped into `./.claude/skills/spike-findings-Aura/` across sessions 1–22. The full per-spike record (all verdicts, tags, session narrative) lives in `.planning/spikes/MANIFEST.md` and the skill's `<metadata>` `processed_spikes` list; per-area implementation blueprints are in `references/*.md`.

## This run (Session-22, 082–085) — Multi-user per-identity isolation, all four planes proven live

| # | Name | Type | Verdict |
|---|------|------|---------|
| 082 | agent-sandbox-realsource-contract | standard | VALIDATED ✓ (real source + live kind run; corrects 079) |
| 083 | two-identity-e2e-tenancy | integration | VALIDATED ✓ (box+Garage+memory together; closes 080/081 tiers) |
| 084 | per-identity-pim-sidecar | standard | VALIDATED ✓ (2-instance live; the 3rd MCP class) |
| 085 | document-ingest-tenancy | standard | VALIDATED ✓ (leak→fix live; the 4th plane) |

Updated skill artifacts: `references/multiuser-per-identity-isolation.md` (extended), `sources/082..085-*/`, feature-area index row, `processed_spikes` 082–085, and the multi-user Requirements bullet in `SKILL.md`.

### Key findings (this run)

- **082** corrected 079 against the real agent-sandbox source + a live kind cluster: `Resolve`=direct `Sandbox` create (NOT `SandboxClaim` — `WarmPoolRef` required); idle=`OperatingMode:Suspended`; the `Backend` seam should speak the **E2B protocol** (the operator's `agent-sandbox/agent-sandbox` is an E2B REST+MCP gateway built ON TOP of the CRD project). Both upstreams hard K8s-bound → Aura owns the Docker backend.
- **083** proved box + Garage + memory isolate the same A/B pair simultaneously through Aura's real `objectstore`/`mcp` seams — closing 080's PUT/GET tier and 081's 2-identity memory recall.
- **084** proved the 3rd MCP class (calendar/PIM + WhatsApp) needs a per-identity **instance**: cross-instance admin tokens 401, filesystem-isolated account stores, per-instance data-protection key rings, ~33 MiB idle each.
- **085** closed the document-ingest gap: the documents pipeline is identity-blind graph-side (unscoped `document_search` leaks); the fix mirrors **yesterday's memory MCP scoping** (`9a4ca594`, the `:User`-ownership pattern) applied to `:Document` — proven live. Documents = the **4th isolation plane**, unified with the others on one `:User{identifier}`=`identityctx` key.

**All four session-21 open tiers are now closed; the v2.0.0 multi-user isolation model has a live proof on every plane. Remaining work is Phase-36/37 build.**

---
_The section below is retained from the Session-21 wrap-up for history._

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

## Next-tier impl proofs — CLOSED in Session-22

- ~~Live S3 PUT-as-A / GET-denied-as-B round-trip~~ → **closed by 083** (403 cross-deny live).
- ~~2-identity agent-memory recall isolation test~~ → **closed by 083** (fact_count 1 for A, 0 for B).
- ~~calendar/whatsapp per-identity-instance decision~~ → **closed by 084** (per-identity instance validated; no upstream multi-account mode needed).
- **New, closed:** document-ingest tenancy (leak + `:User`-ownership fix) → **085**.
- Still deferred to build-time (not spikes): per-identity concurrent-box cost benchmark on the real 32 GB host (078 idle proven; ~33 MiB/PIM instance measured in 084).
