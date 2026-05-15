# Phase01 Source Audit

Status: self-audited scaffold. Source rows name the intended evidence; selected
child phases must be re-audited before implementation.

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 1 through 1C | Phase sequence and gates | Use PRD phases as route | Do not revive old wave order | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-001..ADR-012 | Chat-first, run/event, identity, question, swarm foundations | Preserve accepted architecture decisions | Do not make Telegram or swarm private runtimes | read |
| `D:/Aura/AGENTS.md` | Operating discipline | One bounded slice, known source, known verification | Do not implement from chat memory | read |
| `D:/Aura/docs/chat-interface-prd.md` | Chat Hub contracts, run state, questions | Use as contract source for Phase01A/C | Do not copy stale UI scope into Phase 1 | pending targeted reread |
| `D:/Aura/docs/aura-master-plan.md` | Strategic context and old-wave reconciliation | Use as background | Do not override root PRD when conflicts exist | read |
| `D:/Aura/internal/db/migrations/` | SQLite migration style | Follow existing migration/test pattern | Do not invent separate migration framework | pending targeted reread |
| `D:/Aura/internal/chat/`, `D:/Aura/internal/channels/`, `D:/Aura/internal/telegram/` | Current channel/chat boundary | Preserve current behavior behind tests | Do not migrate live Telegram without fixtures | pending targeted reread |

## Missing Source Questions

- Each selected child phase still needs a fresh focused source audit before
  implementation.
- No online/source verifier ran in this cleanup turn.
