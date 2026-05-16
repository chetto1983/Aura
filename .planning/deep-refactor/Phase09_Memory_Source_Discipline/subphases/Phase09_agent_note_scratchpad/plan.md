# Phase-P: Agent Note Scratchpad — Plan

**Parent phase:** Phase 9 — Memory and Source Discipline
**Status:** ✅ Closed 2026-05-16
**Scope reference:** prd.md §5.7 line 678 (`agent_working_memory`), prd.md §5.7 line 717 (GC lifecycle)

## Goal

Wire `agent_note` as a per-conversation scratchpad for Aura's working memory (capability #4
from the "Aura come Claude Code" brainstorm: "TodoWrite cross-turn checklist"). The note
persists across multiple LLM turns within the same conversation and is garbage-collected
when the conversation ends. It is NOT visible to the user and NOT promoted to wiki or
user memory.

## Stories

| Story | Description |
|-------|-------------|
| US-P01 | `agent_notes` SQLite table (migration v15) + `internal/agentnote/Store` API |
| US-P02 | `agent_note` LLM tool with action-dispatch schema (set/append/get/clear) |
| US-P03 | Wire into agent loop: system-prompt injection + GC on conversation close |
| US-P04 | Closure docs + integration probe (`cmd/probe_chat` case `agent-note-roundtrip`) |

## Design Decisions

- **SQLite-backed, per-conversation.** `agent_notes` table has `conversation_id` as PK.
  For Telegram the conversation_id is the numeric chat_id; for the web API it is the
  userID string ("chat-cli" for the probe harness).
- **Action-dispatch schema (Phase-L oneOf pattern).** Tool schema mirrors `action_error.go`
  convention; actions: set / append / get / clear. Follows existing Phase-L pattern from
  commit `7ccdca33`.
- **System-prompt injection at turn start.** The Telegram path reads the stored note via
  `InvocationBuilder.Build` and calls `convCtx.SetAgentNote(content)` before the LLM
  call, injecting a stable `## Your current note (working memory)` section between the
  base prompt and the dynamic search context (cache discipline: §3 D13).
- **GC via SessionStore.OnClose hook.** `wireBot` registers `agentnote.Store.Clear` as
  a `SessionStore.OnClose` callback. Fires when `SessionStore.Clear(userID)` is called
  (explicit forget or bot restart).
- **Web API path fix (US-P04).** `webToolExecutor.executeOne` was missing
  `WithConversationID(toolCtx, e.userID)`, preventing `agent_note` from resolving the
  conversation ID in the web chat context. Fixed in the US-P04 commit.
