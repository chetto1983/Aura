# internal/telegram/ — Cleanup Arc Closure Report (US-B07, updated US-C04)

Generated: 2026-05-15 (story US-B07)
Updated: 2026-05-15 (story US-C04 — post Phase-C LOC refresh)
Arc: US-B01 through US-B07, then US-C01 through US-C03

---

## 1. LOC Table — internal/telegram/ prod files

Baseline (post US-A19): **3316 prod LOC**
After US-B01..B07 (arc closure): **2530 prod LOC**
After US-C01..C03 (Phase-C): **2411 prod LOC**
Criterion: ≤ 2800 ✅ (actual 2411)

| File | LOC (post-C03) | Telegram concern? |
|------|---------------|-------------------|
| documents.go | 498 | ✅ Telegram document upload (onDocument, onPhoto, OCR/ingest trigger) |
| atomic_tables.go | 468 | ✅ Telegram entity renderer for GFM pipe tables (MessageEntities with alignment) |
| bot.go | 315 | ✅ Bot struct (11 fields + botRuntime), Start/Stop lifecycle, Telegram senders; +4 interface methods for TerminalFinalizer (US-C03) |
| entity_messages.go | 226 | ✅ Streaming message pipeline: composeStreamingMessage, SendAssistantText, EditAssistantMsg, NewHubChatClient |
| access.go | 226 | ✅ Telegram /start + /login handlers; user allowlist bootstrap (TOFU), pending-access fan-out |
| deps.go | 142 | ✅ Deps struct (23 fields) + NewBot constructor; DI surface for cmd/aura/app.go |
| status.go | 99 | ✅ Telegram /status command; budget + context summary sent via tele.Context |
| setup.go | 97 | ✅ NewBot, UserGate Telegram callbacks, newDocHandler wiring, registerHandlers, installBotCommands |
| entity_markdown.go | 85 | ✅ renderForTelegramEntities: Markdown → Telegram MessageEntities |
| handlers.go | 71 | ✅ onMessage: routes Telegram messages to hub.Receive |
| commands.go | 70 | ✅ installBotCommands + registerHandlers; Telegram /commands menu |
| tool_exec_helpers.go | 44 | ✅ ExecToolCalls (thin Bot wrapper delegating to agent.ExecuteToolCalls), MaxToolLoopIterations, TerminalToolPolicyEnabled |
| conversation_snapshot.go | 32 | ✅ StoreOrchestrationSnapshot: delegates to agent.NewSnapshotFromTurnStats + sessionStore (US-C01) |
| conversation_terminal.go | 32 | ✅ FinalizeTerminalTool: thin wrapper calling agent.FinalizeAfterTerminalTool then editing *tele.Message (US-C03) |
| doc.go | 6 | ✅ Architecture invariant assertion (wrapper-only) |
| **TOTAL** | **2411** | All remaining files are Telegram-specific ✅ |

### Files removed in US-B01..B06

| File removed / relocated | LOC | Story | Commit |
|--------------------------|-----|-------|--------|
| conversation_format.go (deleted) | 28 | US-B01 | 5b19fe46 |
| adapters.go → cmd/aura/adapters.go | 56 | US-B02 | e7ba8b4d |
| tools_provider.go → internal/agent/toolsprovider.go | 56 | US-B03 | 13854ef1 |
| conversation_archive.go → internal/conversation/archive_turns.go | 90 | US-B04 | 9b6626f9 |
| helpers.go → cmd/aura/helpers.go | 136 | US-B05 | 7d099be7 |
| runtime_settings.go → internal/config/runtime_settings.go | 197 | US-B06 | 430371e6 |
| **Subtotal removed** | **563** | | |

---

## 2. Channel Adapter Verification

All adapters in `internal/channels/` declare compile-time interface assertions.
Architecture note: `web` and `silent` are outbound-only channels (they receive
events from the agent loop and deliver them, but have no inbound normalization
path — their inputs come via Hub.ReceiveMessage called directly by the API layer
and cron dispatcher respectively).

### internal/channels/telegram/

| Type | Assertion | Interface |
|------|-----------|-----------|
| `Inbound` | `var _ chat.InboundAdapter = Inbound{}` | `chat.InboundAdapter` |
| `Outbound` | `var _ chat.OutboundAdapter = (*Outbound)(nil)` | `chat.OutboundAdapter` |

### internal/channels/cron/

| Type | Assertion | Interface |
|------|-----------|-----------|
| `InboundAdapter` | `var _ chat.InboundAdapter = InboundAdapter{}` | `chat.InboundAdapter` |

### internal/channels/web/

| Type | Assertion | Interface |
|------|-----------|-----------|
| `Router` | `var _ chat.OutboundAdapter = (*Router)(nil)` | `chat.OutboundAdapter` |

### internal/channels/silent/

| Type | Assertion | Interface |
|------|-----------|-----------|
| `Outbound` | `var _ chat.OutboundAdapter = (*Outbound)(nil)` | `chat.OutboundAdapter` |

---

## 3. Architecture Conformance — Diagram Mapping

Every remaining file in `internal/telegram/` maps cleanly to the **"Chat Apps → Telegram"** node of the target architecture diagram.

`documents.go` and `atomic_tables.go` handle Telegram-specific document upload and message entity rendering — the channel protocol concerns that no other node in the diagram handles. `bot.go` and `deps.go` define the `Bot` struct (the Telegram application object) and its dependency surface, both inherently coupled to `gopkg.in/telebot.v4`. `access.go` and `setup.go` implement Telegram-specific user onboarding (TOFU allowlist, `/start`, `/login`) and bot command registration. The streaming pipeline in `entity_messages.go` and `entity_markdown.go` translates the agent loop's text output into Telegram `MessageEntities` with formatting — a Telegram-API concern. `handlers.go`, `commands.go`, `status.go`, `conversation_terminal.go`, and `conversation_snapshot.go` are all thin Telegram handlers that route updates or deliver responses through `tele.Context`. `tool_exec_helpers.go` orchestrates parallel Telegram edit calls during tool execution by delegating to `agent.ExecuteToolCalls`. `doc.go` locks in the wrapper-only architecture invariant. No file in `internal/telegram/` now imports `internal/telegram` itself from a downstream package, and every non-Telegram concern identified in US-A19 has been relocated.

---

## 4. Post Phase-C Summary

Phase-C (US-C01..C03) extracted the remaining channel-neutral logic from three files, further reducing telegram/ by **119 prod LOC** (2530 → 2411).

| Story | Commit | File changed | LOC before | LOC after | Delta |
|-------|--------|-------------|-----------|----------|-------|
| US-C01 | a00c7c86 | conversation_snapshot.go | 55 | 32 | −23 |
| US-C02 | 5434b217 | tool_exec_helpers.go | 129 | 44 | −85 |
| US-C03 | 96194491 | conversation_terminal.go | 63 | 32 | −31 |
| US-C03 | 96194491 | bot.go | 295 | 315 | +20 |
| **Phase-C net** | | | | | **−119** |

### What was extracted

- **US-C01**: `agent.NewSnapshotFromTurnStats(stats TurnStats, now time.Time) Snapshot` — constructor now lives in `internal/agent/session.go`; `conversation_snapshot.go` is a thin delegator.
- **US-C02**: `agent.ExecuteToolCalls(...)` — channel-neutral parallel tool dispatch now lives in `internal/agent/exec_helpers.go` with `ToolRunner` interface; `tool_exec_helpers.go` contains only `ChatIDFromTeleContext` + Bot wrappers.
- **US-C03**: `agent.FinalizeAfterTerminalTool(...)` — no-tool LLM round logic now lives in `internal/agent/terminal.go` with `TerminalFinalizer` interface; `conversation_terminal.go` calls the agent function then edits the `*tele.Message` placeholder via `tele.Context`.

### Prod LOC criterion — final status

| Milestone | Prod LOC | ≤ 2800 criterion |
|-----------|----------|-----------------|
| Post US-A19 (baseline) | 3316 | — |
| Post US-B07 (arc closure) | 2530 | ✅ PASS |
| **Post US-C03 (current HEAD)** | **2411** | ✅ **PASS** |
