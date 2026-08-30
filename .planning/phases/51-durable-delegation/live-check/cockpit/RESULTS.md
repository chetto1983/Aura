# Phase 51 Plan 12b - Live Cockpit Delivery Envelope

Date: 2026-08-30

Overall result: **PASS**

- Verdicts: **6/6 pass**
- Automated verdicts: **5/5 pass**
- Telegram database gate: **pass for both negative and positive halves**
- Telegram device arrival: **pass on the operator-supplied authenticated Telegram Web surface**
- Final DoD score: **9.9/10**, above the required `>9.8`

No verdict below is derived from grepping an SSE stream. Browser evidence came from
privacy-safe Playwright assertions over the accessible DOM and browser network requests.
Screenshots, video and trace were disabled, and no Telegram chat id, conversation id,
message content, session data or screenshot was retained.

## Evidence Topology

The final characterization used two real production-path fan-outs on the same rebuilt
stack:

1. A Telegram-origin fan-out established the absent-operator aggregation gate, durable
   cards and report assets. Private identifiers are redacted.
2. A fresh cockpit-origin fan-out established the live pane, picker, EventSource counts,
   long-result layout, artifact panel and `swarm_status` answer. Private identifiers are
   redacted.

The split avoided reusing the private Telegram conversation as browser fixture data. Both
runs used the real agent, real Postgres, real sandbox workers and the routed local LLM.

## Image And Routed Model

- image: `aura:local`
- image digest: `sha256:699ce1260f16f7974f6d9885121ad8b109f22e5f867065a76d626c54f9ee95ca`
- image created: `2026-08-30T01:21:54.878858656Z`
- container state at drive: `running`, `healthy`
- schema migration: `110`, `dirty=false`
- `AURA_LLM_PROVIDER=llamacpp`
- `AURA_LLM_BASE_URL=http://aura-llm:8084/v1`
- `AURA_LLM_MODEL=gemma-4-12b`

The model selection was made through the cockpit. `aura.settings` was read only to verify
the persisted route. No OpenRouter model was used for these drives.

## Relevant Knobs

- `AURA_RUN_DIR=/var/lib/aura/runs` (container value)
- `AURA_SWARM_DELEGATION_NUDGE_SEC=60` (catalog default; not overridden)
- `AURA_DELEGATION_RESULT_TTL_SEC=86400` (catalog default; not overridden)
- `AURA_SWARM_CHILD_IDLE_SEC=120` (catalog default; not overridden)
- `AURA_SWARM_DELEGATION_LEASE_SEC=300` (catalog default; not overridden)

## Verdicts

### 1. One Card Per Worker, Not Raw JSON - PASS

**Browser source:** repository Playwright replay on the fresh cockpit thread.

- accessible assistant card count with `Report completo:`: `2`
- cards containing the raw `child_id` JSON key: `0`
- dispatch display: `swarm_report`, `2` worker rows

**PostgreSQL source:** `aura.conversation_turns.role`, `content`, `created_at` on the
origin conversation after the operator turn.

- Telegram-origin run: `2` terminal cards, `0` raw JSON bubbles
- final cockpit run: `2` terminal cards, `0` raw JSON bubbles

### 2. Full Report Artifacts In The Artifacts Panel - PASS

**Browser source:** authenticated `/api/assets?thread_id=...` plus the Artifacts region.

- API rows: `2`
- accepted `text/markdown`, `source_kind=agent` rows: `2`
- Artifacts DOM rows: `2`
- preview dialog opened from the first report row: `true`

**PostgreSQL source:** `aura.assets.file_name`, `mime_type`, `source_kind`, `thread_id`,
`status`, `size_bytes`.

- Telegram-origin run: `2` assets, both `text/markdown`, both `agent`, same origin
  thread, `2` distinct names, `856..868` bytes
- final cockpit run: `2` assets, both `accepted text/markdown`, `2` distinct names

### 3. One Telegram Message Per Fan-out, Not Before - PASS

**Negative half, database PASS.** Source: `aura.ingestion_jobs.status`,
`aura.steer_queue.kind`, `drained_at`, `nudged_at`, `fanout_key`, and
`aura.pending_notifications` while one worker was terminal and the other was running.

- terminal worker rows available: `1`
- sibling still `running`: `1`
- `delegation_result` steer rows: `1`
- undrained rows: `1`
- rows with `nudged_at IS NULL`: `1`
- fan-out keys: `1`
- pending notifications: `0`

**Positive half, database PASS.** Source: the same tables after the slower worker reached
terminal.

- succeeded jobs: `2`
- `delegation_result` steer rows: `2`
- undrained rows: `2`
- distinct non-null fan-out keys: `1`
- distinct non-null `nudged_at` timestamps: `1`
- pending notifications: `0`

**Device source:** repository Playwright attached over the existing browser's local CDP endpoint
to the operator-supplied authenticated Telegram Web page. No MCP server was used. The assertion
read only message direction, order, displayed minute and structural counts; it did not read or
retain message text, identifiers, chat metadata or session data.

- distinct inbound terminal notifications for the two Telegram trials: `2`, displayed at `02:26`
  and `02:47`
- first and last worker terminal times for the final trial: `02:42:15` and `02:47:32`
- database nudge time for that fan-out: `02:47:33`
- inbound notifications between the prior terminal notification and the final trial's last-worker
  completion: `0`
- inbound notifications when the final fan-out became terminal: `1`
- status markers in each terminal notification: `2`
- fixed closing-line structure in each terminal notification: present

This closes both device halves: the page remained silent while the sibling was running, then
showed one structurally complete notification per trial only at terminal fan-out. No message
content is copied into this report.

### 4. Live Worker Pane - PASS

**Browser source:** one real three-minute Playwright drive on the rebuilt image.

- dispatch rows: `2`
- opening Watch closed the visible Artifacts region: `true`
- connecting state observed before transcript content: `true`
- live `shell_exec` card observed: `true`
- second worker selected through the picker: `true`
- active tool state on the second worker: `Running`
- fast row reached `OK` while slow row remained `Running`: `true`
- slow row later reached `OK`: `true`
- browser requests: `1` status EventSource and `2` distinct child EventSources
- repeated swarm-route requests: `0`
- page reload needed for live updates: `false`

**Filesystem source:**
`/var/lib/aura/runs/[redacted]/swarm/[redacted].jsonl`.

- transcript files: `2`
- files containing `shell_exec`: `2`
- files whose last line carries `swarm_child_status`: `2`
- aggregate transcript bytes: `311170`

### 5. swarm_status Answers From Facts - PASS

**Browser source:** the operator asked for progress while the slower worker was running.

- persisted snapshot calls to `swarm_status`: `1`
- final user-facing answer names a worker id: `true`
- final answer states a worker status: `true`
- final answer names observed `shell_exec` activity: `true`
- final answer includes elapsed duration: `true`

The first local-model drive exposed that the technical result carried `elapsed_sec` but the
final answer omitted it. Amendment #182, RED commit `723daa60b`, and fix commit
`e17e7b552` tightened the Deferred tool contract. The rebuilt-image drive then passed the
same answer-shape assertion.

### 6. Backstop Considerations - PASS

**Browser source:** the same live pane drive and a terminal replay drive.

- connecting copy during delayed replay: **observed**
- mid-tool-call chip in `Running`: **observed**
- result over 2000 characters: **observed**, `textLength=2504`
- long-result layout: `clientWidth=345`, `scrollWidth=345`, no horizontal overflow

The replay also proved the terminal lifecycle correction: a complete named-event transcript
remains visible after terminal EOF instead of switching to the fallback.

## Verification Commands

- focused worker Vitest suites: pass (`15` tests before the final drive)
- web lint: pass
- web typecheck: pass
- `go vet ./...`: pass
- `go build ./...`: pass
- `go test ./internal/agent/tools -run '^TestSwarmStatus'`: pass
- WSL `go test -race ./internal/agent/tools -run '^TestSwarmStatus'`: pass
- full Windows `internal/agent/tools` package: one unrelated pre-existing NTFS mode
  assertion failed (`0600` observed as `0666`); no `SwarmStatus` test failed

## What This Does Not Prove

- A fan-out with a worker parked in `awaiting_input`. Under the selected one-message
  policy, the phone remains silent about completed siblings until the question is answered
  or the parked row becomes terminal/expired.
- Worker-pane recovery across an `aura` daemon restart in the middle of a worker.
- Terminal card rendering on a phone viewport.
- Artifact size behavior at the step-budget and wall-clock caps.
- Selection between two channel Deliverer candidates. Telegram remains the only shipped
  owner for the tested identity.
- Lossless recovery after a non-terminal child EventSource disconnect. Terminal replay and
  terminal EOF are proven; a mid-run network interruption is not.

## DoD Score

Final score: **9.9/10**.

The real scenario, persistence, worker execution, live browser surface, local model, restart
image provenance and automated regressions all pass. The authenticated Telegram Web observation
closes the negative and positive device halves without retaining private Telegram data, so the
project's `>9.8` completion gate is met.
