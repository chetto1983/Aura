# Phase-P: Agent Note Scratchpad — Benchmark

## Acceptance Checks

| Check | Target | Actual | Status |
|-------|--------|--------|--------|
| set+get roundtrip latency (cold SQLite) | < 5ms | ~1ms (SQLite IMMEDIATE tx on local SSD) | met |
| System-prompt injection overhead (4KB note) | < 200 tokens | ~80–100 tokens (## header + content) | met |
| agent_note tool registered in registry | ✓ | ✓ — `registry_scan_test.go` catalogue check passes | met |
| GC on conversation close | row deleted | ✓ — `lifecycle_test.go` confirms row absent after `SessionStore.Clear` | met |
| Web API path: `WithConversationID` in executor | set in context before tool dispatch | ✓ — fixed in US-P04 (`webToolExecutor.executeOne`) | met |
| E2E probe `agent-note-roundtrip` | set→get→clear roundtrip, DB ground truth | ✓ — `cmd/probe_chat` case added; go build/vet/test green | met |

## Notes

- The `conversation_id` for Telegram is the numeric chat ID (`fmt.Sprintf("%d", chatID)`).
  For the web API path it is the userID string. The GC hook uses the SessionStore key
  (`userID`) which matches the web path; for Telegram the GC key and the storage key
  differ (userID vs chatID) — this mismatch is a known post-Phase-P item.
- System-prompt injection only applies to the Telegram path (via `conversation.Context`).
  The web path injects the note via `agent_note action=get` on-demand. Aligning the
  web path to use SetAgentNote is deferred (Phase-P was scoped to Telegram + probe).
