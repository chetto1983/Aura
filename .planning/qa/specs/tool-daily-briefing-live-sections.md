## Case: tool-daily-briefing-live-sections

**Subject**: daily_briefing
**Gap ID**: US-QA31
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa `daily_briefing` con `limit=5`. Restituisci un briefing in italiano includendo task, proposta pending `qa31-proposal-{stamp}`, issue open `qa31-issue-{stamp}` e conversazione recente. Cita il marker `QA31-{stamp}` e il task `qa31-briefing-{stamp}`.

### Expected behavior
- Tool calls: min=1, max=3
- Required tools called: [daily_briefing]
- Forbidden tools called: [task, wiki_page, propose_patch, source, file, doc]
- Optional tools (may or may not fire, both acceptable): [tool_search]
- Token budget: NO-BASELINE - set after first green run
- Elapsed: <= 30 seconds

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. Setup seeds: active `scheduled_tasks` reminder due today named `qa31-briefing-{stamp}` with payload `QA31-{stamp}`; one pending `proposed_updates`; one open `wiki_issues`; one timestamped `conversations` user turn.
2. `env.toolAttemptsSince(start, "daily_briefing")["daily_briefing"] >= 1`; forbidden write-tool attempts are zero.
3. Seeded DB rows still exist with unchanged statuses after the run.

### Reply assertions (additional, may read r.Reply)
- Must contain: [QA31-{stamp}, qa31-briefing, qa31-proposal, qa31-issue]
- Must NOT contain: [raw JSON, errore]
- Min reply length: 120 chars

### Visual inspection requirement
Print seeded IDs and briefing reply preview.

### Cleanup
Delete only QA31 seeded rows; leave tool_attempts/run evidence.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
