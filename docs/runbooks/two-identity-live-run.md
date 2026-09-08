# Two-identity live run (Phase 01 closing acceptance)

`scripts/musr_live_run.sh` is the harness Phase 01 ("Two Identities, Live and Separated")
closes on: it authenticates as two identities and drives two concurrent, real
`POST /agent/run` conversations against a live `aura serve`, each identity doing the same
three tasks on her own data, deliberately raced so the collision is real rather than
sequential. `scripts/musr_live_run_assert.go` is the blocking half that decides whether the
run passed; the rubric below is recorded as phase evidence and **gates nothing** — see
"What blocks vs what is recorded" below.

## What it needs

Read `scripts/musr_live_run.sh`'s own header comment before running it — it is the primary
source, this document does not repeat its detail. In short: a live, healthy compose stack
(`docker compose ps` — `postgres`, `arcadedb`, `garage` at minimum), `.env` sourced into the
shell with `AURA_PROFILE` set to a strict profile, `AURA_MUSR_ISOLATION=true`, a reachable
`AURA_SANDBOX_IMAGE`, `AURA_ARCADEDB_TENANT_SECRET`, `AURA_AUTHULA_SECRET`, a **real**
`TELEGRAM_BOT_TOKEN` (the provisioning saga mints a genuine deep link — D-08, the one use of
the bot this run makes), `OPENROUTER_API_KEY` (or a local model endpoint the agent can
actually answer from), and `AURA_E2E_AUTHULA_EMAIL`/`AURA_E2E_AUTHULA_PASSWORD` for the
bootstrap `local` operator. Run it from WSL (`go`, `python3` with a real PTY, Docker
reachable) — the primary dev environment per `CLAUDE.md`.

```sh
wsl bash -lc 'cd /mnt/d/Repo/Aura && set -a; source <(awk "{ sub(/\r\$/, \"\"); print }" .env); set +a; bash scripts/musr_live_run.sh'
```

The harness refuses to start — before building the binary, naming every missing
variable — if any precondition is absent. A missing `TELEGRAM_BOT_TOKEN` or
`OPENROUTER_API_KEY` cannot be worked around by the harness itself: neither is something a
script can fabricate, and the harness does not try.

Debug env vars (not part of the acceptance contract): `MUSR_SKIP_CONVERSATIONS=1` stops
after document upload and the ingest wait, printing readiness without spending a model
turn — the gpu_budget dry-run path everything up to the scored conversations should be
exercised through first. `MUSR_DOC_INGEST_WAIT_SEC` overrides the async ingest wait
(default 90s).

## What it produces

One fixed directory, `artifacts/musr-live-run/` (already `.gitignore`d — these are runtime
artifacts, never committed):

| File | Content |
|---|---|
| `transcript-a.jsonl` | Identity A's captured AG-UI SSE frames, one JSON object per line: `{"ts": <unix-epoch-float receive time>, "event": "<AG-UI event type>", "data": <parsed frame body>}`. |
| `transcript-b.jsonl` | The same for identity B. |
| `timings.jsonl` | Both identities' tool-call start/end entries, merged and sorted by `ts`: `{"identity": "a"|"b", "tool": "<name>", "tool_call_id": "<id>", "phase": "start"|"end", "ts": <float>}`. |
| `daemon.log` | The harness's own `aura serve` process log (not the compose service's). |
| `identities.env` | Identity B's UUID, email, and the one Telegram deep link this run mints. |

`scripts/musr_live_run_assert.go --transcripts artifacts/musr-live-run` reads exactly this
directory — the literal string `artifacts/musr-live-run` appears in both the harness and the
assert script, so a producer/consumer path mismatch cannot pass silently against an empty
directory.

## What blocks vs what is recorded

`internal/agenteval/case.go` (lines 14-17) states the position every gate in this repository
has followed since: **there is no LLM judge and no rubric in a gate**, because a check that
needs a model to decide whether it passed cannot be trusted to gate the model. This run does
not amend that position — it splits the evidence in two, cleanly:

- **`scripts/musr_live_run_assert.go` blocks.** Completion, authentication, required tools,
  expected-token, cross-read, and timing-overlap are every one of them a machine check over
  the durable transcript — exact tokens, tool names, timestamps — never natural-language
  matching on an answer. Its exit status is what the harness propagates, and what decides
  whether the phase closes.
- **The rubric below is recorded, never consulted by the gate.** It exists because
  "the machine checks passed" and "this was a *good* two-identity run" are different
  claims, and CLAUDE.md's Definition of Done asks for the second at a >=9.8 bar. A human
  reads both transcripts and scores them; the score and its reasoning land in
  `.planning/phases/01-two-identities-live-and-separated/01-LIVE-RUN-EVIDENCE.md` as phase
  evidence. A run whose assert script exited non-zero is a **failed run** regardless of how
  the prose reads — score it only after confirming a clean assert-script exit.

## The rubric

Score both transcripts against every dimension below, independently, after confirming
`scripts/musr_live_run_assert.go`'s exit was 0. A 9.8 average across all five dimensions,
weighted as shown, is the Definition-of-Done bar. Record the score and the reasoning, not
just the number — a bare number cannot be checked by a second reader.

| Dimension | Weight | What 9.8 means |
|---|---|---|
| Task completion | 30% | All three tasks (document search, memory write, sandbox command) genuinely attempted and completed by BOTH identities, in the SAME turn, with no silent skip and no tool call that errored without the transcript saying so. |
| Answer correctness | 25% | Each identity's final answer states her own seeded token correctly and completely — not paraphrased, not truncated, not confused with the other identity's. |
| Tool-route sanity | 20% | She reached for the right capability for each task — `document_search` for the marker, the memory tool for the write, `shell_exec` for the sandbox command — not merely any tool that happened to work. A correct answer reached by the wrong route (e.g., guessing the token instead of searching for it) scores well below 9.8 here even if `AnswerContains` passes. |
| Isolation legibility | 15% | It is evident from the transcript ALONE, without cross-referencing the other identity's file, that each identity worked on her own data — her own document, her own memory fact, her own sandbox box. No answer references or leaks the other identity's material (this is also machine-checked, but legibility asks whether a human reader would notice a leak unaided). |
| Degradation honesty | 10% | Where anything failed or was uncertain — an empty search result, a tool error, an ambiguous instruction — the transcript says so rather than papering over it with a confident-sounding but unsupported answer. |

Adjust the dimensions or weights only with the reason recorded in the evidence file — this
rubric is the cross-phase scoring instrument every later phase's closing run is measured
with (D-18); changing it later makes earlier scores incomparable, which is a real cost even
though nothing about the change itself is technically irreversible.

## When the score is below 9.8

That is a recorded finding and a follow-up, never a silent pass and never a rewritten
rubric. Fix what the transcripts show is wrong, re-run the harness, and re-score. Do not
edit a dimension's definition to make a transcript clear it — CLAUDE.md's PRD-first
principle applies here exactly as everywhere else: the measurement wins, and if a dimension
turns out to be badly worded, that is itself a recorded finding with its own reasoning, not
a quiet edit.

## What one run does not show

Name at least these when writing the evidence file, per CLAUDE.md's PRD-first
principle ("an amendment declares what it does NOT prove"):

- It samples ONE collision shape (the same three tasks, issued at roughly the same moment).
  It says nothing about a three-way race, a different tool mix racing, or a collision on a
  route this run's three tasks never exercise.
- It runs ONE model on ONE host. Nothing here generalizes to a different model, a different
  host's timing characteristics, or a colder/hotter cache state.
- It says nothing about behaviour under load, under attack, or across a restart — those are
  Phases 3, 4, and 5 respectively.
- Identity B's forced first login is measured to enrol TOTP for real; the forced PASSWORD
  CHANGE half of D-15's own promise has no headless, plan-compliant path in this build
  (measured — see `scripts/musr_live_run.sh`'s header comment for the three independent
  reasons). This run does not exercise it and does not claim to.
