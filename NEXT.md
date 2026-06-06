# NEXT — riprendere da qui

> Stato sessione 2026-06-06. Aggiornato alla chiusura di Phase 18 + Phase 11.

## Dove siamo

**15/20 fasi complete** — 0÷11 (infra DB/Neo4j, agent runtime, LLM client, ask_user, identity, conversations, sandbox→host pivot, web tools, swarm, KV cache, scheduler, skills 7a-7e) + 08.1 (tool_search hardening) + 16 (MCP Sidecar Manager) + 18 (Slice 7e snippet reuse steady-state).

**Restano:** 12 (AG-UI Gateway) → 13 (Channels + Telegram + Multimodal) → 14 (Onboarding + Agent.md) → 15 (Memory Subsystem) → 17 (Packaging & Distribution).

## Quality snapshot (2026-06-06, tutto live-misurato)

| Gate | Valore |
|---|---|
| Coverage owned-surface (full tag matrix) | **86.1%** ≥ 85% |
| golangci-lint | 0 issue |
| CI (push `14d1f23e`) | CI ✓ CodeQL ✓ Skills ✓ |
| Snippet reuse steady-state (CAP-08.1, pagato) | **5 dispatch / 11.06s** (budget ≤6 / <40s; era 21/142.8s authoring) |
| Mutation | skill_write 95.5%; writer_activate 45.2% advisory-accepted (autopsia in docs/aura-quality-snapshot.md) |

## Riprendere — next action

```
/gsd-discuss-phase 12
```

Phase 12 — **AG-UI Gateway**: SSE event protocol transport con boundary `agent ⇸ agui` enforced. Vedi ROADMAP §Phase 12 per goal + requirements (UX-01) + success criteria; dipende da Phase 11 (chiusa).

## Code aperte (non bloccanti)

- **Backlog**: per-request HTTP timeout + 1 retry nel client LLM (network-hang da 186s osservato 2026-06-06, non ricorso)
- **Cosmetic**: warning gsd-tools su CAP-09 traceability (label combinata `CAP-09 / MCP-V2-01` — riga esiste, il parser vuole il label esatto); 2 field-format mismatch STATE.md (`Current Phase Name`/`Last Activity Description`)
- **Igiene repo**: worktree detached `D:/Aura-ci-fe79b33e` (probabile helper CI Codex — verificare prima di rimuovere)
- **Footgun noto**: `make coverage` esegue `TestReset_DownUpRoundTrip` → azzera TUTTI i dati del PG condiviso (by design); dump evidenze in docs/ prima di lanciarlo

## Riferimenti

- PRD: `prd.md` (truth-source; amendment #55 = host-primary snippet posture)
- Roadmap/stato: `.planning/ROADMAP.md` + `.planning/STATE.md`
- Qualità: `docs/aura-quality-snapshot.md` (living doc) + `docs/phase-18-xlsx-call-breakdown.md` (D-03)
