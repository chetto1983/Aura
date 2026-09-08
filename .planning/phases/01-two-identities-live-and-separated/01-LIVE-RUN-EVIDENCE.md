# Phase 01 — Two-Identity Live Run Evidence

**Status: SCORED RUN COMPLETE, machine-checkable half GREEN.** This file records a real run
against the live stack. The rubric score rows are left empty by design — this project runs
`human_verify_mode` at `end-of-phase`; the verifier harvests the scoring instruction from
this plan's Task 3 `<verify><human-check>` block into the phase UAT batch, per D-16 ("read and
score" is the operator's role, not this executor's).

- **Commit the scored run was made on:** `94c38c1b3a009ea02514c7c307c970a39c67db19`
- **Date:** 2026-09-08
- **Host:** WSL, against the live Docker Desktop compose stack already running on this host
  (`aura`, `aura-postgres`, `aura-arcadedb`, `aura-garage`, and siblings — the compose `aura`
  service was left untouched throughout; this run's `aura serve` was a separate process).

## The run

```
==> preconditions OK
==> identity B provisioned: 6d0ebdee-bf43-4f5b-abf6-469310d52a5a
==> Telegram deep link (D-08, minted once): https://t.me/DavMar1983_Bot?start=e3be9af2-8042-4edf-9937-9563e94bdaa1
==> identity A authenticated
==> identity B first login OK (no forced redirect exists to wait for — measured; see header comment)
==> identity B TOTP enrollment complete (the mandatory leg of D-15's first login)
==> identity B: no headless password-change path exists in this build (measured — see header comment); not exercised, recorded honestly
==> threads created: A=01a0812f-91bf-751c-a8c9-8ef090f68f2a B=01a0812f-9212-7c3b-aca3-33882e8c187a
==> seed documents uploaded: A token=MUSR-A-3372d56a B token=MUSR-B-ac7bfb9d
==> starting two concurrent /agent/run conversations (released together)
musr_live_run[b]: terminal event = RUN_FINISHED
musr_live_run[a]: terminal event = RUN_FINISHED
==> conversation A exit=0 conversation B exit=0
==> running the blocking assertion set
musr_live_run_assert: OK — both identities completed, required tools fired, tokens separated, timings overlap (artifacts/musr-live-run)
```

- **Identity A** (bootstrap operator, dvdmarchetto@gmail.com): `bb78065b-0fc2-4c02-b4b7-b9aeceed1511`.
- **Identity B** (provisioned by this run, via `aura identity create`): `6d0ebdee-bf43-4f5b-abf6-469310d52a5a`,
  email `musr-live-run-b-1788873764@example.invalid`.
- **Telegram deep link minted (D-08, the one real use of the bot):**
  `https://t.me/DavMar1983_Bot?start=e3be9af2-8042-4edf-9937-9563e94bdaa1`.
- **Run artifacts** (uncommitted by design — `artifacts/*` is gitignored; this file is where
  their existence and content are attested): `artifacts/musr-live-run/transcript-a.jsonl`
  (972 lines), `transcript-b.jsonl` (2655 lines), `timings.jsonl` (20 entries), `daemon.log`,
  `identities.env`.

## Per-assertion verdict (the blocking half, D-18)

All six passed on `go run ./scripts/musr_live_run_assert.go --transcripts artifacts/musr-live-run`:

| Assertion | Verdict |
|---|---|
| Completion (both transcripts end `RUN_FINISHED`) | PASS |
| Authentication (both open `RUN_STARTED`) | PASS |
| Required tools — identity A (`document_search`, `memory__memory_upsert_fact`, `shell_exec`) | PASS |
| Required tools — identity B (same three) | PASS |
| Expected token — each identity's answer carries her own `MUSR-<label>-<hex>` marker | PASS |
| Cross-read — neither answer carries the other identity's token | PASS |
| Timing overlap — at least one genuine `[start,end]` interval shared between A and B | PASS |

Final answers, extracted from the transcripts:
- **Identity A:** `MUSR-A-3372d56a` — tool sequence: `document_search`, `skill`, `tool_search`
  (deferred-tool loading), `memory__memory_upsert_fact`, `shell_exec`.
- **Identity B:** `Il codice trovato è MUSR-B-ac7bfb9d.` — tool sequence: `document_search`,
  `skill`, `memory__memory_upsert_fact`, `shell_exec`, `memory__memory_upsert_fact`.

## Measured timing overlap

The sandbox task's shell command was widened to `sleep 3 && echo <code>` (costs no LLM/GPU
time) precisely because the FIRST real run — otherwise fully passing — showed genuine
interleaved progress but every individual tool call was sub-second, so no `[start,end]`
window literally overlapped (recorded below, not hidden). The second run's `timings.jsonl`
shows real overlap, e.g.:

- Identity B's `shell_exec` (`call_4pal7347`): `2026-09-08T13:24:33.078Z` – `13:24:36.761Z`.
- Identity A's `skill`/`tool_search` calls fall inside that window:
  `13:24:36.036Z` – `13:24:36.087Z`.
- Identity A's `memory__memory_upsert_fact`/`shell_exec` (`13:24:39.361Z` – `13:24:42.933Z`)
  overlap identity B's second `memory__memory_upsert_fact`
  (`13:24:41.968Z` – `13:24:42.001Z`), fully contained inside A's window.

## First (non-scored) run — recorded honestly, not discarded

Before the widened sandbox command, an earlier real run completed both conversations
correctly (right tokens, required tools, no cross-read) and failed **only** the overlap
assertion. Per this plan's honesty contract that result is recorded, not hidden: the
machine-checkable half is pass/fail as a whole, and that run's assert-script exit was
non-zero. It is not counted as the scored run.

An even earlier attempt failed outright at `POST /agent/run` with `403 forbidden` for
identity B — she had zero capabilities (a freshly provisioned identity has none by default;
`agent.run`, the capability `POST /agent/run` itself requires, must be granted explicitly at
create time). Fixed by passing `-capability agent.run` to `aura identity create`; recorded in
the harness fix commit, not silently retried away.

## Rubric score rows (awaiting the end-of-phase UAT batch — NOT scored here)

| Dimension | Weight | Score | Reasoning |
|---|---|---|---|
| Task completion | 30% | — | Awaiting the operator's read of both transcripts (Task 3 `<verify><human-check>`). |
| Answer correctness | 25% | — | Awaiting. |
| Tool-route sanity | 20% | — | Awaiting. |
| Isolation legibility | 15% | — | Awaiting. |
| Degradation honesty | 10% | — | Awaiting. |

Score only after confirming the assert-script exit above was 0 for the run being scored
(it was, for the run this file records) — a failing machine check is a failed run whatever
the prose reads like, per `docs/runbooks/two-identity-live-run.md`.

## Accumulated test debris (disclosed, not hidden)

Debugging this harness against the live stack required many iterations before the first
clean pass; each dry run and failed real-run attempt provisioned its own identity B (`aura
identity create` has no delete/deprovision verb — see `scripts/musr_live_run.sh`'s own
comment on this). Twelve `musr-live-run-b-*@example.invalid` identities exist in
`aura.identities` on this host as of this session, one of which (`6d0ebdee-...`) is the
identity this evidence file scores; the other eleven are inert leftovers from earlier
iterations while diagnosing (in order) the settings-store precondition gap, the
`host.docker.internal` WSL routing gap, the ArcadeDB/Garage bare-process endpoint gap, the
wrong default `-operator`, the cookie-jar merge bug, the session-renewal staleness bug, and
the missing `agent.run` capability. None of them hold data beyond what their own aborted run
seeded (their own marker document, at most). This is recorded here as a real, measured cost
of building this harness against a live deployment rather than cleaned up silently — no CLI
exists to remove them (see the harness's own comment on this), and this executor will not
invent an undocumented one.

## What this run does NOT demonstrate

Per CLAUDE.md's PRD-first principle:

- It samples ONE collision shape — the same three tasks, issued at roughly the same moment,
  widened by a fixed `sleep 3` in the sandbox command specifically to make the overlap
  measurable. It says nothing about a three-way race, a different tool mix racing, or a
  collision on a route this run's three tasks never exercise.
- It runs ONE model (`gemma4:31b-cloud`, an Ollama-proxied cloud model, reached via
  `host.docker.internal` through the WSL default-gateway remap this harness's `aura serve`
  process uses) on ONE host. Nothing here generalizes to a different model, a different
  host's timing characteristics, or a colder/hotter cache state.
- It says nothing about behaviour under load, under attack, or across a restart — those are
  Phases 3, 4, and 5 respectively.
- Identity B's forced first login is measured to enrol TOTP for real (including a genuine
  production wiring fix this run's own dry-running discovered and repaired — see
  `internal/webauth/authula.go`'s commit). The forced PASSWORD CHANGE half of D-15's own
  promise has no headless, plan-compliant path in this build (measured — see
  `scripts/musr_live_run.sh`'s header comment for the three independent reasons: no mailer
  plugin wired, Aura's own security-question reset requires a completed Telegram link this
  harness will not fake, no admin plugin wired). This run does not exercise it and does not
  claim to.
- The 90-second document-ingest wait is a measured accommodation of the compose config's own
  documented cadence (`AURA_INGEST_SUPERVISOR_INTERVAL`=15s + `AURA_INGEST_INTERVAL_SEC`=60s),
  not a value independently measured against a real ingest cycle's actual latency on this
  host — both real runs' `document_search` calls succeeded well inside that wait, which is
  consistent with, but does not prove, a tighter bound.
