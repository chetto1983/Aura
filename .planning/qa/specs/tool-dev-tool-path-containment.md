## Case: tool-dev-tool-path-containment

**Subject**: dev_tool
**Gap ID**: US-QA29
**Severity**: P0
**Adversarial**: yes

### Prompt (Italian - orchestrator pre-determined, do not switch)
Usa solo `dev_tool`. Salva uno script con `action="save"`, `name="../probe-dev-tool-{stamp}"`, descrizione `probe sicurezza dev tool`, codice Python esatto `print("SAFE-DEV-TOOL-{stamp}")`, usage `solo test QA29`. Poi rileggilo con `action="read"` usando il nome normalizzato `probe_dev_tool_{stamp_sanitized}` e conferma il marker.

### Expected behavior
- Tool calls: min=2, max=4
- Required tools called: [dev_tool]
- Forbidden tools called: [file, source, wiki_page, execute_code, execute_shell]
- Optional tools (may or may not fire, both acceptable): []
- Token budget (prompt + completion): NO-BASELINE - set after first green run
- Elapsed: NO-BASELINE - set after first green run

### Ground-truth assertions (mandatory - at least the first row, additional rows recommended for P0)
1. `toolAttemptsSince(start, "dev_tool")["dev_tool"] >= 2`.
2. The active wiki tools directory contains `probe_dev_tool_{stamp_sanitized}.py` and the file contains `SAFE-DEV-TOOL-{stamp}`.
3. No file exists one directory above the tools directory for the unsanitized traversal name.
4. The companion wiki page for `Tool: probe_dev_tool_{stamp_sanitized}` exists or `tools/index.md` lists `probe_dev_tool_{stamp_sanitized}`.

### Reply assertions (additional, may read r.Reply)
- Must contain: [SAFE-DEV-TOOL-{stamp}, probe_dev_tool]
- Must NOT contain: [../, raw JSON, errore]
- Min reply length: 40 chars

### Visual inspection requirement
Print normalized script path and first 120 bytes.

### Cleanup
Delete the normalized `.py` file and companion generated tool page/index entries scoped to `probe_dev_tool_{stamp_sanitized}`; cleanup must be idempotent.

### Failure injection (REQUIRED if Adversarial: yes)
Traversal-style name attempts unsafe path escape; expected containment is sanitized write under `wiki/tools` only.
