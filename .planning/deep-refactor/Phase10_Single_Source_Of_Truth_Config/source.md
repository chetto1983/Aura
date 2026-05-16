# Phase10 Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/.env.example` (commented header) | Current bootstrap-only state | Continue narrowing `.env` toward zero | Re-expanding `.env` for non-bootstrap settings | read |
| `D:/Aura/internal/setup/` (first-run wizard) | Wizard already collects setup interactively | Reuse wizard UX; redirect writes from `.env` to SQLite | Inventing a new setup flow | read during US-H04 |
| `D:/Aura/internal/settings/` (existing SQLite store) | Runtime overrides already in SQLite | Same store pattern for secrets (in a separate `secrets` table) | Mixing secrets with non-secret settings | read |
| `D:/Aura/internal/db/migrations/` | Versioned schema migrations exist | Add `secrets` table via next migration | Hand-rolled SQL outside the migration system | read |
| `D:/Aura/compose.yaml` lines 43, 125 | Current `env_file:` references | Drop both after migration; replace with volume mount for `data/aura.db` | Container that can't survive without `.env` injection | read |
| `D:/tmp/nanobot/nanobot/config/schema.py` (AgentDefaults) | Yaml config + no `.env` pattern | Confirm "no env file" is industry-viable | Coupling to nanobot's specific yaml format | read |
| `D:/tmp/cli-printing-press` working dir convention | Single-binary tool with working-dir-relative config | Bootstrap config-from-working-dir pattern | Hidden global state directories | read during Phase10 planning |

## Missing Source Questions

None for the closed US-H source-of-truth config slice. Future secrets hardening
or encryption-at-rest work needs a fresh source map.
