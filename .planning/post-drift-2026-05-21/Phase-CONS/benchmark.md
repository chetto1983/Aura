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

## Planned Story Benchmarks

These rows are required before each later story can be called complete. Replace placeholder test names with concrete tests inside that story's commit.

| Story | Exact Command | Fixture / Artifact | Ground Truth | Pass Threshold |
| --- | --- | --- | --- | --- |
| CONS-03 | `go test ./internal/agent ./cmd/aura -run "TestExecuteToolCalls|TestHubBackedWebChat" -count=1` | fake tool registry + web turn using shared executor | web calls `agent.ExecuteToolCalls`; tool attempts and `tools_used` match current behavior | no `webToolExecutor` symbols; all tool attempt rows correct |
| CONS-04 | `go test ./... -run "TestInvocationBuilder|TestAgentCore|TestHubBackedWebChat" -count=1` plus Telegram byte-parity fixture | legacy vs `AURA_AGENTCORE_BUILDER=true` transcript comparison | same tool-call sequence names + argument keys | exact sequence equality; response text drift <=5% where compared |
| CONS-05 | `go test ./internal/chat ./cmd/aura ./internal/channels/web ./internal/channels/telegram -run "TestHub" -count=1` | shared Hub with fake web and Telegram outbound adapters | ChannelWeb events reach only web outbound; ChannelTelegram events reach only Telegram outbound | zero cross-channel deliveries |
| CONS-06 | `go test ./internal/channels/web ./internal/api ./cmd/aura -run "Budget|Archive|Compaction" -count=1` | mock budget runtime + isolated SQLite archive | API reply includes `budget_warning`; `conversations` rows have `channel='web'` | exact JSON field and row count |
| CONS-07 | `go test ./internal/channels/web ./internal/api -run "SSE|DataStream|Streaming" -count=1` plus `curl -N` probe | local SSE endpoint and parser fixture | frames are valid Vercel AI SDK data-stream frames; headers include `text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no` | first byte <500ms in live probe; all fixture frames parse |
| CONS-08 | `go test ./internal/channels/web ./internal/api ./internal/chat -run "Voice|AskUser|Question" -count=1` | fake TTS and fake ask_user tool | `audio_url` serves OGG bytes; pending question persists and answer resumes same run | audio >1024 bytes; question row waiting->answered |
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
