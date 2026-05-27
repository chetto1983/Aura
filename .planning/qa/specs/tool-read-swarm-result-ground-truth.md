## Case: tool-read-swarm-result-ground-truth

**Subject**: read_swarm_result
**Gap ID**: US-QA28
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa `spawn_aurabot` con role `librarian` per contare le vocali nella parola "Aurabot". Poi usa `read_swarm_result` sul task creato e rispondi solo con run_id, task_id, status e numero finale.

### Expected behavior
- Tool calls: min=2, max=5
- Required tools called: [spawn_aurabot, read_swarm_result]
- Forbidden tools called: [run_aurabot_swarm]
- Optional tools (may or may not fire, both acceptable): [list_swarm_tasks]
- Token budget (prompt + completion): NO-BASELINE - set after first green run
- Elapsed: <= 90 seconds

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. `env.toolAttemptsSince(start, "read_swarm_result")["read_swarm_result"] >= 1`.
2. Query latest completed `swarm_tasks` since start; capture `run_id`, `id`, `result`, and assert result contains `4`.
3. Query conversation/tool-result rows since start and assert one tool result contains `"result"` plus the captured `task_id` and `4`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [4, completed]
- Must NOT contain: [Sorry, I couldn't process, read_swarm_result:]
- Min reply length: 20 chars

### Visual inspection requirement
Print read tool-row preview and captured task row to stderr.

### Cleanup
None; swarm/audit rows are durable evidence.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
