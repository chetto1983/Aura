# Phase10 Plan - Single Source of Truth Config

Status: closed 2026-05-15 for the SQLite/secrets source-of-truth config
slice. US-H01..US-H06 shipped.

## Goal

Make SQLite the single source of truth for ALL Aura config. Eliminate `.env`
as a runtime input. The dashboard becomes the only place an operator changes
configuration; first-run setup writes directly to SQLite.

## Scope

- Move secrets (TELEGRAM_TOKEN, LLM_API_KEY, EMBEDDING_API_KEY, GARAGE_S3_*)
  from `.env` into a SQLite `secrets` table (separate from `settings` so
  privacy boundary is explicit).
- Hardcode bootstrap meta-config with env override (DB_PATH=./aura.db,
  HTTP_PORT=127.0.0.1:8080, AURA_HEADLESS=auto-detect, AURA_TIMEZONE=OS).
- Rewrite `internal/setup/` first-run wizard to write to SQLite, not `.env`.
- Update boot loader: open SQLite first, then read tokens from `secrets` table;
  empty token → setup wizard.
- Drop `env_file:` from `compose.yaml`; replace with volume bind for `data/`.
- One-shot migration helper: existing installs with `.env` → import to SQLite
  on first post-upgrade boot, then ignore `.env`.
- Update INSTALL.md + README + .env.example (or delete .env.example).

## Non-Goals

- Do not change the dashboard's existing Settings page UX — only add a
  Secrets page (admin-gated, like SKILLS_ADMIN).
- Do not encrypt the SQLite database at rest in this phase. Secrets table
  uses the same plaintext storage as the rest of `aura.db`; encryption is a
  separate concern documented in INSTALL.md (file-system permissions +
  full-disk encryption recommended).
- Do not break existing `.env`-based deployments without a migration path.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Single source of truth for config | this file | `benchmark.md` | `source.md` | met |
| Secrets in SQLite | this file | `benchmark.md` | `source.md` | met |
| Setup wizard writes SQLite | this file | `benchmark.md` | `source.md` | met |
| Bootstrap meta-config hardcoded | this file | `benchmark.md` | `source.md` | met |
| Migration from `.env` to SQLite | this file | `benchmark.md` | `source.md` | met |
| Docker compose `env_file:` removed | this file | `benchmark.md` | `source.md` | met |

## Implementation Gate

Closed: existing installs have a one-shot `.env` import path, SQLite-backed
secrets are live, setup writes SQLite, and `compose.yaml` has no `env_file:`
directive.

## Story breakdown (Ralph queue candidates)

- US-H01 — `secrets` table schema + repo (migration vN)
- US-H02 — Setup wizard writes to SQLite (replaces `.env` writes)
- US-H03 — Boot loader reads secrets from SQLite before bot start
- US-H04 — Hardcode bootstrap defaults with env override fallback
- US-H05 — Drop `env_file:` from `compose.yaml`, update INSTALL.md
- US-H06 — One-shot `.env` → SQLite migration helper (runs on first boot if
  `.env` exists; logs what was migrated; leaves `.env` in place for operator
  to delete manually).

## Risk

Medium. The setup wizard rewrite touches first-run UX. Migration helper
(US-H06) is essential for existing installs; without it, upgrade is a
breaking change.
