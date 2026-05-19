# Channel x Failure-Mode Coverage Matrix - 2026-05-19 - git_head: c3b1f008

Cross-reference of `docs/qa-channel-surface.md` against `docs/qa-failure-modes.md` and the current test suite. Scope: 4 channels x 18 failure modes = 72 cells. Live baseline `.planning/qa/baseline-run.json` was captured during this QA run and is 30/30 passing.

Coverage distribution: COVERED 4, PARTIAL 59, MISSING 0, N/A 9. Gap severity distribution: P0 0, P1 0, P2 72. COVERED rows have no open gap; P2 is used there only as the non-blocking severity bucket. PARTIAL is not green: it means adjacent unit/probe coverage exists, but the exact channel x failure-mode fault is not verified end-to-end.

Evidence convention: `file:line` points to a concrete test/probe body. "No channel fault injection" means lower-level coverage exists, but not the full channel path.

## Matrix

| channel | failure_mode | covered? | evidence | gap_severity |
|---|---|---|---|---|
| telegram | qdrant down (vector store) | PARTIAL | Tool degradation covered by `internal/agent/tools/registry/memory_search_test.go:921`; no Telegram adapter/probe injects qdrant failure. | P2 |
| telegram | embedding sidecar down | PARTIAL | Embedding transport failure covered by `internal/storage/search/embed_http_test.go:11`; no Telegram `search_memory` turn with sidecar down. | P2 |
| telegram | searxng down (web search) | PARTIAL | SearXNG HTTP error covered by `internal/agent/tools/registry/web_test.go:57`; no Telegram `web_search` outage turn. | P2 |
| telegram | garage S3 down | N/A | Garage backs backup/export routes, not the Telegram conversation channel in `qa-channel-surface.md`. | P2 |
| telegram | mistral OCR fail | PARTIAL | OCR success probe checks `ocr.md` in `cmd/probe_chat/cases.go:1041`; OCR HTTP 500 covered by `internal/storage/sources/ocr/client_test.go:147`; no Telegram OCR-fail turn. | P2 |
| telegram | OpenAI-compatible LLM hard fail | PARTIAL | OpenAI 500 covered by `internal/llm/openai_test.go:69`; Telegram stream retry adjacent at `internal/llm/retry_test.go:267`; no exhausted-failure Telegram UX assertion. | P2 |
| telegram | replicate image generation outage | N/A | `qa-failure-modes.md` says Replicate has no runtime client/tool at this SHA. | P2 |
| telegram | MCP server down | PARTIAL | MCP HTTP bootstrap failure covered by `internal/mcp/client_test.go:177`; no Telegram MCP tool outage turn. | P2 |
| telegram | context deadline / MaxElapsed hit | PARTIAL | Agent loop max-elapsed fallback covered by `internal/agent/loop_test.go:322`; no Telegram deadline-hit delivery assertion. | P2 |
| telegram | MaxIterations hit | PARTIAL | Agent loop cap fallback covered by `internal/agent/loop_test.go:267`; no Telegram cap-hit delivery assertion. | P2 |
| telegram | empty LLM response after tool call | PARTIAL | Loop fallback to last tool result covered by `internal/agent/loop_test.go:599`; no Telegram channel assertion. | P2 |
| telegram | malformed LLM tool-call args | PARTIAL | Classification covered by `internal/llm/classify_test.go:48`; stream argument recovery by `internal/llm/openai_test.go:238`; no Telegram parse-error UX. | P2 |
| telegram | sandbox unavailable | PARTIAL | Nil/unavailable sandbox covered by `internal/agent/tools/registry/exec_test.go:16` and `internal/sandbox/sandbox_test.go:68`; no Telegram disabled-sandbox turn. | P2 |
| telegram | capability deny | PARTIAL | Telegram grant assertions at `internal/telegram/bot_test.go:252`; unauthorized text drop at `internal/telegram/bot_test.go:256`; no tool-denial reply assertion. | P2 |
| telegram | phantom tool detection | PARTIAL | Loop correction covered by `internal/agent/phantom_guard_test.go:270`; Telegram pane e2e at `internal/channels/telegram/status_pane_e2e_test.go:160`; no Telegram ground-truth phantom trap. | P2 |
| telegram | duplicate tool call dedup | PARTIAL | Dedupe covered by `internal/agent/dedupe_test.go:9`; loop skip callback covered by `internal/agent/loop_test.go:624`; no Telegram duplicate side-effect check. | P2 |
| telegram | LLM 429 rate-limit | PARTIAL | Streaming 429 retry covered by `internal/llm/retry_test.go:267`; no full Telegram channel 429 run. | P2 |
| telegram | LLM transient network error | PARTIAL | Transient retry covered by `internal/llm/retry_test.go:11` and `internal/llm/client_test.go:55`; no Telegram UX assertion. | P2 |
| web | qdrant down (vector store) | PARTIAL | Tool degradation covered by `internal/agent/tools/registry/memory_search_test.go:921`; baseline has healthy web memory cases, but no `/api/chat` qdrant outage. | P2 |
| web | embedding sidecar down | PARTIAL | Embedding errors covered by `internal/storage/search/embed_http_test.go:11`; no `/api/chat` sidecar-down recall turn. | P2 |
| web | searxng down (web search) | PARTIAL | SearXNG error covered by `internal/agent/tools/registry/web_test.go:57`; no `/api/chat` web-search outage probe. | P2 |
| web | garage S3 down | N/A | Web channel in `qa-channel-surface.md` is `/api/chat`; Garage backup/export routes are outside that channel. | P2 |
| web | mistral OCR fail | PARTIAL | Web probe `tool-ocr-source` verifies success plus `ocr.md` ground truth at `cmd/probe_chat/cases.go:1041`; OCR 500 is unit-only at `internal/storage/sources/ocr/client_test.go:147`. | P2 |
| web | OpenAI-compatible LLM hard fail | PARTIAL | Web chat service propagates run errors at `internal/channels/web/chat_service_test.go:98`; no `/api/chat` LLM-outage HTTP assertion. | P2 |
| web | replicate image generation outage | N/A | No Replicate runtime client/tool is wired. | P2 |
| web | MCP server down | PARTIAL | MCP bootstrap failure covered by `internal/mcp/client_test.go:177`; no `/api/chat` MCP outage probe. | P2 |
| web | context deadline / MaxElapsed hit | PARTIAL | Healthy under-budget probe at `cmd/probe_chat/cases.go:1226` plus loop cap test at `internal/agent/loop_test.go:322`; no actual deadline-hit web turn. | P2 |
| web | MaxIterations hit | PARTIAL | Healthy multi-step probe at `cmd/probe_chat/cases.go:1174` plus loop cap test at `internal/agent/loop_test.go:267`; probe comments say it is not the cap-hit floor. | P2 |
| web | empty LLM response after tool call | PARTIAL | Loop fallback covered by `internal/agent/loop_test.go:599`; web service error plumbing adjacent at `internal/channels/web/chat_service_test.go:98`. | P2 |
| web | malformed LLM tool-call args | PARTIAL | LLM classify/parser coverage at `internal/llm/classify_test.go:48` and `internal/llm/openai_test.go:238`; no `/api/chat` malformed-toolcall probe. | P2 |
| web | sandbox unavailable | PARTIAL | Sandbox success probes check `tool_attempts` at `cmd/probe_chat/cases.go:829` and `cmd/probe_chat/cases.go:874`; unavailable path is unit-only. | P2 |
| web | capability deny | COVERED | `/api/chat` denial plus `authz_decisions` row covered by `internal/api/chat_test.go:104` and live probe `cmd/probe_chat/cases.go:1112`. | P2 |
| web | phantom tool detection | COVERED | `phantom-trap-nonexistent-task` uses `/api/chat`, scheduled-task DB ground truth, and passed baseline; see `cmd/probe_chat/cases.go:492`. | P2 |
| web | duplicate tool call dedup | PARTIAL | Dedupe covered by `internal/agent/dedupe_test.go:9` and loop callback skip at `internal/agent/loop_test.go:624`; no web duplicate side-effect probe. | P2 |
| web | LLM 429 rate-limit | PARTIAL | Send-path 429 retry explicitly guards `/api/chat` at `internal/llm/retry_test.go:224`; no live `/api/chat` 429 injection. | P2 |
| web | LLM transient network error | PARTIAL | Retry client transient tests at `internal/llm/retry_test.go:11` and `internal/llm/client_test.go:55`; no web endpoint transient-net probe. | P2 |
| cron | qdrant down (vector store) | PARTIAL | `search_memory` qdrant degradation covered by `internal/agent/tools/registry/memory_search_test.go:921`; no cron agent job with qdrant outage. | P2 |
| cron | embedding sidecar down | PARTIAL | Embedding outage covered by `internal/storage/search/embed_http_test.go:11`; no cron recall job with sidecar down. | P2 |
| cron | searxng down (web search) | PARTIAL | SearXNG error covered by `internal/agent/tools/registry/web_test.go:57`; no cron web-search outage job. | P2 |
| cron | garage S3 down | N/A | Cron agent/reminder dispatch does not transit Garage backup/export. | P2 |
| cron | mistral OCR fail | PARTIAL | OCR failure is covered below the channel at `internal/storage/sources/ocr/client_test.go:147`; no cron source/OCR job failure probe. | P2 |
| cron | OpenAI-compatible LLM hard fail | PARTIAL | Cron hub dispatcher propagates loop error at `internal/channels/cron/dispatcher_test.go:165`; scheduler persists generic dispatch failure at `internal/cron/scheduler_test.go:849`; not one LLM-outage E2E. | P2 |
| cron | replicate image generation outage | N/A | No Replicate runtime client/tool is wired. | P2 |
| cron | MCP server down | PARTIAL | MCP bootstrap failure covered by `internal/mcp/client_test.go:177`; no cron MCP job outage. | P2 |
| cron | context deadline / MaxElapsed hit | PARTIAL | Agent loop max-elapsed covered by `internal/agent/loop_test.go:322`; no cron silent job deadline-hit assertion. | P2 |
| cron | MaxIterations hit | PARTIAL | Loop cap covered by `internal/agent/loop_test.go:267`; no cron cap-hit assertion. | P2 |
| cron | empty LLM response after tool call | PARTIAL | Loop fallback covered by `internal/agent/loop_test.go:599`; no cron silent-output assertion. | P2 |
| cron | malformed LLM tool-call args | PARTIAL | LLM classify/parser coverage exists at `internal/llm/classify_test.go:48`; no cron malformed-toolcall job. | P2 |
| cron | sandbox unavailable | N/A | Scheduled-agent perimeter excludes sandbox tools; see `internal/cron/agent_job_test.go:99` and `internal/agent/tools/sets/toolsets_test.go:53`. | P2 |
| cron | capability deny | COVERED | Cron delegation denial and DB authz row covered by `internal/cron/dispatch_test.go:261`; missing identity path at `internal/channels/cron/dispatcher_test.go:215`. | P2 |
| cron | phantom tool detection | PARTIAL | Core loop phantom correction covered by `internal/agent/phantom_guard_test.go:270`; no cron silent run assertion. | P2 |
| cron | duplicate tool call dedup | PARTIAL | Core dedupe covered by `internal/agent/dedupe_test.go:9`; no cron duplicate side-effect test. | P2 |
| cron | LLM 429 rate-limit | PARTIAL | Retry behavior covered by `internal/llm/retry_test.go:224`; no cron 429 job. | P2 |
| cron | LLM transient network error | PARTIAL | Retry transient behavior covered by `internal/llm/retry_test.go:11`; no cron transient-net job. | P2 |
| swarm | qdrant down (vector store) | PARTIAL | `search_memory` qdrant degradation covered by `internal/agent/tools/registry/memory_search_test.go:921`; no swarm child qdrant outage. | P2 |
| swarm | embedding sidecar down | PARTIAL | Embedding outage covered by `internal/storage/search/embed_http_test.go:11`; no swarm child sidecar-down recall. | P2 |
| swarm | searxng down (web search) | PARTIAL | SearXNG error covered by `internal/agent/tools/registry/web_test.go:57`; no swarm child web-search outage. | P2 |
| swarm | garage S3 down | N/A | Swarm hot path does not transit Garage backup/export. | P2 |
| swarm | mistral OCR fail | PARTIAL | OCR failure covered below the channel at `internal/storage/sources/ocr/client_test.go:147`; no swarm child OCR-fail probe. | P2 |
| swarm | OpenAI-compatible LLM hard fail | PARTIAL | Manager runner error marks run/task failed at `internal/swarm/manager_test.go:355`; not actual LLM endpoint failure through child hub. | P2 |
| swarm | replicate image generation outage | N/A | No Replicate runtime client/tool is wired. | P2 |
| swarm | MCP server down | PARTIAL | MCP bootstrap failure covered by `internal/mcp/client_test.go:177`; no swarm MCP child outage. | P2 |
| swarm | context deadline / MaxElapsed hit | PARTIAL | Loop max-elapsed covered by `internal/agent/loop_test.go:322`; swarm `BudgetSecs` shape only appears in `internal/swarm/hub_bridge_test.go:93`. | P2 |
| swarm | MaxIterations hit | PARTIAL | Loop cap covered by `internal/agent/loop_test.go:267`; swarm `max_iterations` dispatch shape only at `internal/swarm/hub_bridge_test.go:93`. | P2 |
| swarm | empty LLM response after tool call | PARTIAL | Loop fallback covered by `internal/agent/loop_test.go:599`; no swarm child empty-response collection test. | P2 |
| swarm | malformed LLM tool-call args | PARTIAL | LLM classify/parser coverage exists at `internal/llm/classify_test.go:48`; no swarm child malformed-toolcall test. | P2 |
| swarm | sandbox unavailable | PARTIAL | Sandbox unavailable is unit-covered at `internal/agent/tools/registry/exec_test.go:16`; no swarm child with sandbox outage. | P2 |
| swarm | capability deny | COVERED | Swarm child denial escalates and records `authz_decisions` at `internal/swarm/parent_child_integration_test.go:33`; manager missing grant path at `internal/swarm/manager_test.go:275`. | P2 |
| swarm | phantom tool detection | PARTIAL | Core phantom correction covered by `internal/agent/phantom_guard_test.go:270`; no swarm child phantom-trap collection test. | P2 |
| swarm | duplicate tool call dedup | PARTIAL | Core dedupe covered by `internal/agent/dedupe_test.go:9`; no swarm duplicate side-effect test. | P2 |
| swarm | LLM 429 rate-limit | PARTIAL | Retry behavior covered by `internal/llm/retry_test.go:224`; no swarm child 429 injection. | P2 |
| swarm | LLM transient network error | PARTIAL | Retry transient behavior covered by `internal/llm/retry_test.go:11`; no swarm child transient-net injection. | P2 |

## Key Findings

- Direct channel x failure coverage is narrow: web capability-deny, web phantom-trap, cron capability-deny, and swarm capability-deny.
- Most cells are PARTIAL because lower-level tests now exist for qdrant, embedding, SearXNG, OCR, MCP, LLM retry, loop budget, phantom, dedupe, and sandbox behavior, but channel-specific fault injection is still absent.
- The current stored live baseline is healthy at 30/30 pass, but it mostly exercises happy paths plus web capability/phantom/budget-adjacent probes.
