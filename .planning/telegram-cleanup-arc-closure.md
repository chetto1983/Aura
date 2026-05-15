# internal/telegram/ — Cleanup Arc Closure Report (US-B07)

Generated: 2026-05-15 (story US-B07)
Arc: US-B01 through US-B07

---

## 1. LOC Table — internal/telegram/ prod files

Baseline (post US-A19): **3316 prod LOC**
After US-B01..B06: **2530 prod LOC**
Criterion: ≤ 2800 ✅ (actual 2530)

| File | LOC (post-arc) | Telegram concern? |
|------|---------------|-------------------|
| documents.go | 498 | ✅ Telegram document upload (onDocument, onPhoto, OCR/ingest trigger) |
| atomic_tables.go | 468 | ✅ Telegram entity renderer for GFM pipe tables (MessageEntities with alignment) |
| entity_messages.go | 226 | ✅ Streaming message pipeline: composeStreamingMessage, SendAssistantText, EditAssistantMsg, NewHubChatClient |
| access.go | 226 | ✅ Telegram /start + /login handlers; user allowlist bootstrap (TOFU), pending-access fan-out |
| bot.go | 295 | ✅ Bot struct (11 fields + botRuntime), Start/Stop lifecycle, Telegram senders |
| deps.go | 142 | ✅ Deps struct (23 fields) + NewBot constructor; DI surface for cmd/aura/app.go |
| tool_exec_helpers.go | 129 | ✅ ExecToolCalls (parallel Telegram dispatch), MaxToolLoopIterations, TerminalToolPolicyEnabled |
| status.go | 99 | ✅ Telegram /status command; budget + context summary sent via tele.Context |
| entity_markdown.go | 85 | ✅ renderForTelegramEntities: Markdown → Telegram MessageEntities |
| setup.go | 97 | ✅ NewBot, UserGate Telegram callbacks, newDocHandler wiring, registerHandlers, installBotCommands |
| handlers.go | 71 | ✅ onMessage: routes Telegram messages to hub.Receive |
| commands.go | 70 | ✅ installBotCommands + registerHandlers; Telegram /commands menu |
| conversation_terminal.go | 63 | ✅ FinalizeTerminalTool: no-tool LLM round after terminal-tool; sends via tele.Context |
| conversation_snapshot.go | 55 | ✅ StoreOrchestrationSnapshot: per-user session snapshots consumed by channels/telegram |
| doc.go | 6 | ✅ Architecture invariant assertion (wrapper-only) |
| **TOTAL** | **2530** | All remaining files are Telegram-specific ✅ |

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

`documents.go` and `atomic_tables.go` handle Telegram-specific document upload and message entity rendering — the channel protocol concerns that no other node in the diagram handles. `bot.go` and `deps.go` define the `Bot` struct (the Telegram application object) and its dependency surface, both inherently coupled to `gopkg.in/telebot.v4`. `access.go` and `setup.go` implement Telegram-specific user onboarding (TOFU allowlist, `/start`, `/login`) and bot command registration. The streaming pipeline in `entity_messages.go` and `entity_markdown.go` translates the agent loop's text output into Telegram `MessageEntities` with formatting — a Telegram-API concern. `handlers.go`, `commands.go`, `status.go`, `conversation_terminal.go`, and `conversation_snapshot.go` are all thin Telegram handlers that route updates or deliver responses through `tele.Context`. `tool_exec_helpers.go` orchestrates parallel Telegram edit calls during tool execution. `doc.go` locks in the wrapper-only architecture invariant. No file in `internal/telegram/` now imports `internal/telegram` itself from a downstream package, and every non-Telegram concern identified in US-A19 has been relocated by US-B01..B06.
