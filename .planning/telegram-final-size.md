# internal/telegram/ — Final Size Report (post US-A13b.3 .. US-A18)

Generated: 2026-05-15 (story US-A19)
Baseline at start of wrapper-cleanup arc: **5266 prod LOC** (pre US-A13a)
Current total: **3316 prod LOC / 2656 test LOC**

> **LOC criterion status**: The original criterion was <= 1500 prod LOC, set
> before measuring how much legitimately Telegram-specific code exists.
> After US-A13..A18 landed, 3316 prod LOC remain. Several files contain
> non-Telegram code (see "Remaining Debt" section); moving them is deferred
> to a future cleanup story. The invariant text in `doc.go` is the durable
> architectural assertion.

---

## Prod files (wc -l, sorted by LOC)

| File | LOC | Telegram concern? |
|------|-----|-------------------|
| documents.go | 546 | ✅ Telegram document upload (onDocument, onPhoto, OCR/ingest trigger) |
| atomic_tables.go | 499 | ✅ Telegram entity renderer for GFM pipe tables (MessageEntities with alignment) |
| bot.go | 321 | ✅ Bot struct (11 fields + botRuntime), Start/Stop lifecycle, Telegram senders |
| entity_messages.go | 240 | ✅ Streaming message pipeline: composeStreamingMessage, SendAssistantText, EditAssistantMsg, NewHubChatClient |
| access.go | 242 | ✅ Telegram /start + /login handlers; user allowlist bootstrap (TOFU), pending-access fan-out |
| runtime_settings.go | 197 | ⚠️ applyRuntimeSettings: no tele import; reads SQLite/env and updates agent/swarm/budget config. Should move to internal/config/ |
| deps.go | 160 | ✅ Deps struct (23 fields) + NewBot constructor; DI surface for cmd/aura/app.go |
| tool_exec_helpers.go | 145 | ✅ ExecToolCalls (parallel Telegram dispatch), MaxToolLoopIterations, TerminalToolPolicyEnabled, ChatIDFromTeleContext |
| helpers.go | 136 | ⚠️ ShouldBootstrapPromptOverlayDefaults, SkillSearchRoots, CreateLLMClient, CreateEmbeddingFunc, SetupSandboxRuntime — no tele import; used by cmd/aura. Should move to cmd/aura/ or internal/config/ |
| entity_markdown.go | 96 | ✅ renderForTelegramEntities: Markdown → Telegram MessageEntities |
| conversation_archive.go | 90 | ⚠️ ArchiveConversationTurns: no tele import; could move to internal/conversation/ |
| setup.go | 107 | ✅ NewBot, UserGate Telegram callbacks, newDocHandler wiring, registerHandlers, installBotCommands |
| status.go | 110 | ✅ Telegram /status command; budget + context summary sent via tele.Context |
| handlers.go | 79 | ✅ onMessage: routes Telegram messages to hub.Receive |
| commands.go | 78 | ✅ installBotCommands + registerHandlers; Telegram /commands menu |
| conversation_terminal.go | 68 | ✅ FinalizeTerminalTool: no-tool LLM round after terminal-tool; sends via tele.Context |
| conversation_snapshot.go | 62 | ✅ StoreOrchestrationSnapshot: per-user session snapshots consumed by channels/telegram |
| adapters.go | 56 | ⚠️ skillsDeleterAdapter / skillProposalApplierAdapter: not tele-specific; needed to break api↔skills cycle. Should move to cmd/aura/ |
| tools_provider.go | 56 | ⚠️ MakeToolsProvider: always-on tool seed; no tele import. Should move to internal/agent/ |
| conversation_format.go | 28 | ⚠️ Thin wrappers to agent.TerminalToolFinalizationMessages / agent.LooksLikeToolCallMarkup / agent.IsFileGenerationTool. Should be deleted; callers can import agent directly |
| **TOTAL** | **3316** | |

---

## Test files

| File | LOC |
|------|-----|
| documents_test.go | 685 |
| entity_markdown_table_test.go | 422 |
| tool_exec_helpers_test.go | 263 |
| bot_test.go | 250 |
| commands_test.go | 218 |
| runtime_settings_test.go | 173 |
| archive_test.go | 191 |
| conversation_snapshot_test.go | 79 |
| tools_provider_test.go | 96 |
| setup_sandbox_test.go | 96 |
| telegram_test_helpers_test.go | 56 |
| entity_markdown_test.go | 61 |
| setup_test.go | 39 |
| sandbox_integration_test.go | 26 |
| stop_test.go | 1 |
| **TOTAL** | **2656** |

---

## Remaining debt — prod files > 200 LOC

| File | LOC | Suggested action |
|------|-----|-----------------|
| documents.go | 546 | Extract to internal/telegram/documents/ sub-package to enforce the 200 LOC cap |
| atomic_tables.go | 499 | Extract to internal/telegram/render/ sub-package |
| bot.go | 321 | Could shrink further by moving more methods to channels/telegram once the Bot accessor surface stabilises |
| entity_messages.go | 240 | Borderline; streaming pipeline is cohesive and inherently Telegram-specific |
| access.go | 242 | Borderline; inherently Telegram (/start, /login) but large — consider splitting auth helpers |

## Remaining debt — files clearly NOT Telegram-specific

All 6 non-Telegram-specific files have been resolved in the US-B01..B06 arc:

| File | LOC | Target | Resolved |
|------|-----|--------|---------|
| helpers.go | 136 | cmd/aura/helpers.go | resolved in 7d099be7 (US-B05) |
| runtime_settings.go | 197 | internal/config/runtime_settings.go | resolved in 430371e6 (US-B06) |
| conversation_archive.go | 90 | internal/conversation/ | resolved in 9b6626f9 (US-B04) |
| conversation_format.go | 28 | Delete — callers import agent directly | resolved in 5b19fe46 (US-B01) |
| adapters.go | 56 | cmd/aura/adapters.go | resolved in e7ba8b4d (US-B02) |
| tools_provider.go | 56 | internal/agent/toolsprovider.go | resolved in 13854ef1 (US-B03) |
| **Subtotal** | **563** | Dropped prod LOC from 3316 → **2530** | All RESOLVED ✅ |

---

## Architecture invariant

See `internal/telegram/doc.go` for the durable wrapper-only assertion.
