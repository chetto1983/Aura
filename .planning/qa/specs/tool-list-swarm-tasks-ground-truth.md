## Case: tool-list-swarm-tasks-ground-truth

**Subject**: list_swarm_tasks
**Gap ID**: US-QA27
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa `spawn_aurabot` con role `librarian` per contare le vocali nella parola "Aurabot". Poi usa `list_swarm_tasks` sul run creato e rispondi solo con run_id, task_id, status e numero finale.

### Expected behavior
- Tool calls: min=2, max=5
- Required tools called: [spawn_aurabot, list_swarm_tasks]
- Forbidden tools called: [run_aurabot_swarm, read_swarm_result]
- Optional tools (may or may not fire, both acceptable): []
- Token budget (prompt + completion): NO-BASELINE - set after first green run
- Elapsed: <= 90 seconds

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. `env.toolAttemptsSince(start, "list_swarm_tasks")["list_swarm_tasks"] >= 1`.
2. Query latest completed `swarm_tasks` since start; capture `run_id`, `id`, `result`, and assert result contains `4`.
3. Query conversation/tool-result rows since start and assert one tool result contains `"tasks"` plus the captured `run_id` and `task_id`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [4, completed]
- Must NOT contain: [Sorry, I couldn't process, list_swarm_tasks:]
- Min reply length: 20 chars

### Visual inspection requirement
Print list tool-row preview and captured task row to stderr.

### Cleanup
None; swarm/audit rows are durable evidence.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
