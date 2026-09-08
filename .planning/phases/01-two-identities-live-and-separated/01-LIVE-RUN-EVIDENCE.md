# Phase 01 — Two-Identity Live Run Evidence

**Status: BLOCKED — no scored run exists yet.** This file records what was measured while
building the harness, and names exactly what is missing before a real run can be scored. It
is written honestly per CLAUDE.md's PRD-first principle rather than fabricated to look
complete: **`/gsd-execute-phase` must not report this plan, or this phase, as closed on the
strength of this file.**

- **Commit the harness was built and dry-run-tested against:** `dfd46cc6b44aaa497e5245f43cfa806fa272c643`
- **Date:** 2026-09-08
- **Host:** this repository's WSL environment, against the live Docker Desktop compose stack
  already running on this host (`aura`, `aura-postgres`, `aura-arcadedb`, `aura-garage`, and
  siblings — `docker compose ps` at the time of writing showed all core services healthy).

## What was measured

- The harness (`scripts/musr_live_run.sh`) and the blocking assertion set
  (`scripts/musr_live_run_assert.go`) are both written, committed, and internally verified:
  - The assert script's five fixture pairs (`scripts/testdata/musr_live_run/{clean,
    clean-swapped,leaking,empty,no-overlap}`) pass/fail exactly as designed — see commits
    `6711f253f` (RED) and `69b0caf30` (GREEN) for the measured pass/fail transcript of each.
  - The PTY-driven secret-prompt mechanism (`scripts/musr_live_run_ptyexpect.py`), which
    `aura identity create`'s TTY-only password/security-answer prompts require, was verified
    standalone against a fake interactive prompt (WSL): both secrets delivered correctly,
    output captured, exit code propagated.
  - The harness's precondition gate was run for real against this host's live `.env`: it
    correctly refused **before building the binary**, naming every missing variable —
    `scripts/musr_live_run.sh`'s own acceptance criterion for this behavior is satisfied by a
    real measurement, not by reading the code.
- **The precondition gate found real gaps in this host's current `.env`**, checked via a
  throwaway presence-only diagnostic (reported SET/EMPTY, never a value; not committed):
  `AURA_MUSR_ISOLATION`, `AURA_SANDBOX_IMAGE`, `TELEGRAM_BOT_TOKEN`, and
  `OPENROUTER_API_KEY` are all unset or empty in the root `.env` at the time of this session,
  even though the already-running `aura` compose container is healthy (its process
  environment was populated at its own `docker compose up` time, which can diverge from the
  current `.env` file's contents without a restart).
  - `AURA_MUSR_ISOLATION` and `AURA_SANDBOX_IMAGE` are self-suppliable, non-secret overrides
    for the harness's OWN separately-started `aura serve` process (it never touches the
    compose service or the persisted `.env` file) — `AURA_MUSR_ISOLATION=true` and
    `AURA_SANDBOX_IMAGE=ghcr.io/chetto1983/aura-sandbox:edge` (`.env.example`'s documented
    default) would clear these two without modifying the operator's real deployment
    configuration.
  - `TELEGRAM_BOT_TOKEN` and `OPENROUTER_API_KEY` are real secrets this executor does not
    have and will not fabricate. `aura identity create` genuinely requires a working
    Telegram bot to mint identity B's deep link (D-08); the scored conversations genuinely
    require a real model endpoint. Per this plan's own honesty contract — "if something
    blocks the scored run outright, halt and report rather than substituting a weaker
    proof" — this is exactly that: **halted, not worked around.**

## What is NOT recorded here (because it did not happen)

- No `aura identity create` run against the live stack (no identity B UUID, no real Telegram
  deep link minted).
- No live Authula login for either identity.
- No `POST /agent/run` conversation, for either identity — the machine-checkable assertion
  set has never been run against real transcripts, only against the committed fixtures.
- No measured timing overlap.
- No rubric score. The score rows below are placeholders, not results.

## Rubric score rows (awaiting a real run — NOT scored)

| Dimension | Weight | Score | Reasoning |
|---|---|---|---|
| Task completion | 30% | — | Awaiting a real run. |
| Answer correctness | 25% | — | Awaiting a real run. |
| Tool-route sanity | 20% | — | Awaiting a real run. |
| Isolation legibility | 15% | — | Awaiting a real run. |
| Degradation honesty | 10% | — | Awaiting a real run. |

## What this evidence file does NOT demonstrate (in addition to the blocked status above)

- It does not demonstrate that the harness's untested sections (identity B provisioning
  through TOTP enrollment, thread creation, document upload, the async ingest wait, and the
  two concurrent conversations) work end to end — only that the mechanisms they depend on
  (PTY-driven secret entry, the precondition gate, the assert script) work in isolation.
- It does not demonstrate anything about the forced password-change leg of D-15's first
  login — `scripts/musr_live_run.sh`'s header comment records, with citations, that no
  headless plan-compliant path exists for it in this build.
- It does not demonstrate anything about the real ingest latency
  (`AURA_INGEST_SUPERVISOR_INTERVAL`/`AURA_INGEST_INTERVAL_SEC`) the harness's document-seed
  wait is calibrated against — that wait is a measured accommodation of the compose config's
  own documented cadence, not a value measured against a real ingest cycle on this host.

## Next step

Supply (or point this session at) a real `TELEGRAM_BOT_TOKEN` and `OPENROUTER_API_KEY` (or a
local model endpoint), then re-run:

```sh
wsl bash -lc 'cd /mnt/d/Repo/Aura && set -a; source <(awk "{ sub(/\r\$/, \"\"); print }" .env); set +a; bash scripts/musr_live_run.sh'
```

and replace this file's content with the real measured evidence per
`docs/runbooks/two-identity-live-run.md`.
