# 51-09 Task 3 — live verification of the D-03 termination model (measured 2026-08-29)

Stack: Docker Desktop, `aura:local` `sha256:9c12bd4a…` built 06:30:58Z from HEAD `34c5428e0`
(image freshness confirmed BEFORE driving; the previous image was 0104-era and crash-looped on
`migration tracker incompatible: version=107 want=104`). LLM: OpenRouter
`deepseek/deepseek-v4-flash-0731:nitro` (read from `aura.settings`, not assumed). Driver:
`drive.sh` + `goal-long.txt` / `goal-stall.txt` from a throwaway `curlimages/curl` container on
`aura_default`; verdicts read from the daemon log, Postgres (as `aura`, because `aura_app` without
an identity GUC sees nothing — the same RLS fact that produced defect A below) and the per-child
transcript, never from the SSE stream.

Container knobs during the runs (`docker exec aura env`): `AURA_SWARM_CHILD_IDLE_SEC=120`;
`AURA_SWARM_DELEGATION_LEASE_SEC` unmapped by compose → code default 300; `AURA_LLM_TOTAL_TIMEOUT_SEC=120`,
`AURA_LLM_STREAM_IDLE_TIMEOUT_SEC=60`; `AURA_SHELL_MAX_TIMEOUT_MS` unmapped → 10 min default.
**For the long run only** `compose.d03.yaml` raised `AURA_LOOP_MAX_WALLCLOCK_SEC=1200` /
`AURA_LOOP_MAX_STEPS=60` (see perimeter, §5). Restored to shipped defaults afterwards (verified).

## 1. Long run — conv `01a04c3b-0fb8-788d-b7eb-bcdcc3cc3d33`, job `41aaeb98-80a0-4d16-92a3-6fb675447a29`

Goal: 10 real documents, one `shell_exec` per document (soffice → PDF, `pdftoppm -r 300`,
`tesseract` OCR of page 1, `pdftotext | wc -w`), then one sha256 pass. Operator turn returned in
**14 s** with `swarm_spawn` on the wire and the assistant turn "Worker in coda…" (SC#1's first half).

| attempt | spawned | ended | `swarm.child.completed` | what happened |
|---|---|---|---|---|
| 1 | 06:35:46 | 06:39:41 | `status=failed dur=235.0s` | 5 documents done (two 47-page PDFs at 300 dpi included). Last worker event 06:37:41 (tool result of doc 5). Then the next LLM call produced no token for 120 s: `agent llm call error kind=stream_open_deadline err=context deadline exceeded` (the client's own `AURA_LLM_TOTAL_TIMEOUT_SEC`) at 06:39:41.463, the same instant the staleness deadline expired → report `stalled: no worker event for 2m0s`. Row → `queued`, retried after the 15 s backoff. |
| 2 | 06:39:58 | 06:44:51 | `status=ok dur=293.4s` | All 10 documents processed, report text complete ("All 10 documents processed…", in `steer_queue` body). **`swarm.delegation.record_failed err="append turn … seq 0: allocate turn seq …: conversation not found"`** → row `queued` again (defect A). `steer_queue` row `delegation_result` 06:44:51 written; `nudged_at` set (channel push fired, no error). |
| 3 | 06:45:08 | 06:50:35 | `status=ok dur=327.5s` | Same work redone from scratch; same `record_failed`; `swarm.delegation.dead_letter attempts=3`. One `agent llm call error kind=stream_retryable` (60 s stream idle) at 06:46:11 was retried by the client and did not affect the worker. |

`conversation_turns` for the conversation after 06:35:47: **0 assistant rows** (the SC#1 write never
landed). `steer_queue`: 2 × `delegation_result` (06:44:51, 06:50:35), undrained, nudged.

## 2. Stall run — conv `01a04c49-d670-7a69-aef7-d3c07b84c23d`, job `f967435e-39e2-4939-9b0c-721e9f9a64a4`

Goal: first and only action `shell_exec` `sleep 480 && echo STALL-PROBE-DONE` with an explicit
`timeout_ms=590000` (verified in the transcript). Operator turn returned in 12 s.

- 06:51:50 `swarm.child.spawned w1`; 06:51:54 the tool call starts (last worker event).
- 06:53:54 (**120.2 s later**) `swarm.child.failed error="context canceled"` → `swarm.child.completed status=failed dur=124.1s`; row error `stalled: no worker event for 2m0s`. The cancel is the staleness one (`context canceled`, distinct from the LLM deadline's `context deadline exceeded`).
- 06:54:10 attempt 2 spawned (15 s backoff) and re-issued the same `sleep 480`.
- 06:54:49 `docker top aura-box-b130c94d-…`: **the attempt-1 `sleep 480` (pid 53680, elapsed 2:54) is still alive** in the box next to attempt 2's (pid 57127). The reap ended the goroutine and returned `[command cancelled]` to the worker, but the sandbox process was not killed (defect E).
- The aura restart for §4 killed attempt 2's goroutine while its row held the lease; the row will be reclaimed at lease expiry, reaped once more and dead-lettered on its own. Its box processes self-exit at 480 s.

## 3. Transcript route (SWARM-10, plan 51-07) read mid-run

`GET /api/conversations/<conv>/swarm/w1/transcript` while attempt 1 was inside a `shell_exec`:
**200, 247 800 bytes, 16 ms**, twice 3 s apart (identical: no event during the tool call — consistent
with the staleness model). Traversal `..%2F..%2Fetc` → **404**; no cookie → **401**.

## 4. Boot gate (`idle == lease`)

`compose.bootgate.yaml` set `AURA_SWARM_CHILD_IDLE_SEC=300` (lease default 300). The daemon refused,
exit 71, restart-looping with:
`aura serve: config: AURA_SWARM_CHILD_IDLE_SEC=300 must be strictly less than AURA_SWARM_DELEGATION_LEASE_SEC=300 — a lease renewed only on worker events expires exactly `lease` seconds after the worker's last event, so an inactivity deadline that is not strictly shorter lets a reclaim race a goroutine that may still be alive`.
Restored: `docker compose --profile sandbox up -d aura` → healthy, `AURA_SWARM_CHILD_IDLE_SEC=120`, no `AURA_LOOP_*` in env.

## 5. Verdicts and perimeter

Proven (D-03):
- A worker that keeps emitting events is not reaped, however long it runs: **293 s and 327 s** completions, both past the retired ~240 s effective wall clock, both with continuous progress.
- A worker with no event for `AURA_SWARM_CHILD_IDLE_SEC` is reaped **once**, at 120 s, its context cancelled, its report `stalled: …`, the reap visible and attributed in the daemon log.
- `idle >= lease` refuses to boot naming both knobs.

NOT shown / found instead:
- **Defect A (51-10 delivery, SC#1 broken live):** the consolidated report is never written to `conversation_turns` — the claim loop calls `DelegationDelivery.Deliver` on a context without `identityctx`, so `conversations.Store.scopedTx` sets no `app.current_identity` and RLS hides the conversation (`allocate turn seq … conversation not found`). The worker itself gets `identityctx.WithIdentityID` (`delegation_run.go:238`); its report does not. Every attempt therefore fails, the queue retries the WHOLE worker (3 × 5 min of real work) and dead-letters. 51-10's tests used a fake recorder; the live path was never exercised under RLS.
- **Defect E (shell_exec box path):** a cancelled/timed-out `shell_exec` leaves its process running in the sandbox. `usersandbox.DockerBackend.Exec` on ctx cancel only closes the hijacked stream (`docker_backend_exec.go:31-60`); the process-group `Kill` capability already exists on `ExecStream` (`:79-150`, PID file + separate box exec) and is not used by `shell_exec`. With the retry policy this yields N concurrent copies of the same command in one box.
- **Finding B (retry semantics, decision needed):** the shipped 51-01 policy (`max_attempts=3`, 15 s backoff) retries a reaped worker AND a successful worker whose report could not be recorded, re-running the goal from scratch each time. Plan step 4 ("no second worker picked up the same goal afterwards") is not what the tree does; the plan's threat model only forbids two CONCURRENT workers (lease reclaim under a live goroutine), which was not observed. Both measured stalls were upstream OpenRouter stream-open deadlines, i.e. the case a retry is for.
- **Finding C (label ambiguity):** `AURA_SWARM_CHILD_IDLE_SEC` (120) equals `AURA_LLM_TOTAL_TIMEOUT_SEC` (120), so an LLM stream-open timeout and a staleness reap fire in the same millisecond and the report says `stalled` for what is an upstream stall. Correct under D-03's definition (no progress is no progress), but the report cannot tell the two apart.
- **Finding D (dark config):** `.env` carries `AURA_LOOP_MAX_WALLCLOCK_SEC=300` / `AURA_LOOP_MAX_STEPS=25` but `compose.yaml` does not map either, so the container runs on code defaults regardless of `.env`. The 327 s completion above would have been cut at 300 s by the Budget wallclock under shipped defaults: D-03 removes the per-child wall clock, the tree Budget (300 s / 25 steps) remains the ceiling until the knobs are mapped and raised.
- Not measured: `attempt_count` staying at 1 on a successful run (step 3) — every run here was retried for defect A, so the "no reclaim under a live worker" assertion is only supported indirectly (no concurrent `swarm.child.spawned` ever overlapped a live one; each spawn followed a `completed`).
- Not measured: the Telegram nudge's arrival on the operator's phone (fired without error at 06:44:51+grace; only the operator can confirm).

## 6. Re-verification after the fixes (image `a119b9a6…` built 08:37:51Z from `e5c8227b2`, 2026-08-29)

Shipped defaults throughout (`AURA_SWARM_CHILD_IDLE_SEC=120`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300` and
`AURA_LOOP_MAX_STEPS=25` now visible in `docker exec aura env` — defect D wired). Two probes issued
1 s apart from the throwaway curl container: `stall` (conv `01a04cab-a178-…`, job `12cf82b2…`) and
`long4` (conv `01a04cab-a7fb-…`, job `f1550cc1…`, 4 documents so the goal fits the 300 s budget).
Both operator turns returned in **13 s** with `swarm_spawn` on the wire.

| what | measured | verdict |
|---|---|---|
| **Defect E** (box process leak) | stall attempt 1: tool call 08:38:44, reaped 08:40:44 (`dur=124.8s`, `context canceled`, transcript `[command cancelled]`). At 08:42:07 `docker top aura-box-b130c94d…` shows **no `sleep 480`** (yesterday's shape: alive at 2:54 elapsed). `/tmp/.aura-exec-79050c1c….pid` remains, `pid=14` dead — the killed wrapper skips its EXIT trap, as documented; the long4 worker's own `shell_exec` files were removed by the trap (one seen at 08:41, gone by 08:42). | **fixed** |
| **Defect A** (SC#1 record under RLS) | long4 attempt 2 completed `status=ok dur=182.0s` at 08:48:58 → `aura.ingestion_jobs` **`succeeded`** (attempt 2), `aura.conversation_turns` has the assistant row `[Delegated worker report -- goal: "Lavora SOLO dentro /workspace …` at 08:48:58, `aura.steer_queue` has one `delegation_result` (drained=f, nudged=t: no live turn at delivery, so the channel nudge is per policy). | **fixed** |
| **Finding C** again | long4 attempt 1: last event 08:41:51 (`Costituzione_della_Repubblica_italiana.pdf: pagine=47 parole=14137`), then `agent llm call error kind=stream_open_deadline` at 08:43:51.397 and the reap in the same millisecond → row `stalled: no worker event for 2m0s`. Second time today at the same shape (after a large document's tool result). The report says `stalled`; the cause was upstream. | unchanged, recorded |
| **Finding F — NEW (fixed in `02a2092d0`)** | the two rows were claimed together (`defaultDelegationBatchSize=4`) and `ProcessOnce` ran them **serially**: long4 claimed 08:38:39 but spawned only at 08:40:44, after the stall worker's reap — 125 s waiting with its lease ticking and nobody renewing it. With a 300 s worker ahead of it the 300 s lease would have expired in the queue and the row fenced out as lease-lost. `ProcessOnce` now runs the batch concurrently, one heartbeat per row (unit-proven: slow+fast rows claimed together, fast report recorded first). | fixed, live proof in §7 |
| operator experience | the operator (in the cockpit, 08:45:11) asked *"qualcosa non ha funzionato?"* — the 3-minute job took 10 minutes wall-clock (attempt 1 reaped on an upstream stall, attempt 2 queued behind the stall probe) and the parent agent could only `ls` the box to guess; then, at 08:48:58, the raw JSON report appeared as an assistant bubble. Both are the SWARM-12 gap (PRD Amendment #172), not this plan. | measured, gap plan |

Not shown here: `attempt_count = 1` on an undisturbed successful run (attempt 1 stalled upstream
again); the Telegram nudge's arrival (fired without error).

## 7. Finding F, measured three times before it was fixed for real (2026-08-29)

| build | what happened to two delegations issued 1 s apart | verdict |
|---|---|---|
| `e5c8227b2` (serial pass) | claimed together 08:38:39; the second **spawned 08:40:44**, 125 s later, after the first was reaped — a lease ticking in the queue with nobody renewing it | F found |
| `02a2092d0` (batch concurrent inside the pass) | first claimed within its second (08:54:42); the second row, created 3 s later, **still `queued` at 08:56:35** — the pass blocks until its workers finish, and nothing claims in the meantime | F half-fixed |
| `ca1673b8d` (`Run` keeps claiming) | both rows **`queued` at 09:07:45**, 60 s after issue: `cmd/aura` never calls `Run` — it drives `ProcessOnce` from the shared runtime ticker (`serve_delegation.go:197`, `asset_processing_worker.go`), and `ProcessOnce` still ran its pass to completion | wrong seam |
| `fef1928cc` (`ProcessOnce` dispatches and returns; image `8761e305…`) | both rows created 09:15:38, both **`running` at 09:15:40**, two `swarm.child.spawned` at **09:15:40.026** (0.3 ms apart) | **F fixed** |

Lesson recorded in the code comment: the daemon's own ticker is the loop; a pass must claim what
fits in the free slots and return. `Wait()` exists for tests and one-shot callers only.

Also measured on `fef1928cc` (run 5): the 4-document delegation completed `ok` at the **first attempt** —
`0ff698b8` `succeeded`, `attempt_count=1`, report row in `conversation_turns` at 09:16:12, one
`delegation_result` steer row — the "no reclaim under a live worker" assertion §5 could only support
indirectly is now observed directly. The stall probe was reaped three times at ~120 s and
dead-lettered at 09:22:20 with no `sleep 480` left in the box at 09:32. Perimeter: recreating the
`aura` container (`compose up -d` with a new image) kills the daemon's goroutines but holds no handle
on box processes, so a `shell_exec` in flight at that moment runs to its natural end inside the box
(one such `sleep 480` from the 09:15 recreate was alive at 09:18, gone by 09:23) — a restart is not a
reap, and the lease reclaim on the next boot is the only recovery for the row.
