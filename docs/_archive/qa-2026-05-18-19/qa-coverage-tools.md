# Tool Coverage Matrix - 2026-05-19 - git_head: c3b1f0085533aac961f37ce4d2d362146b4c205c

Cross-reference of the 23 LLM-callable tools in `docs/qa-tool-surface.md` against the existing probe and unit suite.

Sources scanned:

- `cmd/probe_chat/cases.go`
- `cmd/probe_chat/phase07d.go`
- `cmd/probe_chat/phase07e.go`
- `cmd/probe_chat/phase07f.go`
- `internal/agent/tools/registry/*_test.go`
- `internal/agent/tools/index/*_test.go`
- `internal/agent/tools/sets/*_test.go`
- `internal/agent/tools/swarm/*_test.go`
- `.planning/qa/baseline-run.json`
- `.planning/qa/baseline-run.stderr.log`

Baseline: `.planning/qa/baseline-run.json` has 30/30 PASS. `baseline-run.stderr.log` shows `tool-subagent-dispatch` passed through an infra-skip branch with zero successful `tool_attempts` rows, so that row is PARTIAL even though the case is green.

Severity rule applied strictly from the surface inventory: P0 means the tool has an E2E gap or failing case and either its category is `storage-write`, `external-API`, or `sandbox-exec`, or its `capability_gate` is non-empty. Since every surfaced tool is capability-gated, every E2E MISSING/PARTIAL row is P0. P1 is reserved here for a non-P0 unit gap on a non-trivially used tool. P2 means no active gap after this scan.

## Count summary

| gap_severity | total tools | E2E COVERED | E2E PARTIAL | E2E MISSING | unit COVERED | unit PARTIAL | unit MISSING |
|--------------|-------------|-------------|-------------|-------------|--------------|--------------|--------------|
| P0 | 11 | 0 | 4 | 7 | 11 | 0 | 0 |
| P1 | 1 | 1 | 0 | 0 | 0 | 1 | 0 |
| P2 | 11 | 11 | 0 | 0 | 11 | 0 | 0 |
| TOTAL | 23 | 12 | 4 | 7 | 22 | 1 | 0 |

## Per-tool matrix

| tool | E2E status | unit status | evidence | currently_passing | gap_severity |
|------|------------|-------------|----------|-------------------|--------------|
| `file` | COVERED | COVERED | E2E: `file-write-read-roundtrip` in `cases.go:369` asks the model to write and reread a workspace file, then verifies bytes on disk with `os.ReadFile` at `cases.go:380`. Unit: `file_test.go:174` covers unified `file.Execute` write/read/search/patch/list and `file_test.go:237` covers sensitive path denial; `workspace_files_test.go` covers helper Execute paths. | true | P2 |
| `source` | COVERED | PARTIAL | E2E: `source-store-read-roundtrip` verifies raw source bytes through `env.fetchSourceRaw`; `phase07e-source-span-read` verifies `search_memory` then `source(action=read)` through `tool_attempts`; `tool-ingest-source` verifies the generated wiki page through the API; `tool-ocr-source` verifies `ocr.md` content through the source markdown API. Unit: `source_test.go` directly covers helper Execute paths (`store_source`, `read_source`, `list_sources`, `lint_sources`, `ocr_source`, `delete_source`) but no test directly instantiates unified `NewSourceTool` or `SourceTool.Execute`. | true | P1 |
| `web` | PARTIAL | COVERED | E2E: `web-fetch-summarize-context-engineering` in `cases.go:122` requires a tool call and reply keywords, length, and bullets, but it has no fetched-byte, DB, or tool-attempt ground truth. Unit: `web_dispatcher_test.go:48` and `:68` cover unified `web.Execute` search/fetch dispatch; `web_test.go` covers SearXNG and direct fetch behavior. | true | P0 |
| `doc` | COVERED | COVERED | E2E: `doc-xlsx-roundtrip`, `doc-docx-roundtrip`, `doc-pdf-roundtrip`, and `doc-xlsx-italian-chars` fetch raw source bytes and inspect XLSX/DOCX/PDF structure with `docinspect`. Unit: `doc_test.go:97`, `:114`, and `:132` cover unified `doc.Execute`; `files_test.go` covers per-format persistence and delivery. | true | P2 |
| `wiki_page` | COVERED | COVERED | E2E: `wiki-page-create` verifies `/api/wiki/page` returns the created page body; `phase07f-wiki-frontmatter-metadata` verifies the page body and metadata through `env.fetchWikiPage`. Unit: `wiki_test.go:95` through `:364` covers `wiki_page.Execute` create, replace, edit, append, conflicts, source annotations, and reindex submission. | true | P2 |
| `task` | COVERED | COVERED | E2E: `schedule-reminder` verifies the scheduled task via `env.fetchTask`; `phantom-trap-nonexistent-task` checks the scheduler DB for a nonexistent task. Unit: `scheduler_test.go` covers unified `task.Execute` for schedule, list, cancel, run_now, recurrence, agent_job payload normalization, and validation. | true | P2 |
| `search_memory` | COVERED | COVERED | E2E: `phase07e-source-span-read` verifies successful `search_memory` and `source` `tool_attempts`; `phase07f-wiki-frontmatter-metadata` verifies wiki API body after search; `failure-max-iterations` and `failure-max-elapsed-wrap` drive repeated `search_memory` calls and verify run completion in the DB. Unit: `memory_search_test.go` covers Execute for metadata, scopes, compact search, freshness, ranking, follow-up hints, and graceful degradation. | true | P2 |
| `recall_operational` | COVERED | COVERED | E2E: `phase07d-mixed-tier-recall` seeds compact memory, prompts explicit `recall_operational`, verifies the approved operational token, rejects wrong-tier leaks, and checks `tool_attempts` for `recall_operational`. Unit: `recall_operational_test.go` covers empty, populated, recalled marker, filter-by-tool, and pending-excluded behavior. | true | P2 |
| `recall_user_memory` | COVERED | COVERED | E2E: `phase07d-mixed-tier-recall` seeds compact memory, prompts explicit `recall_user_memory`, verifies the approved user-memory token, rejects wrong-tier leaks, and checks `tool_attempts` for `recall_user_memory`. Unit: `recall_user_memory_test.go` covers empty, populated, filter-by-category, and pending-excluded behavior. | true | P2 |
| `agent_note` | COVERED | COVERED | E2E: `agent-note-roundtrip` sends set/get/clear turns on one web thread, verifies the exact `agent_notes` DB row after set, verifies the get reply, and verifies the row is empty or gone after clear. Unit: `agent_note_test.go` covers Execute for set, append, get, clear, empty get, and roundtrip. | true | P2 |
| `daily_briefing` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `daily_briefing`. Unit: `daily_briefing_test.go` covers Execute composition, archive/proposal/issue reader interfaces, and empty-store handling. | n/a | P0 |
| `propose_patch` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `propose_patch`. Unit: `propose_patch_test.go` covers Execute for wiki, user-memory, operational proposals, idempotency, auto-accept operational writes, provenance rejection, quarantine, hard reject, and write-proposal allowlist behavior. | n/a | P0 |
| `ask_user` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `ask_user` pause/resume behavior. Unit: `registry_test.go:213` executes `ask_user` through the registry and verifies the `ErrAwaitingUserInput` sentinel is not logged as a tool failure. | n/a | P0 |
| `execute_code` | COVERED | COVERED | E2E: `tool-execute-code` checks sandbox health, prompts `execute_code`, verifies the Fibonacci result in the reply, and verifies a successful `tool_attempts` DB row for `execute_code`. Unit: `exec_test.go` covers Execute through the sandbox executor interface, artifact delivery, source persistence, readable persisted script artifacts, internal-tool manifests, and manifest filtering. | true | P2 |
| `execute_shell` | COVERED | COVERED | E2E: `tool-execute-shell` checks sandbox health, prompts deterministic `echo`, verifies the marker in the reply, and verifies a successful `tool_attempts` DB row for `execute_shell`. Unit: `exec_test.go:91` directly covers `execute_shell.Execute` through a command executor and `exec_test.go:106` covers non-process registration gating. | true | P2 |
| `dev_tool` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `dev_tool`. Unit: `tool_mgmt_test.go` covers Execute for missing/unknown action, list-empty, list-with-scripts, read missing/name, read returning code, save validation, and save success. | n/a | P0 |
| `tool_search` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `tool_search`; tool retrieval remains untested through the live agent loop. Unit: `tool_search_test.go` covers Execute query validation, hit ranking, and no-match helpful output. | n/a | P0 |
| `request_dashboard_token` | MISSING | COVERED | E2E: no `cmd/probe_chat` case verifies token issuance through chat, hashed `api_tokens` persistence, out-of-band delivery, or response sanitization. Unit: `auth_test.go` covers Execute happy path, token-writer interface, no-user rejection, allowlist rejection, revoke-on-send-failure, and nil dependency gating. | n/a | P0 |
| `subagent_dispatch` | PARTIAL | COVERED | E2E: `tool-subagent-dispatch` prompts `subagent_dispatch` and checks for the expected answer, but the current baseline stderr shows `tool_attempts subagent_dispatch ... count=0` and the verifier returns success through the infra-skip branch; ground truth does not prove successful Execute. Unit: `subagent_test.go` covers Execute for spawn, 3-node fanout, cap rejection, collect, mixed tiers, write-proposal allowlist, default allowlist, and partial collect errors. | true | P0 |
| `run_aurabot_swarm` | MISSING | COVERED | E2E: no `cmd/probe_chat` case prompts or verifies `run_aurabot_swarm`; `tool-swarm-lifecycle` covers `spawn_aurabot`, not the run-swarm tool. Unit: `swarm/tools_test.go:138`, `:181`, and `delegation_policy_test.go:175`, `:190`, `:205` cover Execute, capability enforcement, stale alias rejection, role validation, and delegation policy application. | n/a | P0 |
| `spawn_aurabot` | COVERED | COVERED | E2E: `tool-swarm-lifecycle` prompts `spawn_aurabot`, then list/read, and verifies a completed `swarm_tasks` DB row since probe start. Unit: `swarm/tools_test.go:99`, `:282`, and `:295` cover Execute, disallowed tool rejection, and lifecycle integration with list/read. | true | P2 |
| `list_swarm_tasks` | PARTIAL | COVERED | E2E: `tool-swarm-lifecycle` asks the model to call `list_swarm_tasks` and requires at least three tool calls, but it does not verify a `list_swarm_tasks` `tool_attempts` row or inspect the list output; DB ground truth only proves the spawned task completed. Unit: `swarm/tools_test.go:295` and `:355` directly cover `NewListSwarmTasksTool(...).Execute`. | true | P0 |
| `read_swarm_result` | PARTIAL | COVERED | E2E: `tool-swarm-lifecycle` asks the model to call `read_swarm_result` and requires at least three tool calls, but it does not verify a `read_swarm_result` `tool_attempts` row or inspect the read output; DB ground truth only proves the spawned task completed. Unit: `swarm/tools_test.go:295` and `:355` directly cover `NewReadSwarmResultTool(...).Execute`. | true | P0 |
