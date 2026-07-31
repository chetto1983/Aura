# NEXT — Aura resume dashboard

> Lightweight resume pointer. **Full source of truth = [.planning/state.md](.planning/state.md)** (GSD-managed) and [.planning/ROADMAP.md](.planning/ROADMAP.md). This file is a hand-maintained dashboard; when it disagrees with `state.md`, `state.md` wins.

_Aggiornato 2026-06-13._

## Stato fasi — 20/23 complete

Tutte le fasi 0–13, 15, 16, 18, 19, 20 sono chiuse (`[x]` in ROADMAP). Restano **due** fasi aperte:

| Fase | Stato | Cosa manca |
|---|---|---|
| **14 — Onboarding + Agent.md** | implementata, **automated-green**, non chiusa | live Telegram operator sign-off + flip ROADMAP `[ ]`→`[x]` + (eventuale) riga quality-snapshot. Vedi `.planning/phases/14-*/14-VALIDATION.md` (`status: automated_passed_manual_pending`). Codice presente: `internal/onboarding/`, `internal/profile/`, telegram `profile_onboarding.go`. |
| **17 — Packaging & Distribution** | **non implementata** (l'ultima fase aperta) | esiste solo `17-SPEC.md` (16/16 acceptance non spuntate). Da fare: fat container image (Go + uvx + npx + mcp-neo4j-cypher), `curl\|sh` installer con secret-gen, `aura doctor`, Caddy, D-22 keyless-boot relaxation. |

## Next action raccomandata

1. Chiudere il **live sign-off di Phase 14** (CDP Telegram harness → `reference_cdp_telegram_live_test_harness.md`), poi flippare ROADMAP + aggiungere la riga UX-05 in `docs/aura-quality-snapshot.md`.
2. `/gsd-spec-phase 17` → plan → execute (Packaging).

## Audit corrente

`docs/audit/` mantiene solo lo stato non risolto corrente: **8 righe**,
di cui **5 vincoli esterni** e **3 follow-up owned**. La release resta
**NO-GO** finché la tabella non è vuota. Dettaglio:
[docs/audit/README.md](docs/audit/README.md).

## Salute repo

- **CI verde**: CI + CodeQL + Skills + `windows-unit` (O-07).
- **Coverage** owned-surface **90.3%** (re-measured 2026-06-13 @ HEAD 882df109 via `make coverage`, full integration matrix su stack live; **ogni package owned ≥85%**, floor `swarm` 85.4%). Campagna coverage 2026-06-13: 16 package sotto-floor portati ≥85% (skilladapters 0→100%, reasoningtrace 71.8→95.8%, cron/handlers 71.1→96.9%, runner 72.4→96.2%, db 76.5→90.2%, onboarding 79→96.8%, …); `-race` pulito (untagged + db tagged).
- Quality snapshot living doc: [docs/aura-quality-snapshot.md](docs/aura-quality-snapshot.md).

## Note igiene (non bloccanti)

- `.planning/state.md` ha un'inconsistenza interna: il frontmatter dice ancora `stopped_at: Phase 08.2 / Current focus: Phase 09`, mentre il corpo (riga ~39) dice correttamente "Phase 17 the last open phase". Il corpo e il conteggio `completed_phases: 20` sono autorevoli.
