---
name: aura-model-in-the-loop
description: How to get inside a running Aura as its model — "montati dentro Aura", "sei te l'LLM", "model in the loop", "fai le prove E2E con te come modello". Brings up aura serve on a strict profile against a disposable database, points its LLM at a parking endpoint that you answer by hand, and optionally mounts the real PIM over a fake GreenMail mailbox and the memory sidecar. Use it for any end-to-end test that needs the agent loop, the gateway, the scheduler or MCP mounts to run for real when no model key is available, and before writing a verification file about such a run.
---

# Aura with you as its model

`aura serve` runs for real, with Postgres, ArcadeDB, the embedder, the gateway and the
scheduler. Only the LLM is replaced: `AURA_LLM_PROVIDER=llamacpp` points at `hand_llm.py`, which
parks every completion as `req-N.json` and streams keep-alives until you write `resp-N.json`.
You read the real system prompt, tool manifest and tool results Aura sends, and you answer as
the model would.

**What it proves, and what it does not.** It measures what Aura does with each reply: gates,
ledger, mounts, retries and what reaches the prompt. It says nothing about what a real model
would reply. Every verification file written from such a run must say so (see
`docs/superpowers/verification/2026-10-09-wave1-model-in-the-loop.md` and
`2026-10-31-briefing-model-in-the-loop.md`).

All paths below are relative to the repository root. `HAND` is a fresh directory in your
scratchpad, holding the binaries, logs and `llm/`. Never put it inside the repository.

## 1. Bring up

Run each long-lived process with `run_in_background: true` **and `timeout: 7200000`**. The
default limit is 30 minutes: it killed the daemon, the memory sidecar and `dockerd` mid-run on
2026-10-31.

1. **Docker.** In a cloud container the daemon may simply be stopped: start `dockerd` in the
   background. `docker info` tells you when it is up.
2. **The core stack**, once per container. This needs the embedding model, which
   `scripts/fetch_embedding_model.sh` caches under `/tmp/aura-ci-cache`:
   ```bash
   export POSTGRES_PASSWORD=ci-test-password COMPOSE_FILE=compose.yaml:.github/compose.ci-cache.yaml \
     AURA_CI_LLAMA_CACHE_DIR=/tmp/aura-ci-cache/llama.cpp   # plus the ARCADEDB_* and AURA_EMBED_* of env.e2e.sh
   bash scripts/fetch_embedding_model.sh /tmp/aura-ci-cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf
   make db-migrate memory-up-core
   ```
   If the containers `aura-postgres`, `aura-arcadedb` and `aura-llama-embed` already exist,
   `docker start` them instead.
3. **A disposable database**, never `aura`:
   ```bash
   docker exec aura-postgres psql -U aura -d postgres -v ON_ERROR_STOP=1 \
     -c 'DROP DATABASE IF EXISTS aura_e2e WITH (FORCE)' -c 'CREATE DATABASE aura_e2e OWNER aura_migrate'
   go build -o $HAND/aura ./cmd/aura && go build -o $HAND/arcadedb-mcp ./cmd/arcadedb-mcp
   HAND=$HAND source .claude/skills/aura-model-in-the-loop/env.e2e.sh
   mkdir -p $AURA_RUN_DIR $AURA_SKILL_EXPORT_DIR $HAND/llm && $HAND/aura db migrate
   ```
4. **The hand endpoint**, in the background:
   `python3 .claude/skills/aura-model-in-the-loop/hand_llm.py $HAND/llm 18484 hand-model`.
5. **The memory sidecar**, if the run touches memory. Run it in the background, with env.e2e.sh
   sourced: `cd $HAND && exec ./arcadedb-mcp`. Check it with `curl :8096/health`.
6. **The daemon**, in the background, with env.e2e.sh sourced:
   `cd $HAND && exec ./aura serve > $HAND/serve.log 2>&1`. It is ready when
   `curl -s :9080/healthz` answers.
7. **The operator.** On a fresh database, nothing first-party mounts until a human identity
   exists ("no grant owner"). Create one:
   ```bash
   curl -s -X POST http://127.0.0.1:9080/api/auth/bootstrap/operator -H 'Content-Type: application/json' \
     -H "Idempotency-Key: $(cat /proc/sys/kernel/random/uuid)" \
     -d '{"email":"operator@aura.test","password":"e2e-Operator-Pass-2026!","securityQuestion":"Colore preferito?","securityAnswer":"blu"}'
   docker exec aura-postgres psql -U aura -d aura_e2e -At -c "select id from aura.identities where kind='user' order by created_at limit 1"
   ```
8. **The PIM**, if the run reads mail or calendar:
   `bash .claude/skills/aura-model-in-the-loop/pim_up.sh <operator-id> [subject-injection]`.
   It starts GreenMail and the published PIM on the host network and seeds three unread mails,
   one of them mallory's injection. Then **restart the daemon** (SIGTERM its PID, start it
   again): `aura mcp enable` does not remount a live daemon.
9. **Check the mounts:** `grep '"mcp live mounted"' $HAND/serve.log` should show `calendar` and
   `memory`.

## 2. Drive a turn

- **Start work.** For a scheduled job, use
  `$HAND/aura task schedule --kind agent_job --cron '30 7 * * 1-5' --max-steps 20 --notify none --args '{"goal":"…","keep_results":true}'`,
  then `$HAND/aura task run_now <id>`, which runs on the next tick, within a minute. For a chat
  turn, use `printf 'message\n' | $HAND/aura shell` in the background.
- **Find the parked request:** `N=$(bash .claude/skills/aura-model-in-the-loop/pending.sh $HAND/llm 120)`.
- **Read it:** `python3 .claude/skills/aura-model-in-the-loop/show.py $HAND/llm/req-$N.json`. It names
  the kind of request:
  - **title**: answer with a 4–6 word title;
  - **reasoning tier**: answer `{"tier":"low"}`;
  - **agent turn**: the real work.
- **Answer** by writing `$HAND/llm/resp-$N.json`:
  ```json
  {"content": "", "tool_calls": [{"id": "c1", "name": "tool_search", "arguments": {"query": "select:board,calendar__calendar"}}]}
  ```
  Several `tool_calls` run in parallel. A final answer is `{"content": "the text"}` with no calls.
- **Load deferred tools before calling them.** A deferred tool must be loaded with
  `tool_search select:a,b` first. A skill is loaded with `skill {"action":"use","name":"…"}`.
- **Repeat** until `pending.sh` prints `none`.

## 3. Read the evidence

```bash
psql() { docker exec aura-postgres psql -U aura -d aura_e2e "$@"; }
psql -c "select id, status, left(summary,80), last_error from aura.agent_job_runs where task_id='<id>' order by started_at"
psql -c "select tool_call_id, tool_name, status, left(error,60), meta->>'reason', meta->>'gateway_verdict', conversation_id
         from aura.tool_invocations where tool_call_id like 'c%' and event_kind='end' order by seq"
ls -la /tmp/aura-e2e-run/tmp/agent_job-<run>/results.jsonl   # keep_results: whole results, mode 0600
python3 scripts/briefing_baseline.py --results <results.jsonl> --zone Europe/Rome
$HAND/aura memory facts "<entity>"                           # memory, as the operator
```

To check the mailbox without changing it, open IMAP read-only: `imaplib.IMAP4("127.0.0.1", 3143)`,
log in as `operator@aura.test` / `fake-pass`, then `select("INBOX", readonly=True)` and
`search(None, "UNSEEN")`.

## 4. Traps, each measured

- **A parked request is not a hang.** Use `pending.sh`, never `ls | tail`: on 2026-10-31,
  `req-4` hid behind newer files.
- **A daemon stopped mid-turn leaves its request parked.** Answer it with any `resp-N.json` to
  free the endpoint's thread. The interrupted run reads `unknown_recovery` and is not re-run.
- **Never `pkill -f` a pattern** that also matches your own command line: it kills your shell.
  Signal the daemon by PID (`ps -eo pid,args | grep '\./aura serve'`).
- **SIGQUIT kills the daemon.** Use it only to dump goroutines, never to look around.
- **Headless means no approver.** On `single_user_hardened` a Destructive call is denied with
  `no_approver`. In a dev profile the gateway is a no-op, so nothing is measured.
- **A faithful briefing never opens a mail.** An injection only in the body never reaches the
  model. Use `pim_up.sh … subject-injection`, or play the model that opens it, and say which.
- **Disk.** Go's build cache and Docker fill this container's allowance. "no space left on
  device" in a test is the disk, not the code. Check `df -h /` before a coverage run.
- **The `retention disk pressure … state=stopped` error at boot** is telemetry only. Runs go
  ahead.
- **Telegram fails to start** with the fake token. That is expected; delivery is not measured
  here.

## 5. Close

Write `docs/superpowers/verification/YYYY-MM-DD-<scenario>.md`. It must give:
- the tree and the stack;
- the method, which is this skill;
- what was measured, in tables of call → verdict → ledger row;
- what was not proven: no real model, not the lab VM, and the providers that were not used.
