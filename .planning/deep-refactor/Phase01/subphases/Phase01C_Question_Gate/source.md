# Phase01C Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 1C | Question gate requirements | Use PRD gate semantics | Do not make questions UI-only | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-005/006 | Questions as backend primitives | Shared question flow | Telegram-only memory approval | read |
| `D:/Aura/docs/chat-interface-prd.md` | Question state/event contract | Use for event naming and run state | Pulling in unrelated UI scope | pending targeted reread |
| `D:/Aura/internal/agent*`, `D:/Aura/internal/tools` | Risky tool boundaries | Gate before risky actions | Prompt-only approval | pending targeted reread |

## Missing Source Questions

- Existing tool risk/approval signals must be mapped before implementation.
