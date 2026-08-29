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
