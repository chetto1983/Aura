# Phase-CONS Benchmark Contract

**Role:** benchmark
**Status:** self-audited planning repair, 2026-05-24
**Rule:** Smoke checks are prechecks only. A story passes only when its benchmark asserts ground truth from captured LLM requests, SQLite rows, API responses, SSE frames, filesystem artifacts, or rendered UI state.

## Global Slice Gates

Run these for every Go backend slice unless the story narrows them further:

| Gate | Command | Pass Threshold |
| --- | --- | --- |
| Narrow tests | `go test ./cmd/aura ./internal/channels/web ./internal/chat -count=1` | Pass |
| Shared build/vet | `go vet ./...` and `go build ./...` | Pass |
| Touched-path lint | `golangci-lint run <touched packages> --timeout=10m --new-from-rev=HEAD` | 0 new findings |
| Duplicate check | `dupl -t 60 <touched go files/dirs>` | 0 clone groups in touched production code |
| LOC cap | existing lefthook/file-size gate | Every touched file <=600 LOC |
| Patch hygiene | `git diff --check` | Pass |

For frontend Wave B slices:

| Gate | Command | Pass Threshold |
| --- | --- | --- |
| Web build | `npm --prefix web run build` | Pass |
| Browser/runtime probe | Playwright or Browser against local `/chat` | Target UI state visible and usable |

## US-CONS-02 - Web Session/State Collapse

### B-CONS-02-A: Existing Web Bridge Precheck

- **Command:** `go test ./cmd/aura ./internal/channels/web -run "TestHubBackedWebChat|TestChatService" -count=1`
- **Fixture:** existing fake LLM/fake hub tests.
- **Artifact:** `go test` output.
- **Ground truth:** existing run persistence, tool attempt persistence, terminal text_response behavior, and web bridge event folding still pass.
- **Pass threshold:** all selected tests pass.
- **PRD gate:** `/api/chat` remains a compat wrapper over Chat Hub.
- **Classification:** precheck, not completion evidence.

### B-CONS-02-B: Thread Context Retention + Token Accounting

- **Command:** `go test ./cmd/aura -run TestHubBackedWebChatUsesSessionStoreContext -count=1`
- **Fixture to implement:** fake LLM that records every `llm.Request.Messages` and returns deterministic assistant text plus token usage across three turns with the same `userID` and `threadID`.
- **Artifact:** captured request messages inside the test plus `SessionStore.Load("web:<userID>:<threadID>")`.
- **Ground truth:** third request contains evidence from turns 1 and 2, and `conversation.Context.TotalTokensUsed()` equals the sum of fake token usage from all completed turns.
- **Pass threshold:** exact string containment for retained turn-1/turn-2 assistant content; exact token total.
- **PRD gate:** web and Telegram share conversation discipline while threads remain per-channel.

### B-CONS-02-C: Thread Isolation

- **Command:** `go test ./cmd/aura -run TestHubBackedWebChatSessionStoreScopesThreads -count=1`
- **Fixture to implement:** two web threads for the same user, then another turn on the first thread.
- **Artifact:** captured request messages and `SessionStore.Load` for both keys.
- **Ground truth:** `web:alice:thread-a` retains only thread-a history; `web:alice:thread-b` retains only thread-b history.
- **Pass threshold:** no cross-thread content in captured request messages.
- **PRD gate:** threads stay per-channel and per-thread.

### B-CONS-02-D: Tool Result Compaction Visible On Next Prompt

- **Command:** `go test ./cmd/aura -run TestHubBackedWebChatCompactsToolResultsBeforeNextTurn -count=1`
- **Fixture to implement:** fake tool call path adds a completed tool result larger than `MaxToolResultChars`; run another turn with the same web session.
- **Artifact:** next-turn captured `llm.Request.Messages`.
- **Ground truth:** older completed tool-result message is compacted with the standard compaction marker and its content length is <=1200 chars, while the most recent full tool result policy is preserved.
- **Pass threshold:** exact marker present; max compacted tool message length <=1200; no orphan tool-result without its assistant tool call.
- **PRD gate:** web gets the same context compaction discipline as Telegram.

### B-CONS-02-E: Removed Dead Code

- **Command:** `rg -n "webChatSessions|webAgentState|trimWebMessages|webChatIdleTTL|webChatMaxMessages" cmd/aura internal`
- **Fixture:** source tree after US-CONS-02 patch.
- **Artifact:** `rg` output.
- **Ground truth:** removed type/helper names do not remain in production code; tests reference only the replacement contract.
- **Pass threshold:** no production matches except commit/history text outside source tree.
- **PRD gate:** one module per slice includes dead-code removal.

## US-CONS-03 - Web Tool Executor Collapse

### B-CONS-03-A: Shared Executor Options

- **Command:** `go test ./internal/agent -run "TestExecuteToolCalls(SuccessPath|ErrorPropagation|SummaryAggregation|UsesConversationIDOption|AppliesToolTimeout|RecordsToolAttempt)$" -count=1`
- **Fixture:** fake `ToolRunner` capturing `AllowedToolNamesFromContext`, `UserIDFromContext`, `ConversationIDFromContext`, search args, and a blocking runner for timeout.
- **Artifact:** captured context fields and `tool_attempts` SQLite row.
- **Ground truth:** `WithConversationID` overrides the chatID fallback without losing search `chat_id`; `WithToolTimeout` returns a deadline error through the shared result path; tool attempts still record argument keys.
- **Pass threshold:** exact captured `userID`, `conversationID`, allowed tools, `chat_id`, and timeout error text.
- **PRD gate:** web can reuse the shared executor without losing web-visible tool context.

### B-CONS-03-B: Web Shared Executor Wiring

- **Command:** `go test ./cmd/aura -run "TestHubBackedWebChat(RecordsToolAttempts|TerminatesOnTextResponseTool|UsesSessionStoreContext|SessionStoreScopesThreads|CompactsToolResultsBeforeNextTurn)$|TestExtractLastTextResponseArg" -count=1`
- **Fixture:** hub-backed web chat with fake LLM and `context_probe` tool.
- **Artifact:** captured probe fields plus SQLite `tool_attempts`.
- **Ground truth:** web dispatch calls the shared executor; the tool sees the visible allowlist, `userID=alice`, `conversationID=web:alice:default`, a non-empty `runID`, and the `tool_attempts` row is linked to the web run.
- **Pass threshold:** all captured fields match exactly and exactly one `tool_attempts` row exists for `context_probe`.
- **PRD gate:** web and Telegram share tool execution discipline.

### B-CONS-03-C: Removed Web Executor Dead Code

- **Command:** `rg -n "webToolExecutor|web_chat_executor|TestWebToolExecutor|cleanWebToolList|cleanToolList\(" cmd/aura internal/agent`
- **Fixture:** source tree after US-CONS-03 patch.
- **Artifact:** `rg` output.
- **Ground truth:** stale web executor symbols, stale direct executor tests, and duplicated web tool-list helper are absent from source.
- **Pass threshold:** no matches; `rg` exits 1.
- **PRD gate:** one module per slice includes dead-code removal.

### B-CONS-03-D: Dedicated Slice QA

- **Command:** `go test ./internal/agent/... ./cmd/aura/... -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/agent --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/agent/exec_helpers.go internal/agent/exec_helpers_test.go internal/agent/runtask.go internal/agent/runtask_helpers.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after replacing the web executor.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-04A - Agentcore Builder Web Adoption

### B-CONS-04A-A: Builder Contract

- **Command:** `go test ./internal/agentcore -count=1`
- **Fixture:** fake `agent.ChatClient`, `agent.ToolExecutor`, and `agent.State`.
- **Artifact:** built `agent.Invocation` and copied prompt/tool metadata.
- **Ground truth:** required runtime fields are rejected when missing; prompt modules and tool definitions are copied so caller mutations cannot alter an already-built invocation.
- **Pass threshold:** exact errors for missing client/executor/state and exact copied metadata.
- **PRD gate:** `internal/agentcore.Builder` exists as the channel-neutral invocation assembly owner.

### B-CONS-04A-B: Web Uses Agentcore Assembly

- **Command:** `go test ./cmd/aura -run "TestHubBackedWebChat|TestExtractLastTextResponseArg" -count=1`
- **Fixture:** existing hub-backed web tests with fake LLM, fake tool registry, terminal `text_response`, and session-store retention.
- **Artifact:** API replies, captured LLM requests, session store state, and SQLite rows from the tests.
- **Ground truth:** web behavior is unchanged while `cmd/aura/web_chat.go` obtains its `agent.Invocation` from `agentcore.Builder`.
- **Pass threshold:** all selected web tests pass.
- **PRD gate:** first transport moved to the shared builder without widening runtime behavior.

### B-CONS-04A-C: Dedicated Slice QA

- **Command:** `go test ./internal/agentcore ./cmd/aura ./internal/agent ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./internal/agentcore ./cmd/aura --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 internal/agentcore cmd/aura/web_chat.go cmd/aura/web_chat_test.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after web adopts agentcore assembly.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-04B - Telegram Agentcore Builder Adoption

### B-CONS-04B-A: Telegram Builder Wiring

- **Command:** `go test ./internal/channels/telegram ./internal/channels/telegram/fixture -count=1`
- **Fixture:** existing Telegram adapter and byte-parity fixture tests.
- **Artifact:** fixture test output and Telegram adapter unit output.
- **Ground truth:** Telegram behavior remains stable while `internal/channels/telegram/invocation_builder.go` obtains its `agent.Invocation` from `agentcore.Builder`.
- **Pass threshold:** all selected Telegram tests pass.
- **PRD gate:** second transport moved to the shared builder without changing transport-only hooks.

### B-CONS-04B-B: Dedicated Slice QA

- **Command:** `go test ./internal/channels/telegram/... ./internal/agentcore ./internal/chat ./cmd/aura -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./internal/channels/telegram ./internal/agentcore --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 internal/channels/telegram/invocation_builder.go internal/agentcore`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after Telegram adopts agentcore assembly.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-04C - Agentcore Adoption Closure Gate

### B-CONS-04C-A: Web And Telegram Builder Adoption Gate

- **Command:** `go test ./internal/agentcore -run "TestAgentCoreBuilderAdoptedByWebAndTelegram|TestAgentCoreBuilderDoesNotAddRuntimeFlagShim" -count=1`
- **Fixture:** source-level parser test over `cmd/aura/web_chat.go`, `internal/channels/telegram/invocation_builder.go`, and runtime/config files.
- **Artifact:** parsed Go source and test output.
- **Ground truth:** both transports call `agentcore.Builder.Build` with `agentcore.InvocationInput`; neither transport owns a non-empty `agent.Invocation` literal; runtime/config files do not add a no-op `AURA_AGENTCORE_BUILDER` shim after the legacy path was removed.
- **Pass threshold:** exact source assertions pass.
- **PRD gate:** `internal/agentcore.Builder` is the single invocation assembly owner for web and Telegram.

### B-CONS-04C-B: Telegram Fixture Byte-Parity Closure

- **Command:** `go test ./internal/channels/telegram/fixture -run TestSnapshotsByteParity -count=1`
- **Fixture:** Telegram fixture snapshot suite.
- **Artifact:** fixture test output.
- **Ground truth:** Telegram rendering bytes remain stable after invocation construction moved behind `agentcore.Builder`.
- **Pass threshold:** byte-parity test passes.
- **PRD gate:** Telegram transport behavior did not drift while closing the builder migration.

### B-CONS-04C-C: Dedicated Slice QA

- **Command:** `go test ./internal/agentcore ./internal/channels/telegram/fixture ./cmd/aura ./internal/channels/telegram -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./internal/agentcore ./internal/channels/telegram ./cmd/aura --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 internal/agentcore internal/channels/telegram/invocation_builder.go cmd/aura/web_chat.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after closing US-CONS-04.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-05 - Shared Web And Telegram Hub

### B-CONS-05-A: Channel-Scoped Shared Hub Routing

- **Command:** `go test ./internal/chat ./cmd/aura ./internal/channels/web ./internal/channels/telegram -run "TestHub|TestInbound|TestChatService" -count=1`
- **Fixture:** shared Hub with fake web and Telegram outbound adapters, web inbound adapter, and hub-backed web chat tests.
- **Artifact:** delivered `chat.OutboundEvent` slices and web normalized `chat.InboundMessage`.
- **Ground truth:** `ChannelWeb` dispatches reach only web outbound; `ChannelTelegram` dispatches reach only Telegram outbound; web requests are normalized by `webadapter.New()` with `web:<userID>:<threadID>` scoping.
- **Pass threshold:** zero cross-channel deliveries; exact web thread ID and payload assertions pass.
- **PRD gate:** web and Telegram share one user-facing Hub while staying channel-scoped.

### B-CONS-05-B: Removed Duplicate Hub Constructors

- **Command:** `rg -n "newHubBackedWebChatService|telegramadapter\\.NewHub|func NewHub|injected by NewHub" cmd/aura internal/channels/telegram internal/channels/web`
- **Fixture:** source tree after US-CONS-05 patch.
- **Artifact:** `rg` output.
- **Ground truth:** web and Telegram no longer create separate user-facing Hubs.
- **Pass threshold:** no matches; `rg` exits 1.
- **PRD gate:** the THREE-Hub anomaly is reduced to shared web+Telegram plus separate cron.

### B-CONS-05-C: Dedicated Slice QA

- **Command:** `go test ./cmd/aura ./internal/channels/web ./internal/channels/telegram ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/chat ./internal/channels/web ./internal/channels/telegram --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/chat_hub.go cmd/aura/app_wire.go cmd/aura/main.go cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/channels/web internal/channels/telegram/invocation_builder.go internal/chat/hub_test.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after sharing the Hub.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-06 - Web Soft-Budget, Compaction, And Archive Parity

### B-CONS-06-A: Web Soft-Budget Reply Field

- **Command:** `go test ./cmd/aura ./internal/api ./internal/channels/web -run "TestHubBackedWebChatReportsSoftBudgetWarning|TestChatReplyIncludesBudgetWarning|TestChatService" -count=1`
- **Fixture:** fake web LLM with token usage, shared budget tracker with tiny soft budget, buffered web router, and API JSON handler.
- **Artifact:** `api.ChatReply` / `webadapter.ChatReply` values and JSON response body.
- **Ground truth:** `budget_warning` is non-empty once the shared budget runtime crosses the soft threshold; the JSON field is emitted to the API response.
- **Pass threshold:** exact non-empty warning assertions pass.
- **PRD gate:** web surfaces the same soft-budget warning behavior Telegram already sends.

### B-CONS-06-B: Web Archive Rows

- **Command:** `go test ./cmd/aura ./internal/conversation ./internal/db/migrations -run "TestHubBackedWebChatArchivesConversationTurns|TestArchiveStore_AppendPreservesChannel|TestArchiveStore_AppendAndList" -count=1`
- **Fixture:** isolated SQLite DB, current migrations, `conversation.ArchiveStore`, and hub-backed web chat.
- **Artifact:** `conversations` rows.
- **Ground truth:** web turns append user and assistant rows with `channel='web'`; legacy/default archive writes still read back as `channel='telegram'`.
- **Pass threshold:** exact row counts and channel values pass.
- **PRD gate:** web turns are durable archive facts, not transient HTTP replies only.

### B-CONS-06-C: Dedicated Slice QA

- **Command:** `go test ./cmd/aura ./internal/channels/web ./internal/api ./internal/conversation ./internal/db/migrations ./internal/chat -count=1`; `npm --prefix web run build`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/api ./internal/chat ./internal/channels/web ./internal/conversation ./internal/db/migrations --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/chat_hub.go cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/api/chat.go internal/api/chat_test.go internal/api/conversations.go internal/api/conversations_test.go internal/channels/web/chat_service.go internal/channels/web/chat_service_test.go internal/channels/web/outbound.go internal/chat/agentloop.go internal/conversation/archive.go internal/conversation/archive_internal_test.go internal/conversation/archive_test.go internal/conversation/archive_turns.go internal/db/migrations/m01_create_current_schema.go internal/db/migrations/migrations_test.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint/web-build regressions; touched-file duplication is zero; full Go suite passes after web budget/archive parity.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

## US-CONS-07 - Web SSE UI Message Stream

### B-CONS-07-A: Web Streaming Adapter Frames

- **Command:** `go test ./internal/channels/web -run "Test(StreamRouter|Inbound)" -count=1`
- **Fixture:** `StreamRouter` with an in-memory writer, live `llm.Token` channel, tool start/end events, and duplicate aggregated `EventMessageDelta`.
- **Artifact:** emitted SSE bytes parsed as JSON frames.
- **Ground truth:** frames include `start`, `text-start`, two live `text-delta` chunks with both `textDelta` and `delta`, `text-end`, `tool-call-start`, `tool-call-end`, `tool-result`, `finish`, and literal `data: [DONE]`.
- **Pass threshold:** parsed frame types and payload fields match exactly; duplicate aggregated final content is not emitted as a third `text-delta`; duplicate active thread returns `ErrStreamAlreadyActive`.
- **PRD gate:** web streaming is a channel adapter concern, not a second agent loop.

### B-CONS-07-B: API Stream Contract

- **Command:** `go test ./internal/api -run "TestChatStream|TestChatReplyIncludesBudgetWarning" -count=1`
- **Fixture:** fake `ChatStreamService`, legacy `{message, thread_id}` body, assistant-ui `{messages, threadId}` body, and unavailable-service negative path.
- **Artifact:** `httptest.ResponseRecorder` status, headers, body, and fake service capture.
- **Ground truth:** `POST /chat/stream` sets `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache, no-transform`, `X-Accel-Buffering: no`, `x-vercel-ai-ui-message-stream: v1`; assistant-ui message arrays resolve the latest user text; unavailable service returns 503.
- **Pass threshold:** exact header values, exact captured user/thread/message fields, `[DONE]` terminator present, 503 negative path passes.
- **PRD gate:** `/api/chat/stream` becomes Wave B's backend runtime contract while `/api/chat` remains compatible.

### B-CONS-07-C: Hub-Backed Streaming Service

- **Command:** `go test ./cmd/aura -run "TestWebChatStream|TestHubBackedWebChat" -count=1`
- **Fixture:** shared web+Telegram Hub with fake streaming LLM, in-memory SSE writer, and SQLite run store.
- **Artifact:** captured LLM request, SSE bytes, flush count, and `runs` row.
- **Ground truth:** streaming web turns call `llm.Client.Stream` (not `Send`), include the user message in the LLM request, emit token frames as `text-delta`, terminate with `[DONE]`, and persist a completed `runs` row for `thread_id='web:alice:default'`.
- **Pass threshold:** `streamCalled=true`, `sendCalled=false`, exactly two text-delta frames for `Hel`/`lo`, completed run row count is 1.
- **PRD gate:** shared Hub supports web streaming mode without regressing buffered web chat.

### B-CONS-07-D: Dedicated Slice QA

- **Command:** `go test ./cmd/aura ./internal/channels/web ./internal/api ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/api ./internal/channels/web ./internal/chat --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/chat_hub.go cmd/aura/web_chat.go cmd/aura/web_chat_stream.go cmd/aura/web_chat_test.go internal/api/chat.go internal/api/chat_stream.go internal/api/chat_test.go internal/api/router.go internal/channels/web/chat_client.go internal/channels/web/chat_service.go internal/channels/web/inbound.go internal/channels/web/inbound_test.go internal/channels/web/sse_chat_client.go internal/channels/web/streaming_outbound.go internal/channels/web/streaming_outbound_test.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages plus full repository Go test suite.
- **Artifact:** command outputs in this slice run.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; full Go suite passes after adding web streaming.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups` for touched files.
- **PRD gate:** self-audited slice QA before atomic commit.

### B-CONS-08-A: Web Voice Audio Cache And Endpoint

- **Command:** `go test ./internal/api -run "TestChatVoiceAllReturnsAudioURLAndServesBytes|TestChatAnswerForwardsQuestionAnswer|TestChatReplyIncludesPendingQuestion" -count=1`
- **Fixture:** fake buffered chat service, fake voice synthesizer returning OGG bytes with `OggS` prefix, and bounded `AudioCache`.
- **Artifact:** `/chat?voice=all` JSON response, `GET /chat/audio/{cache_id}` response, and fake service call capture.
- **Ground truth:** `audio_url` is non-empty, voice receives the assistant reply text, audio endpoint returns `Content-Type: audio/ogg`, body length is greater than 1024 bytes, and body prefix is `OggS`.
- **Pass threshold:** exact status 200 for both requests; exact content type; byte-length and prefix assertions pass; answer endpoint forwards `question_id`, answer text, and selected option IDs.
- **PRD gate:** web buffered replies can attach temporary playable audio without persisting audio as canonical state.

### B-CONS-08-B: Pending Question Buffered And Streaming Projection

- **Command:** `go test ./internal/channels/web -run "Test(ChatService|StreamRouter|Inbound)" -count=1`; `go test ./internal/api -run "TestChat(ReplyIncludesPendingQuestion|Stream|ReplyIncludesBudgetWarning)" -count=1`
- **Fixture:** fake Hub events with `chat.EventQuestionRequested`, buffered web router, and UI Message Stream parser.
- **Artifact:** buffered `ChatReply.PendingQuestion` and parsed SSE frames.
- **Ground truth:** buffered mode returns `pending_question{id, question, options[], kind}`; streaming mode emits a `data-pending-question` chunk whose `data` object carries the same fields.
- **Pass threshold:** exact question ID/text/kind/options in buffered JSON and streaming frame; stream still terminates with `[DONE]`.
- **PRD gate:** CONS-12 can render ask_user UI from the backend contract without guessing from assistant prose.

### B-CONS-08-C: Durable Web ask_user Resume

- **Command:** `go test ./cmd/aura -run "TestWebChatAskUserPendingAndAnswerResume" -count=1`
- **Fixture:** shared Hub, SQLite run store, registered `AskUserTool`, authorized web actor, and fake LLM that first calls `ask_user` then finalizes after receiving the tool result.
- **Artifact:** first `ChatReply`, second `AnswerChat` reply, SQLite `chat_questions`, SQLite `run_events`, and captured LLM request messages.
- **Ground truth:** first reply has pending question `Which option?`; `chat_questions` row is `waiting` for `thread_id='web:alice:default'`; `AnswerChat(...,"2")` resumes with final text `resumed with beta`; the row becomes `answered` with non-empty `answer_run_id`; `run_events` includes `question_answered` with `causation_id=<question_id>`; final LLM request includes tool result `ask-1=beta`.
- **Pass threshold:** all DB scalar assertions equal 1 and the captured tool result is present.
- **PRD gate:** browser answer resumes the same durable question/run thread instead of starting an unrelated web turn.

### B-CONS-08-D: Dedicated Slice QA

- **Command:** `go test ./internal/channels/web ./internal/api ./internal/chat -run "Voice|AskUser|Question" -count=1`; `go test ./cmd/aura ./internal/channels/web ./internal/api ./internal/chat ./internal/channels/telegram -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/api ./internal/channels/web ./internal/channels/telegram ./internal/channels/askuser ./internal/chat --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/app_wire.go cmd/aura/chat_hub.go cmd/aura/web_chat.go cmd/aura/web_chat_test.go cmd/aura/web_chat_helpers_test.go cmd/aura/web_voice.go internal/api/chat.go internal/api/chat_test.go internal/api/router.go internal/api/chat_audio.go internal/channels/askuser/askuser.go internal/channels/telegram/ask_user_resume.go internal/channels/telegram/ask_user_resume_test.go internal/channels/web/chat_service.go internal/channels/web/chat_service_test.go internal/channels/web/inbound.go internal/channels/web/outbound.go internal/channels/web/streaming_outbound.go internal/channels/web/streaming_outbound_test.go`; `git diff --check`; `go test ./... -count=1`
- **Fixture:** touched packages, full Go repository suite, file-size split for `cmd/aura/web_chat_test.go`.
- **Artifact:** command outputs in this slice run plus diff inspection.
- **Ground truth:** no compile/vet/lint regressions; touched-file duplication is zero; `cmd/aura/web_chat.go` and `cmd/aura/web_chat_test.go` stay under 600 lines after helper split; full Go suite passes after voice/ask_user parity.
- **Negative/adversarial check:** unauthenticated/misconfigured answer/audio services return non-success in API tests, active stream duplicate rejection remains covered, and the ask_user fixture requires an authorized actor before the tool pause can happen.
- **Pass threshold:** all commands pass; `dupl` reports `Found total 0 clone groups`; LOC gate remains below 600 for production web chat and primary web chat test file.
- **PRD gate:** self-audited slice QA before the US-CONS-08 atomic commit.

## Planned Story Benchmarks

These rows are required before each later story can be called complete. Replace placeholder test names with concrete tests inside that story's commit.

| Story | Exact Command | Fixture / Artifact | Ground Truth | Pass Threshold |
| --- | --- | --- | --- | --- |
| CONS-03 | Completed by B-CONS-03-A..D above | fake tool registry + hub-backed web turn using shared executor | web calls `agent.ExecuteToolCalls`; tool attempts and visible tool context match current behavior | no `webToolExecutor` symbols; tool attempt row and captured context fields correct |
| CONS-04 | Completed by B-CONS-04A..C above | web + Telegram builder source gate, Telegram byte-parity fixture, full Go suite | web and Telegram invoke `agentcore.Builder`; no transport-owned non-empty `agent.Invocation` literal; no no-op runtime flag shim | adoption/parity tests pass; fixture and full suite pass |
| CONS-05 | Completed by B-CONS-05-A..C above | shared Hub with fake web and Telegram outbound adapters; source negative check | ChannelWeb events reach only web outbound; ChannelTelegram events reach only Telegram outbound; duplicate Hub constructors are gone | zero cross-channel deliveries; stale constructor symbols absent |
| CONS-06 | Completed by B-CONS-06-A..C above | mock budget runtime + isolated SQLite archive | API reply includes `budget_warning`; `conversations` rows have `channel='web'` | exact JSON field and row count |
| CONS-07 | Completed by B-CONS-07-A..D above | local SSE endpoint, parser fixture, shared Hub streaming service | frames are valid AI SDK UI Message Stream SSE frames; headers include `text/event-stream`, `Cache-Control: no-cache, no-transform`, `X-Accel-Buffering: no`, `x-vercel-ai-ui-message-stream: v1`; shared Hub calls `llm.Stream` | all fixture frames parse; stream terminates with `[DONE]`; buffered `/chat` tests still pass |
| CONS-08 | Completed by B-CONS-08-A..D above | fake TTS/audio cache, fake ask_user tool, shared Hub with SQLite run store | `audio_url` serves OGG bytes; pending question is exposed in buffered/SSE modes; answer resumes the same durable question thread | audio >1024 bytes; question row waiting->answered; `question_answered` event persisted |
| CONS-09 | `npm --prefix web run build` plus browser probe | assistant-ui `/chat` route against local backend | text-only stream renders in Thread and composer can send | visible streamed assistant message |
| CONS-10 | `npm --prefix web run build` plus markdown fixture probe | markdown/code/wiki-link assistant message | GFM, code block, and `[[slug]]` render correctly | DOM assertions pass |
| CONS-11 | `npm --prefix web run build` plus tool-call fixture probe | SSE tool-call frames | generic and specialized tool cards render with status and summaries | DOM cards present; no secret args displayed |
| CONS-12 | `npm --prefix web run build` plus ask_user browser probe | pending question frame and answer endpoint | options/free text submit resumes run | final assistant reply appears after answer |
| CONS-13 | `npm --prefix web run build` plus attachment/audio/dictation probes | fake audio URL, test PDF upload, browser speech mock | audio control plays URL; source chip sends source_id; dictated text enters composer | DOM/API assertions pass |

## Phase Completion Evidence

Phase-CONS is not complete until:

- all CONS-02..13 stories are committed atomically,
- `scripts/ralph/prd.json` marks every CONS story `passes:true`,
- `progress.md` contains one entry per story with commands, artifacts, and failures,
- `go test ./...`, `go vet ./...`, `go build ./...`, and `npm --prefix web run build` pass after the final story,
- a final cross-transport probe asserts web and Telegram parity for `tools_used`, `tool_calls`, `llm_calls`, archive rows, context compaction, streaming, ask_user, and voice where applicable.
