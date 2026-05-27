## Case: tool-search-doc-routing

**Subject**: tool_search
**Gap ID**: US-QA30
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa `tool_search` con query="genera un documento Word da blocchi strutturati" e limit=5. Non creare file. Rispondi solo cosi: `tool=<nome>; schema=<3 campi/azioni viste>`.

### Expected behavior
- Tool calls: min=1, max=1
- Required tools called: [tool_search]
- Forbidden tools called: [doc, execute_code, execute_shell]
- Optional tools (may or may not fire, both acceptable): []
- Token budget (prompt + completion): <= 55,000 total from `tool-execute-shell` baseline x1.5; split unavailable.
- Elapsed: <= 15 seconds.

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. `env.toolAttemptsSince(startedAt, "tool_search")["tool_search"] >= 1`.
2. The successful `tool_attempts` row for `tool_search` has `arg_keys_json` containing `query` and `limit`.
3. `toolAttemptsSince(startedAt, "doc")["doc"] == 0`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [tool=doc, action, docx]
- Must NOT contain: [source_id, Sorry, I couldn't process, Error:]
- Min reply length: 25 chars

### Visual inspection requirement
Print counts and the `tool_search` attempt preview: `fmt.Fprintf(os.Stderr, "[case=tool-search-doc-routing] tool_search=%d doc=%d attempt=%s\n", ...)`.

### Cleanup
None; read-only. Reset `startedAt`.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
