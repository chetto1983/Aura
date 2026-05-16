# Aura Conversation Inventory - 2026-05-16

Role: evidence

## Scope

This inventory was built from read-only inspection of `D:/Aura/data/aura.db`,
`D:/Aura/data/logs/aura-2026-05-16.log`, and `docker compose logs --since=6h aura`.
No live conversation, run, question, schedule, wiki, source, or projection rows
were edited.

## Current Conversation State

- `conversations`: 3,096 rows for Telegram chat `1148481707`.
- Role split: 584 `user`, 1,362 `assistant`, 1,150 `tool`.
- Date range: 2026-05-09 through 2026-05-16.
- 2026-05-16 volume: 411 archived turns at inspection time.
- Duplicate `(chat_id, turn_index)` rows: none found in the earlier integrity
  pass.

## Current Run State

- `runs`: 161 rows at inspection time.
- Telegram: 75 completed, 4 failed, 1 cancelled, 2 running.
- Web: 74 completed, 1 running, 4 waiting_for_user.
- `run_events`: 269 `tool_start` and 269 `tool_end` events.
- `run_outbox`: 0 rows.
- `tool_attempts`: 0 rows before this code fix, despite the 269 persisted tool
  start/end pairs.

## Fixed In This Slice

### Tool Attempts Were Not Recorded For Hub Chat Tools

Evidence:

- `run_events` showed 269 `tool_start` and 269 `tool_end` events.
- `tool_attempts` was empty.
- Code inspection showed Telegram and web Hub paths used custom executors that
  bypassed `agentExecutor`, the only executor that wrote `tool_attempts`.

Fix:

- Added channel-neutral `agent.RecordToolAttempt`.
- Wired `agent.ExecuteToolCalls` custom-channel helper to record
  `tool_attempts` when a `run_id` and attempts repo are available.
- Wired Telegram `Bot` runtime with `AttemptsRepo` from the composition root.
- Wired web chat `webToolExecutor` with the Hub `run.ID` and `AttemptsRepo`.
- Added tests proving tool attempts are recorded for:
  - channel-neutral helper execution,
  - Telegram Hub run context,
  - web Hub chat run context.

Limits:

- Historical `tool_attempts` rows were not backfilled.
- The currently running Docker service still needs rebuild/restart before this
  code path is active in the live bot.

### Telegram `ask_user` Was Treated As A Tool Error

Evidence:

- Log line on 2026-05-16 showed `tool call failed` for `tool="ask_user"` with
  `ask_user: awaiting user input`.
- Conversation archive also stored `Error: ask_user: awaiting user input...`.
- The Telegram helper returned a formatted tool error instead of propagating
  `ErrAwaitingUserInput` into the agent loop.

Fix:

- `agent.ExecuteToolCalls` now detects `ErrAwaitingUserInput`, attaches the
  tool call id, avoids writing a tool-result message, and returns the waiting
  sentinel.
- Telegram invocation mapping now forwards `AwaitingUserInput` to
  `agent.ExecutionSummary`, allowing the Hub to emit `question_requested` and
  preserve `waiting_for_user`.
- Added a Telegram unit test proving no tool result is appended while waiting.

## Open Inventory Items

### Stale Or Live Non-Terminal Runs

Observed non-terminal rows:

- Telegram running `eb7146211876a353`, started 2026-05-16T16:22:37Z.
- Telegram running `c979e1c2f22b299d`, started 2026-05-16T09:16:30Z.
- Web running `6fe4608273a55680`, started 2026-05-16T08:42:54Z.
- Web waiting_for_user: `dafabe1fe49ab31e`, `b71e2677b9683e41`,
  `13f3c2b4aca94092`, plus historical `952087b617b3c8a6` with empty thread id.

No rows were closed or modified. A future cleanup slice should define a durable
reconciliation policy for stale `running` and historical `waiting_for_user`
rows.

### Waiting Questions

`chat_questions` has three `waiting` rows:

- `43211e22`: irreversible source delete confirmation for
  `src_b3d20341e7b5ff32`.
- `a21b8513`: `Phase01C live question gate final?`
- `3cdcab1a`: `Phase01C live question gate after fix?`

These were left untouched because answering, expiring, or cancelling them would
mutate live user state.

### Repeated Tool Schema Misuse In Archived Conversations

Pattern counts from archived tool content:

- `wiki_page: unknown action`: 10.
- `wiki_page edit: old_text is required`: 4.
- `read_file: workspace`: 26.
- `web: 'action' is required`: 9.

These are now better candidates for Phase06 tool-experience learning because
new future executions will populate `tool_attempts`. Historical rows remain
archive-only evidence.

### Container CLI Gaps From Archived Conversations

Archived turns 3743-3746 explicitly probed the Aura runtime container and found
the Debian base too sparse for operator diagnostics. Present commands included
`python3`, `curl`, `wget`, `git`, `jq`, `openssl`, `unzip`, and `zip`; missing
commands included `vim`, `nano`, `dig`, `nslookup`, `ping`, `ss`, `ip`,
`netstat`, `traceroute`, `nmap`, `tcpdump`, `strace`, `htop`, `socat`, `make`,
`gcc`, `ssh`, and `yq`.

The same archive contains Python runtime misses for `python-docx` and
`matplotlib`. The operational conclusion is that Aura should strengthen the
container CLI surface used by `execute_shell` / `execute_code` instead of
adding many narrow native tools for routine diagnostics and document/data work.
No password-audit or brute-force tools were adopted.

### Tool Index Periodic Reconcile Warning

Logs repeatedly show periodic `toolindex reconcile` with `errors=1`, while boot
reconciles report `errors=0`. The warning line does not include the underlying
error detail, so this remains an inventory item rather than a fixed bug.

Recommended follow-up slice:

- Add structured detail for periodic reconcile errors, without logging raw tool
  arguments or secrets.
- Use a targeted probe against the tool index state and Qdrant collection to
  determine whether this is a transient projection issue or an actual stale
  index.

### Projection Freshness Counter Mismatch

`projection_state` reports `compact_memory_documents` as `fresh` while
`pending_count=87`, `completed_count=1`, and `failed_count=0`.

This may be legitimate queue semantics, but it looks inconsistent with Aura's
freshness doctrine. It was not changed here because Phase07/RAG freshness work
is the correct owner.

### Audit Plane Is Empty

`audit_events` contains 0 rows. This was not treated as a bug in this slice
because no current denial event was reproduced. It remains a watch item for
identity/grant, settings, memory, wiki/source, export, purge, and privileged
payload-access slices.

## Verification

Passed:

```powershell
go test ./internal/agent ./internal/telegram ./internal/channels/telegram ./cmd/aura
go test ./internal/chat ./internal/channels/web ./internal/channels/cron ./internal/storage/runs ./internal/agent/tools/attempts
```

Not run in this slice:

- Live Telegram E2E, because that would interact with the current bot/user
  channel.
- Live DB cleanup/backfill, because the user did not explicitly authorize
  mutation of historical conversation/run/question state.
