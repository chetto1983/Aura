---
phase: 52
slug: mid-turn-steering
status: validated
nyquist_compliant: false
wall_clock_score: 9.0
created: 2026-08-26
---

# Phase 52 — Validation Evidence Record (Gate 3 / Definition of Done)

> No RESEARCH.md preceded this phase, so this is the phase's first and only validation
> artifact — created retroactively by 52-08 rather than seeded before execution. Its
> Manual-Only / Not-Proven table is load-bearing, not decorative (CLAUDE.md ACC-01: a
> green suite closes nothing).

## Reading rule, stated before any number (T-52-56)

**STEER-02's pass/fail gate is the step CEILING and the wallclock DEADLINE, not consumption.**
Both are process-wide constants (`AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`,
confirmed in `.env` and `internal/agent/budget.go:29-41`), computed once per run from the same
running server process and never mutated by the steer drain code (`internal/agent/llm_agent_steer.go`,
119 LOC, read in full — no call into `Budget.SetMaxSteps` or any deadline mutation). A moved
ceiling or deadline is the only failure condition. Consumption (rounds, tool calls, wallclock
elapsed) is reported as **corroboration only**, with this tie-breaker fixed in advance: a
steered run consuming MORE is not a violation if the ceiling/deadline did not move — it is the
operator getting the redirect they asked for.

## Container freshness and live model (T-52-55)

- **Live model**: `aura.settings` (queried live) → `AURA_LLM_MODEL = deepseek/deepseek-v4-flash-0731:nitro`, `AURA_MODEL_CONTEXT_WINDOW = 1000000`. Every SSE/DB observation below is against this OpenRouter model, not a mock.
- **Image freshness (with an honest gap named)**: `aura:local` was rebuilt at `2026-08-25T23:54:21Z` (`docker inspect aura:local`), and the running container `aura` started at `2026-08-25T23:54:34Z` (`docker inspect aura --format '{{.State.StartedAt}}'`) — after commit `99c07b5ba` (23:52:22Z, the RESUME-01 500→400 fix) and after every 52-07 commit (last at 22:51:46Z, `3cbc7edcf5`). No runtime-affecting commit landed after this container started — every later commit in this session is test-only (`inbox_test.go`) or docs-only (`prd.md`, `docs/aura-quality-snapshot.md`, `.planning/*`). **Gap**: the earliest backend tests (the A/B pair, the leftover/terminal-refusal cases, and the first RESUME-01 attempt — 23:31:59Z to ~23:49:20Z) ran against the PRIOR container incarnation, whose exact build commit was not verified before those tests began, a process miss against this plan's own "verify BEFORE anything else" instruction. This is not blind: the RESUME-01 500 (pre-fix) was observed in that exact window, which is itself evidence the prior container was running genuinely pre-fix code rather than a wrong/broken image, and every backend steer code path those tests exercised (inbox, both drain points, `aura.steer` persistence) was already complete since 52-04/52-05 (last commit `92c8cec76`, 20:14:30Z — over 3 hours earlier). The live browser Playwright session (see SC#1/#2) ran after the 23:54 rebuild and is unaffected.

---

## Success Criteria — evidence

Each row below names an observable (status code, DB row, SSE frame, browser assertion), never a
test name.

### SC#1 — Typing a redirect while Aura is mid-task changes what she does next, live, no tool killed

**Cockpit, backend (curl)**: conversation `01a03b44-4ac6-7925-a558-7409abc0ac26` (run2). Scenario:
shell_exec `sleep 8 && echo pronto`, then web_search "previsioni meteo Roma", then a one-sentence
answer. Steer POSTed at `2026-08-25T23:34:47.282Z` (17s into the run, after `shell_exec` had
already finished): `POST /agent/runs/{runID}/steer {"text":"CAMBIO ISTRUZIONI: non cercare piu il
meteo. Invece usa web_search per cercare \"prezzo Bitcoin oggi in euro\"..."}`  → **HTTP 202
`{"status":"queued"}`**. The `aura.steer` CUSTOM frame confirms `delivery: "tool_result_append"`,
`round: 2`. The subsequent `web_search` TOOL_CALL_ARGS carries `"Bitcoin"` (not `"meteo"`), and the
final TEXT_MESSAGE reads *"Il prezzo del Bitcoin oggi è di circa 67.600 €, secondo CoinMarketCap
[1]."* — the baseline run (`01a03b44-2da8-7afd-8980-c22def15970d`, unsteered, identical opening
prompt) answered *"...prevede per oggi a Roma cielo poco nuvoloso..."* (the weather). **No tool
killed**: both runs' `shell_exec` TOOL_CALL_RESULT show `{"exit_code":0,"timed_out":false}` with
`pronto` printed — the in-flight tool ran to completion before the steer was even dispatched.

**Cockpit, live browser (Playwright against the running stack, not CI)**: a throwaway spec
(`web/e2e/_tmp-52-08-steer-live.spec.ts`, deleted after use — not a permanent test) drove a real
Chromium session: logged in via `gotoAuthenticated`, created conversation
`01a03bca-06ca-75bd-a462-4ea980faad43`, typed the scenario into the REAL composer textbox
(`getByTestId('chat-composer').getByRole('textbox')`), pressed Enter, observed the real
`POST /agent/run`. While the stop control was visible (run in flight), typed the redirect into the
SAME textbox — **the composer accepted the input, no dead field** — and clicked the dedicated
"Redirect the current turn" button (`aria-label="Reindirizza il turno in corso"` / EN
`"Redirect the current turn"`, `chat.steer.sendAria`). Observed live:
```
STEER_HTTP_STATUS 202
STEER_HTTP_BODY {"status":"queued"}
NOTICE_TEXT Message sent — the turn was redirected.
RUN_FINISHED: stop control hidden again
RELOAD_SHOWS_STEER_TEXT: true
```
Persisted transcript for this conversation (`aura.conversation_turns`, RLS-scoped query):
```
seq | role      | content (truncated)
1   | user      | Esegui questi passaggi ESATTAMENTE in ordine...
2   | assistant | (tool call)
3   | tool      | pronto [aura_shell exit_code=0 duration_ms=10060 timed_out=false]
4   | user      | CAMBIO ISTRUZIONI: non cercare piu il meteo...
5   | assistant | (tool call)
6   | tool      | ## web_search ...
7   | assistant | (tool call)
8   | tool      | {"results":[{"title":"Cambio Bitcoin Euro..."
9   | assistant | Il prezzo del Bitcoin oggi è di circa 67.556 €...
```
The steer landed at seq 4, immediately after the completed shell_exec, and the final answer (seq 9)
reflects the redirect. The test conversation was deleted afterward (`DELETE /api/conversations/...`
→ 204) to keep the operator's live data clean.

**Telegram**: NOT proven this session — see Manual-Only table.

**Verdict: MET (cockpit, both backend and browser).**

### SC#2 — The steer appears in the persisted conversation at the point it landed, not appended at the end

`aura.conversation_turns` for `01a03b44-4ac6-7925-a558-7409abc0ac26` (the curl-driven run):
```
seq | role      | content (truncated)
1   | user      | Esegui questi passaggi...
2   | assistant | (tool call: shell_exec)
3   | tool      | pronto [...]
4   | assistant | (tool call: tool_search)
5   | tool      | ## web_search ...
6   | user      | CAMBIO ISTRUZIONI: non cercare piu il meteo... [msg-6]
7   | assistant | (tool call: web_search)
8   | tool      | {"results":[{"title":"Bitcoin (BTC)...
9   | assistant | Il prezzo del Bitcoin oggi è di circa 67.600 €...
```
The steer (msg-6 of 9) sits between the `tool_search` result and the redirected `web_search` call —
not appended after msg-9 (the final answer). **Resume/replay leg (STEER-03's third leg,
`GET /agent/runs/{runID}/events` with `Last-Event-ID`)**: `resume_before.sse` (fetched from event id
464) starts with the `aura.steer` CUSTOM frame itself; `resume_after.sse` (fetched from id 465,
one past the steer) omits it and starts at the next `REASONING_START` — the replay ring places the
steer frame in sequence, exactly where a client reconnecting mid-stream would expect it.
**Reload leg**: the live browser session reloaded the page and asserted the redirect text visible
in the transcript (`RELOAD_SHOWS_STEER_TEXT: true`, see SC#1).

**Verdict: MET.**

### SC#3 — Steering a run that has just finished is delivered automatically as the next user turn, with a visible line, never silently swallowed

Conversation `01a03b44-4c68-74e6-bb3e-823c35feaa88`: opening prompt "qual e la capitale della
Francia?" (no tools, answered "Parigi" in round 1). A steer was POSTed at `23:37:03Z`
(`{"text":"messaggio in ritardo: raccontami una barzelletta breve"}`) — both drain points had
already been passed (no tool calls exist in this scenario to hold drain-B open, and drain-A fires
essentially synchronously at round start). Response: **HTTP 202 `{"status":"queued"}`**. The
`aura.steer` CUSTOM frame in the SSE stream shows `delivery: "auto_delivery_next_turn"`. The run's
own SSE stream continued and produced a SECOND assistant TEXT_MESSAGE (the joke). Persisted
evidence, `aura.conversation_turns`:
```
seq | role      | content
1   | user      | Senza usare nessuno strumento, rispondimi SOLO con una parola: qual e la capitale della Francia?
2   | assistant | Parigi
3   | user      | The previous turn ended before this message could be delivered, so it is being sent now as a new message:
    |           | messaggio in ritardo: raccontami una barzelletta breve
4   | assistant | Perché i programmatori confondono Halloween e Natale? Perché OCT 31 è uguale a DEC 25. 🎃→🎄
```
Row 3's content is the byte-exact `steerAutoDeliveryNotice` string from `52-05-SUMMARY.md`, followed
by the leftover text — this is the "visible line," and it is genuinely visible (not swallowed): row
4 is the assistant's actual answer to the leftover request.

**COUNT query (T-52-58, the double-write guard):**
```sql
SELECT count(*) FROM aura.conversation_turns
WHERE conversation_id = '01a03b44-4c68-74e6-bb3e-823c35feaa88'
  AND content LIKE 'The previous turn ended before this message could be delivered%';
```
**Result: `1`.** Not 2 — the drain-time branch and the follow-on turn's own persistence did not
double-write.

**Verdict: MET.**

### SC#4 — A steered turn consumes no more steps or wallclock than an unsteered one (D-13 A/B)

See the reading rule at the top of this document. **The ceiling and deadline did not move** — they
are the same process-wide constants (`AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`)
for both runs, and the steer drain code touches neither. Consumption, reported as corroboration:

| | Baseline (unsteered) | Steered | Delta |
|---|---|---|---|
| Conversation id | `01a03b44-2da8-7afd-8980-c22def15970d` | `01a03b44-4ac6-7925-a558-7409abc0ac26` | — |
| Step ceiling (`AURA_LOOP_MAX_STEPS`) | 25 | 25 | **0 — unmoved** |
| Wallclock deadline (`AURA_LOOP_MAX_WALLCLOCK_SEC`) | 300s | 300s | **0 — unmoved** |
| Rounds consumed (`REASONING_START` count) | 4 | 4 | 0 |
| Tool calls consumed (`TOOL_CALL_START` count) | 3 (shell_exec, tool_search, web_search) | 3 (shell_exec, tool_search, web_search) | 0 |
| Wallclock elapsed (POST → stream close, wall-clock timestamps) | 66s (`23:32:38Z`→`23:33:44Z`) | 59s (`23:34:30Z`→`23:35:29Z`) | −7s (steered run was FASTER) |

No extra round was needed — the redirect replaced the second tool call's query in place (same
round, same tool-call slot), so there is no "what did the extra round do" case to explain here: the
steered run did not consume more of anything, only different content within the identical
structural budget. STEER-02's own prohibition against explaining away a legitimate extra round does
not need to be invoked because none occurred.

**Verdict: MET — ceiling/deadline unmoved (the gate), consumption identical or better (the
corroboration).**

### SC#5 — The same steer works from a channel, not only the cockpit

**Cockpit leg**: proven above (SC#1/#2), at both the backend and the live-browser level.

**Telegram leg: NOT PROVEN this session.** See Manual-Only table below for the full reasoning. The
wiring is asserted by 52-06's realistic unit tests (`internal/channels/telegram/bot_dispatch_steer_test.go`,
`bot_dispatch_queue_test.go`, `cmd/aura/steer_wiring_test.go` — all `human_judgment: false`), which
per ACC-01 is not sufficient on its own ("a green suite alone is not evidence a requirement is
met"). No real Telegram round-trip was exercised this session.

**Verdict: PARTIALLY MET — cockpit leg live-proven twice (backend + browser); channel-diversity leg
(Telegram) not live-proven. This is the one criterion this phase does not close on live evidence
alone.**

---

## RESUME-01 (folded from PRD amendment #133) — live evidence

**400, empty accept (bug found and fixed).** `POST /api/approvals/{token}/resolve` with
`{"action":"accept","content":""}` against the real pending clarification pause
(`01a03b52-40b9-754c-b634-b616bfd7003b`, from conversation `01a03b44-4d39-7be5-bd37-f26e414889bf`'s
`ask_user` interrupt) returned:
- **Before the fix** (`23:49:20Z`): `HTTP 500 Internal Server Error` — `submit answer: invalid resume
  answer: accepted content must not be empty`. `handleResolveApproval` in
  `internal/agui/approvals_api.go` mapped `ErrPauseExpired`/`ErrPauseNotFound`/
  `ErrResumeDecisionNotAllowed` explicitly but fell through to a generic 500 for
  `askuser.ErrInvalidAnswer` — a real bug, found by exercising exactly this defect live, not by
  reading the code first.
- **Fix** (commit `99c07b5ba`): added the missing `errors.Is(err, askuser.ErrInvalidAnswer)` branch
  → 400. Added `TestApprovalsAPI_ResolveInvalidAnswerReturns400`. Rebuilt the image.
- **After the fix** (`23:54:56Z`, same token, re-attempted): `HTTP 400 Bad Request` — `invalid resume
  answer: submit answer: invalid resume answer: accepted content must not be empty`.

**403, decision the pause's policy does not permit.** Seeded a real `aura.paused_states` row
(`53e7339b-1df7-4510-9081-8e141a30a7e8`, `kind='approval'`, `resume_context = {"allowed_decisions":
["accept"]}`) via direct SQL against the live conversation, then exercised the real HTTP route:
`POST /api/approvals/53e7339b.../resolve {"action":"decline",...}` → **HTTP 403 Forbidden** —
`approval decision not allowed`.

**TTL expiry resolves as an expiry, never as a yes.** Seeded a real pause
(`e1f12f8b-98c9-40c7-81b0-a3c1768b8c95`, `kind='approval'`, `created_at = now() - interval '3 days'`,
older than the 48h default TTL) and let the REAL periodic sweep (60s interval,
`min(ttl/2, 60s)` per `cmd/aura/approval_expiry.go`) run — no config override, no restart. Observed
in `aura.paused_states` after the sweep:
```
token: e1f12f8b-98c9-40c7-81b0-a3c1768b8c95
resumed_at: 2026-08-26 00:00:41.530197+00
resumed_answer: {"action": "expired", "content": "approval expired before a decision was made"}
```
`resumed_answer.action = "expired"`, never `"accept"`. A subsequent POST attempt against the same
now-expired token (`00:00:59Z`) correctly returned **HTTP 410 Gone** — `approval expired`.

**Cleanup**: both seeded pauses (`53e7339b...` and `01a03b52...`) and all 8 throwaway conversations
created this session were resolved/deleted afterward — the operator's live `/api/approvals` queue
and conversation list are clean, no orphaned test data.

**Verdict: MET — all three folded defects exercised live; one real bug found and fixed in the
process.**

---

## Delivery branches (T-52-56/exercise-both requirement)

- **`tool_result_append` (the ratified primary)**: proven live — the `aura.steer` CUSTOM frame for
  conversation `01a03b44-4ac6-7925-a558-7409abc0ac26` explicitly carries `"delivery":
  "tool_result_append"`, against the real OpenRouter provider (`deepseek/deepseek-v4-flash-0731:nitro`),
  not a fake client. This closes the half of amendment #132's concern that asked whether the primary
  delivery shape survives a real provider round-trip — it does.
- **`user_message_fallback` (the plain-`user`-message path)**: **NOT exercised live this session.**
  Reproducing it requires landing a steer while the conversation's history tail is NOT a tool
  result — i.e., between drain point A (pre-API-call) and any tool dispatch. Every attempt to win
  this race (posting the steer immediately after observing `RUN_STARTED`) instead landed as
  `auto_delivery_next_turn`, because drain-A fires essentially synchronously at round start,
  faster than an external HTTP round-trip can react. This matches 52-04-SUMMARY.md's own prior
  finding: this branch has only ever been proven against a fake LLM client in a unit test, never
  against Aura's real provider path. Recorded as a finding per the plan's own instruction, not
  papered over as a flaky run.

---

## FA-1 through FA-5 (register from `52-02-PLAN.md`) — dispositions

| ID | Requirement | Disposition |
|----|-------------|-------------|
| FA-1 | STEER-02 | **CLOSED.** The live A/B above (baseline vs. cockpit-steered, both backend-driven) is exactly the evidence FA-1 said was missing until 52-08. Ceiling and deadline unmoved; consumption identical. |
| FA-2 | STEER-03 | **CLOSED (owned by 52-04, corroborated live here).** 52-04's own test asserted the drain-point `seq` invariant against a real multi-round turn. This session's live evidence (two independent conversations, both showing the steer positioned mid-sequence, plus the Last-Event-ID resume proof) corroborates it against a real provider, not just the test. |
| FA-3 | STEER-04 | **CLOSED (owned by 52-05, corroborated live here).** Both named cases — the 410-on-terminal (see Manual-Only: exercised but not captured with a fresh live artifact this pass, see note) and the leftover auto-delivery (proven above with a COUNT=1) — were exercised. No third, unnamed case (neither delivered nor named) surfaced. |
| FA-4 | STEER-05 | **STILL PARTIALLY OPEN.** 52-06 asserted `convID(chatID) == ThreadID` with a discriminating unit test (`TestOneInboxServesBothSurfaces`). This session did NOT live-corroborate it against a real Telegram round-trip — see the Manual-Only table. The assumption is unit-closed, not live-closed. |
| FA-5 | STEER-06 | **STAYS OPEN, unchanged.** This session did not perform a fresh exhaustive re-read of amendment #132 against the reference implementations; no new contradiction was found, but none was actively sought either. Recorded so the gap does not silently disappear. |

**Terminal refusal (410) note**: this was exercised live earlier in the session
(`terminal_refusal_headers.txt`: `HTTP/1.1 410 Gone`, body `run has ended: message was not queued;
send it as a normal turn`, against a genuinely-finished run's id) but the captured evidence file
does not preserve which specific run id was targeted with the same rigor as the other cases; the
behavior itself (410 with an actionable body) is confirmed, and FA-3's disposition reflects that.

---

## Manual-Only / Not-Proven table

| Claim | Requirement | Why not proven this session | What would prove it |
|---|---|---|---|
| A plain-text Telegram message redirects a live turn, with the exact `turnSteeredMessage` echo | STEER-05 / SC#1 / SC#5 | The bot uses long-polling `getUpdates` against the real Telegram Bot API (`internal/channels/telegram/bot.go`); no local-bot-api sidecar is running, no Telethon/Pyrogram session, no `API_ID`/`API_HASH` in `.env`. There is no way to script an inbound message as a real Telegram user from this environment. | A human, using their own Telegram client, sends a plain-text message to the bot while a turn is visibly running, and reports the reply and the next round's behavior. |
| A photo sent to Telegram during a live turn is queued (`turnQueuedForNextTurnMessage`) and its own turn actually runs afterward | STEER-05 (D-05 media queue) | Same structural limitation — requires a real Telegram client sending a real photo. | A human sends a photo mid-turn from their phone; confirm the queued reply, then confirm a SECOND turn actually executes afterward (a conversation-turn row, not just a reply). |
| `/cancel`-ing a live turn with a queued photo announces `turnQueuedNotDeliveredMessage` rather than silence | STEER-05 (D-05) | Same structural limitation. | A human repeats the photo case and issues `/cancel` before the live turn ends; confirm the queued-not-delivered reply arrives. |
| The `user_message_fallback` steer delivery branch survives a real OpenRouter round-trip | STEER-03 (delivery-branch coverage) | Every attempt to land a steer while the history tail is not a tool result raced against drain-point A and lost, landing as `auto_delivery_next_turn` instead. | Instrument a deliberate delay in drain-point A (test-only build tag) or find a scenario shape where the tail is reliably a plain-assistant message when the steer lands, then repeat against the real provider. |
| The cockpit's `SteerNotice` refusal variants (`invalid`, `busy`, `ended`, `failed`) render correctly in the browser | STEER-04 (UI-level) | This session drove only the success path (`redirected`) live in the browser; the 410/429/400 refusal strings were only confirmed at the HTTP layer (curl), not rendered and read from the DOM. | Repeat the live-browser session, additionally POSTing a steer at a terminal run and reading the `role="status"` refusal text from the page. |
| The exhaustive re-read of amendment #132 against the reference implementations found no further contradiction (FA-5) | STEER-06 | Out of scope for a live-E2E validation pass; no fresh reading was performed. | A dedicated re-read pass, as 52-01 originally did, checking every clause of #132 against the current `internal/agui`/`internal/steer` tree. |

**A note on the flaky test found, not fixed**: a full-suite `vitest` run showed one failure in
`AppShell.shell.test.tsx` under resource contention; the same file re-run in isolation passed
17/17. A fresh full-suite re-run for this document's own coverage measurement (below) passed
cleanly (exit code 0). Per the deviation rules' scope boundary, this pre-existing flake — unrelated
to any file this phase touched — was not fixed; logged here rather than silently ignored.

---

## Score

| Success Criterion | Score (of 2.0) | Basis |
|---|---|---|
| SC#1 — redirect changes behavior live, no tool killed | 2.0 | Cockpit backend + cockpit browser, both proven |
| SC#2 — persisted at landed position | 2.0 | DB position + Last-Event-ID resume + browser reload |
| SC#3 — leftover auto-delivered with visible line | 2.0 | COUNT=1, exact notice string |
| SC#4 — no budget extension | 2.0 | Ceiling/deadline unmoved (structural), consumption identical |
| SC#5 — works from a channel, not only cockpit | 1.0 | Cockpit leg proven; Telegram leg not live-proven this session |

**Total: 9.0 / 10.**

**This is below the 9.8 bar. The phase does not close this session.** The gap is specific and
named, not diffuse: SC#5's Telegram leg (and, downstream of it, STEER-05's full closure and FA-4)
require a human physically sending a message from their own Telegram account while a turn is live —
that is not a code defect, it is a channel this autonomous session structurally cannot drive. Every
other success criterion, and RESUME-01 in full (with a real bug found and fixed along the way), is
proven live against the running stack with database-level and browser-level evidence, not test
output.

**Recommended closing action**: a short human-in-the-loop session — send one plain-text redirect and
one photo to the bot during a live turn, and one `/cancel` — would close SC#5, STEER-05, and FA-4 in
full. Everything else in this phase is ready to close as-is.

---

## Gate re-measurement (Task 2) — numbers produced by this tree today

All commands run per CLAUDE.md's "Quality tooling & gates" (WSL, `~/.local/bin:~/go/bin` on PATH,
composed DSNs exported). Logs preserved at the paths noted.

| Gate | Result | Evidence |
|---|---|---|
| `go vet ./...` | clean, exit 0 | run on current tree (HEAD `bbbcea04d`) |
| `go build ./...` | clean, exit 0 | run on current tree |
| `go test -race ./...` | **73 packages, 0 failures** | `/tmp/race_test.log` (WSL) |
| `govulncheck ./...` | **0 vulnerabilities** in code or imported packages (3 in required-but-uncalled modules) | `/tmp/govulncheck.log` |
| `bash scripts/coverage_docker.sh` (owned-surface, `db_integration`, disposable `aura_cov`) | **86.3% (27069/31384)**, ≥85% floor | `/tmp/coverage_docker.log` |
| Per-package coverage, this run | `internal/agui` 85.5% (4251/4971); see below for the others (filtered profile, same run) | `cover_gate.out.filtered` (WSL, gitignored) |
| `npm run test` (vitest + coverage), fresh run | **exit 0**, all tests green | statements 91.16% (7202/7900), branches 85.19% (4887/5736), functions 90.41% (2056/2274), lines 92.96% (6578/7076) |
| `npm run mutation` (Stryker), persisted report from this session | **74.64%** killed (1928/2583; break threshold 70%) | `web/reports/mutation/mutation.json` — Phase 52 files are not in Stryker's fixed `mutate` list, so this is the pre-existing baseline, not a Phase-52-specific figure |
| `make web-freshness` equivalent (Docker webbuild stage, Linux Node 24, since no native Linux Node host is available here) | `diff -rq` against committed `internal/webui/dist`: **zero differences** | fresh `docker build --target webbuild --no-cache` this session, extracted and diffed byte-for-byte |
| Go mutation, `internal/steer/inbox.go` | **100% (25/25 killed, 0 survived)**, in a throwaway worktree (`/tmp/aura-mutation-wt-52-08`, removed after) | fixed a real mutation gap this session (52%→84%→100%) by adding hardcoded-literal boundary tests that don't move in lock-step with the mutated production constant |
| Go mutation, `internal/agent/llm_agent_steer.go` | **100% (34/34 killed, 0 survived)** | no fix needed |
| Go mutation, `internal/channels/telegram/bot_dispatch_queue.go` | **74.07% (20/27 killed)**, above the 70% floor | accepted as-is to preserve budget, per the plan's own ">= 70%" bar |
| `AURA_QUALITY_CHANGED_FILES=... bash scripts/quality_snapshot_gate.sh` | `ok: quality snapshot gate checked 1 row(s) against base date 2026-08-25` | re-run after the re-attestation edit below |
| File sizes, every `.go` file this phase touched | `approvals_api.go` 328, `approvals_api_unit_test.go` 365, `internal/steer/inbox_test.go` 354 — all ≤600 | `wc -l` |

**Per-package coverage (this run, filtered `db_integration` profile)**: `internal/agui` 85.5%
(4251/4971, re-attested in `docs/aura-quality-snapshot.md` this session). The plan additionally
asks for `internal/steer`, `internal/agent`, `internal/runner`, `internal/askuser`,
`internal/channels/telegram`, `internal/config` — those were spot-measured earlier in this session
from the same coverage run (steer 92.0%, agent 88.5%, runner 90.4%, askuser 78.3%, telegram 88.0%,
config 84.2%); the `internal/agui` figure above is the one independently re-derived and re-verified
for this document via `awk` against the raw profile, since it is the file this phase's own bug fix
touched.

**Quality snapshot re-attestation**: `docs/aura-quality-snapshot.md`'s "AG-UI gateway" row (the only
row whose CI-gate-path glob — `internal/agui/**` — matches a file this phase changed) was
re-attested with today's date and a prepended note describing the RESUME-01 500→400 fix and the
fresh 85.5%/86.3% figures, keeping the prior 2026-08-25 note intact as history. Commit `bbbcea04d`.

---

## Sign-off

- [x] All five ROADMAP Success Criteria have live evidence or an explicit not-proven entry (SC#5).
- [x] The D-13 A/B states its reading rule before its numbers and shows the ceiling/deadline unmoved.
- [x] The leftover auto-delivery is recorded as a COUNT (`1`), not an eyeballed observation.
- [x] RESUME-01's three folded defects are exercised live, with one real bug found and fixed.
- [x] Both delivery branches are recorded as exercised (`tool_result_append`) or explicitly
      unexercised with the reason (`user_message_fallback`).
- [x] FA-1 through FA-5 each carry a closed/still-open disposition; none disappeared quietly.
- [x] Every gate figure was produced by this tree today, not carried forward.
- [ ] Score ≥ 9.8 — **NOT MET (9.0/10).** The phase does not close on this session's evidence; the
      named gap (SC#5's Telegram leg) requires one short human-in-the-loop session to close.

**Approval:** phase does not close; recommend closing SC#5 with a human Telegram check, then
re-running this document's Success-Criteria section only (the gate/coverage numbers above do not
need to be re-measured for that follow-up).
