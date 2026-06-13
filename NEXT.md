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

## Milestone appena chiusa — audit-closure (2026-06-13)

`docs/audit/`: **38 CLOSED / 0 PARTIAL / 0 OPEN / 2 TRACKED** (R-26, R-40). CodeQL 0 alert aperti. CI completamente verde (CI + CodeQL + Skills + nuova lane `windows-unit`). Dettaglio in [docs/audit/reconciliation-2026-06-13.md](docs/audit/reconciliation-2026-06-13.md).

## Salute repo

- **CI verde**: CI + CodeQL + Skills + `windows-unit` (O-07).
- **Coverage** owned-surface **86.0%** (re-measured 2026-06-13 @ HEAD 19078ff9 via `make coverage`, full integration matrix su stack live; floor ≥85%). Drag principali: `skilladapters` 0.0%, reasoningtrace 71.8%, runner 72.4%, onboarding 79.0%.
- Quality snapshot living doc: [docs/aura-quality-snapshot.md](docs/aura-quality-snapshot.md).

## Note igiene (non bloccanti)

- `.planning/state.md` ha un'inconsistenza interna: il frontmatter dice ancora `stopped_at: Phase 08.2 / Current focus: Phase 09`, mentre il corpo (riga ~39) dice correttamente "Phase 17 the last open phase". Il corpo e il conteggio `completed_phases: 20` sono autorevoli.
