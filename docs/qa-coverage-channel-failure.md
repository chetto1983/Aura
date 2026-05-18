# Channel x Failure-Mode Coverage Matrix - 2026-05-18 - git_head: 3652fdf2

Cross-references docs/qa-channel-surface.md (4 channels: telegram, web, cron, swarm) against docs/qa-failure-modes.md (7 external + 10 internal = 17 failure modes) and the existing test suite. Total cells = 68 (4 x 17). Baseline probe state from .planning/qa/baseline-run.json: 18/21 pass, 3 fail (2 infra, 1 product candidate). Lint baseline (.planning/qa/lint-baseline.json): 59 issues, none directly intersecting these failure paths.

Distribution: COVERED 4, PARTIAL 14, MISSING 31, N/A 12, STATIC-INSUFFICIENT 7. P0 gaps: 19. P1 gaps: 26. P2 gaps: 19.

Evidence convention: file:line targets a specific test or production check; "none" means a content grep found no test that exercises the cell.

## Matrix

| channel | failure_mode | covered? | evidence | gap_severity |
|---|---|---|---|---|
| telegram | qdrant down (vector store) | MISSING | none - no telegram test injects a qdrant 5xx/timeout; static internal/storage/qdrant/client_test.go exercises HTTP shape only | P1 |
| telegram | embedding sidecar down | MISSING | none - embed_*_test.go are unit; no telegram-channel test induces embed failure | P1 |
| telegram | searxng down (web search) | MISSING | none - internal/agent/tools/registry/web_test.go has no 5xx/429 path | P1 |
| telegram | garage S3 down | N/A | telegram path does not depend on garage at call time (backup is operator-side) | P2 |
| telegram | mistral OCR fail | STATIC-INSUFFICIENT | flagged by qa-failure-modes; no telegram-side probe of chat reply wording when OCR fails | P0 |
| telegram | OpenAI-compat LLM hard fail | PARTIAL | internal/llm/retry_test.go:12-54 (transient bucket); internal/channels/telegram/chat_client.go:59-63 has canned-fallback but no test asserts Telegram-message UX after exhaustion | P0 |
| telegram | replicate (image gen) | N/A | not wired in tree per failure-modes section replicate | P2 |
| telegram | MCP server down | STATIC-INSUFFICIENT | flagged by qa-failure-modes; internal/agent/executor_test.go covers not-allowed tool path but no telegram E2E for MCP-popular-down phrasing | P0 |
| telegram | MaxElapsed (context deadline mid-turn) | STATIC-INSUFFICIENT | internal/agent/loop_test.go:322-340 covers loop-level MaxElapsedHit but reply-vs-Telegram UX not asserted; flagged in qa-failure-modes | P0 |
| telegram | MaxIterations hit | STATIC-INSUFFICIENT | internal/agent/loop_test.go:276-300 covers loop stat; no telegram-channel test asserts wrap-up wording; flagged in qa-failure-modes | P0 |
| telegram | empty LLM response after tool | PARTIAL | internal/llm/classify_test.go:42-46 (ErrEmptyOutput to Content); internal/agent/loop_test.go:475 skip-empty-delta - but no end-to-end telegram assertion | P0 |
| telegram | LLM tool-call arg parse error | PARTIAL | internal/llm/classify_test.go:48-52 (ErrMalformedToolCall to Content); no telegram E2E verifying canned fallback reaches user | P0 |
| telegram | sandbox unavailable | PARTIAL | internal/agent/tools/registry/exec_test.go:16-21 (NilManager returns nil tool); no telegram-channel test injecting nil sandbox and verifying graceful explanation | P0 |
| telegram | capability deny (identity authorizer) | PARTIAL | internal/telegram/bot_test.go:253-256 asserts skills.install deny path at DB level; no test asserts user-visible reply text | P0 |
| telegram | phantom tool detection | COVERED | internal/agent/phantom_guard_test.go (positives/negatives); internal/channels/telegram/status_pane_e2e_test.go:171-290 exercises Phantom-guard text round-trip on Telegram pane; probe cmd/probe_chat/cases.go:484-519 (phantom-trap-nonexistent-task) - passes baseline | P2 |
| telegram | duplicate tool call (dedup) | PARTIAL | internal/agent/dedupe_test.go:9-39 covers in-batch and cross-iteration dedup; no telegram-channel test verifying user never sees the stub | P2 |
| telegram | LLM 429 rate-limit | STATIC-INSUFFICIENT | internal/llm/retry_test.go:12-54 and classify_test.go:54-58 cover bucket; flagged by qa-failure-modes for worst-case wall-clock on Telegram (no test) | P0 |
| telegram | LLM transient net error | PARTIAL | internal/llm/classify_test.go:30-34 (DeadlineExceeded to Transient); internal/llm/retry_test.go retry budget; no telegram-channel UX assertion | P0 |
| web | qdrant down | MISSING | none - internal/api/chat_test.go mocks Chat service; no qdrant-fault injection at web layer | P1 |
| web | embedding sidecar down | MISSING | none - same: web layer never sees embed errors in tests | P1 |
| web | searxng down | MISSING | none | P1 |
| web | garage S3 down | MISSING | internal/api/backups_test.go exists but tests success path; no down-injection at web /api/maintenance layer for chat path | P2 |
| web | mistral OCR fail | STATIC-INSUFFICIENT | flagged in qa-failure-modes; web upload-then-OCR-fail status visible in dashboard, but no test asserts the dashboard-side error rendering | P0 |
| web | OpenAI-compat LLM hard fail | PARTIAL | internal/llm/retry_test.go covers retry; internal/channels/web/chat_service_test.go:105 injects EventError into hub events - verifies buffer, not HTTP 200-with-error contract from chat.go:49 | P0 |
| web | replicate | N/A | not wired | P2 |
| web | MCP server down | STATIC-INSUFFICIENT | flagged | P0 |
| web | MaxElapsed mid-turn | STATIC-INSUFFICIENT | flagged; no web test asserts ChatReply on deadline | P0 |
| web | MaxIterations hit | MISSING | chat_service_test.go has no MaxIter scenario; loop unit tests do not cover ChatReply marshalling | P0 |
| web | empty LLM response after tool | PARTIAL | classify unit tests; no /chat HTTP-level test for empty-reply tail | P0 |
| web | LLM tool-call arg parse error | PARTIAL | classify unit tests; no /chat HTTP-level test | P0 |
| web | sandbox unavailable | MISSING | exec_test.go unit only; no web /chat test that disables sandbox and asserts reply | P0 |
| web | capability deny | COVERED | internal/api/chat_test.go:102-119 TestChatBearerMissingGrantDenied + :89-100 TestChatBearerRejectsBodyUserOverride; asserts HTTP 403 and DB authz_decisions row | P2 |
| web | phantom tool detection | PARTIAL | phantom_guard_test.go covers guard, but no web /chat HTTP test for phantom-corrected reply path | P1 |
| web | duplicate tool call (dedup) | PARTIAL | dedupe_test.go covers algorithm; no /chat HTTP assertion | P2 |
| web | LLM 429 | STATIC-INSUFFICIENT | flagged; retry_test.go only. ChatReply elapsed-ms behavior under 429 burst untested | P0 |
| web | LLM transient net | PARTIAL | unit-only | P0 |
| cron | qdrant down | MISSING | none - internal/channels/cron/dispatcher_test.go does not exercise vector failures | P1 |
| cron | embedding sidecar down | MISSING | none | P1 |
| cron | searxng down | MISSING | none | P1 |
| cron | garage S3 down | N/A | cron path itself does not transit garage on the hot loop | P2 |
| cron | mistral OCR fail | N/A | cron does not upload PDFs; OCR is source-pipeline | P2 |
| cron | OpenAI-compat LLM hard fail | PARTIAL | dispatcher_test.go:165-177 TestHubDispatcher_AgentJob_PropagatesLoopError covers error-propagation when loop fails; does not assert silent-adapter warn log content | P1 |
| cron | replicate | N/A | not wired | P2 |
| cron | MCP server down | STATIC-INSUFFICIENT | flagged | P0 |
| cron | MaxElapsed mid-turn | MISSING | no cron test sets short MaxElapsed and verifies silent log | P1 |
| cron | MaxIterations hit | MISSING | no cron-side cap-hit assertion | P1 |
| cron | empty LLM response after tool | MISSING | none | P1 |
| cron | LLM tool-call arg parse error | MISSING | none | P1 |
| cron | sandbox unavailable | MISSING | none | P1 |
| cron | capability deny | COVERED | dispatcher_test.go:215-231 TestCronAgentLoopRequiresIdentityDelegation asserts ErrUnauthorized when identity missing; :179-213 happy-path verifies delegated capability_grants row | P2 |
| cron | phantom tool detection | N/A | cron messages are silent-mode; phantom-guard runs in loop but user-visible Telegram-edit-pipeline UX described in qa-failure-modes is telegram-specific | P2 |
| cron | duplicate tool call (dedup) | PARTIAL | algorithm covered by dedupe_test.go; no cron-channel test | P2 |
| cron | LLM 429 | MISSING | none - retry_test.go does not run via cron-loop adapter | P1 |
| cron | LLM transient net | PARTIAL | same as 429: covered at retry layer but not at cron adapter | P1 |
| swarm | qdrant down | MISSING | swarm/hub_e2e_test.go uses captureLoop - no qdrant call | P1 |
| swarm | embedding sidecar down | MISSING | none | P1 |
| swarm | searxng down | MISSING | none | P1 |
| swarm | garage S3 down | N/A | swarm hot path does not transit garage | P2 |
| swarm | mistral OCR fail | N/A | swarm dispatches subagents; OCR is upload-side | P2 |
| swarm | OpenAI-compat LLM hard fail | PARTIAL | manager_test.go fakeRunner.err propagates agent.Result error; no test verifies silent/outbound.go warn log content | P1 |
| swarm | replicate | N/A | not wired | P2 |
| swarm | MCP server down | MISSING | none | P1 |
| swarm | MaxElapsed mid-turn | MISSING | no swarm BudgetSecs-exhaustion test asserts run.LastError surface | P1 |
| swarm | MaxIterations hit | MISSING | none | P1 |
| swarm | empty LLM response after tool | MISSING | none | P1 |
| swarm | LLM tool-call arg parse error | MISSING | none | P1 |
| swarm | sandbox unavailable | MISSING | none | P1 |
| swarm | capability deny | COVERED | internal/chat/hub_swarm_test.go:171-205 and :25 DecisionDeny path asserts EventError(swarm_dispatch_denied) + RunStatusFailed + authz_decisions row; internal/agent/tools/swarm/tools_test.go:205 swarm.spawn deny | P2 |
| swarm | phantom tool detection | N/A | swarm subagent reply is harvested as final_text, not streamed via Telegram-edit pipeline; phantom-guard runs inside child loop but qa-failure-modes describes UX-specific impact | P2 |
| swarm | duplicate tool call (dedup) | PARTIAL | algorithm covered; no swarm channel test | P2 |
| swarm | LLM 429 | MISSING | none | P1 |
| swarm | LLM transient net | MISSING | none | P1 |

## Count summary

- Total cells: 68 (4 channels x 17 modes)
- COVERED: 4 (telegram/phantom, web/capability-deny, cron/capability-deny, swarm/capability-deny)
- PARTIAL: 14
- MISSING: 31
- N/A: 12
- STATIC-INSUFFICIENT (carry-forward from Phase 1): 7
- P0 gaps: 19
- P1 gaps: 26
- P2 gaps: 19

## STATIC-INSUFFICIENT cells inventory

The 7 cells inherited from Phase-1 (qa-failure-modes.md) auto-promoted to P0. Each requires a live container probe.

| cell | live-probe approach |
|---|---|
| telegram x qdrant-down (verbalization) | docker compose stop qdrant; via probe_chat send a memory-search prompt; capture reply; assert presence/absence of memory failure verbiage. Ground truth: scheduled_tasks/no qdrant calls in logs. |
| telegram x embedding-sidecar-down | docker compose stop aura-llama-embed; send memory-search prompt; assert reply degrades to raw-conversation context with no citations; check structured logs for embeddings-request errors. |
| telegram x mistral-OCR-fail | Upload PDF with MISTRAL_API_KEY=invalid via runtime settings; assert chat-side ingestion-failed message wording; verify raw bytes persisted under wiki/raw/src_HEX/ despite OCR failure. |
| telegram x MCP-server-down | Kill a popular MCP server (e.g. wikipedia); ask for its tool; capture reply phrasing; assert no phantom claim. |
| telegram x MaxElapsed-mid-turn | Set AURA_AGENT_LOOP_MAX_ELAPSED=10s runtime; trigger long web research; assert reply text matches interruptedAssistantContent template OR finalAnswerOnBudget recap; record wall-clock to user. |
| telegram x MaxIterations-hit | Set AURA_AGENT_LOOP_MAX_STEPS=3 runtime; trigger multi-tool prompt; assert wrap-up acknowledges incomplete state (no false-complete). |
| telegram x LLM-429-rate-limit | Inject a proxy returning 429 for first 3 attempts then 200; assert user-visible delay <=30s and final answer; alternatively mock LLM_BASE_URL to permanent 429 to time canned-fallback path. |

## Key findings

- **Capability-deny is the only failure mode with end-to-end coverage on three channels** (web, cron, swarm). Telegram capability-deny is only partial - bot_test.go:253-256 asserts DB row but not the user-visible silent-drop UX.
- **No channel has any test for qdrant/embedding/searxng outages.** All 12 cells across 4 channels are MISSING. The retrieval-degradation contract documented in qa-failure-modes is unverified anywhere.
- **All MaxIterations/MaxElapsed/empty-response/malformed-toolcall failure paths stop at the unit-test boundary in internal/agent/loop_test.go and internal/llm/classify_test.go.** No channel-level adapter test asserts what the user actually sees when these fire.
- **The probe_chat suite has zero negative-fault cases beyond phantom-trap-nonexistent-task.** All other 20 cases assume external deps are healthy. The 3 baseline failures are infra (2x phase07d/e fixture refusal) and 1 product candidate (agent-note DB row missing) - none exercise the channel x failure-mode surface.
- **Cron and swarm have the weakest coverage** (10 MISSING each) because their outbound is silent-mode; nothing tests the operator-log contract documented in internal/channels/silent/outbound.go:66. Failures there are completely invisible until production.
