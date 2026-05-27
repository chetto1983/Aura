## Case: web-fetch-example-domain-evidence

**Subject**: web
**Gap ID**: US-QA32
**Severity**: P0
**Adversarial**: no

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa lo strumento `web` con `action="fetch"` su `https://example.com/`. Rispondi in italiano con il titolo esatto e la frase inglese che inizia con "This domain is for use".

### Expected behavior
- Tool calls: min=1, max=1
- Required tools called: [web]
- Forbidden tools called: []
- Optional tools (may or may not fire, both acceptable): []
- Token budget (prompt + completion): <= 67000 total (baseline web-fetch 44604 x 1.5; split unavailable).
- Elapsed: <= 27 seconds.

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. Setup captures `startedAt`; Verify queries `tool_attempts` for `tool_name='web'`, `outcome='ok'`, `started_at >= startedAt`, and `arg_keys_json` containing `action,url`.
2. Verify independently fetches `https://example.com/` and asserts bytes contain `Example Domain` and the phrase prefix `This domain is for use`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [Example Domain, This domain is for use]
- Must NOT contain: [Sorry, I couldn't process, tool_result, {]
- Min reply length: 80 chars

### Visual inspection requirement
Print `fmt.Fprintf(os.Stderr, "[case=web-fetch-example-domain-evidence] fetched preview: %s\n", safePreview(body, 200))`.

### Cleanup
None; read-only web/DB checks only.

### Failure injection (REQUIRED if Adversarial: yes)
N/A.
