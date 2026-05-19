## Case: tool-run-aurabot-swarm-one-shot

**Subject**: run_aurabot_swarm
**Gap ID**: US-QA26
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa solo `run_aurabot_swarm` con `mode="wait"`. Obiettivo del team: senza cercare file o memoria, restituisci esattamente la parola di controllo `ORIONE` e il marker `QA26-{stamp}`. Non usare `spawn_aurabot`, `list_swarm_tasks` o `read_swarm_result`.

### Expected behavior
- Tool calls: min=1, max=2
- Required tools called: [run_aurabot_swarm]
- Forbidden tools called: [spawn_aurabot, list_swarm_tasks, read_swarm_result]
- Optional tools: []
- Token budget (prompt + completion): NO-BASELINE - set after first green run
- Elapsed: NO-BASELINE - set after first green run

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. `toolAttemptsSince(start, "run_aurabot_swarm")["run_aurabot_swarm"] >= 1`; forbidden tool attempts are zero.
2. `swarm_runs` has completed row since `start` with `goal LIKE '%QA26-{stamp}%'`, empty `last_error`.
3. `swarm_tasks` for that `run_id`: count >= 1, all `status='completed'`, at least one `result` contains `QA26-{stamp}` and `ORIONE`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [ORIONE]
- Must NOT contain: [raw JSON, errore]
- Min reply length: 40 chars

### Visual inspection requirement
Print `run_id,status,goal` and first task `role,status,result[:200]`.

### Cleanup
None; leave swarm run/task rows as audit evidence.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
