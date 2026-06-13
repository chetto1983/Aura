package tools

// SessionEvictor is implemented by tools that hold per-session (per-conversation)
// state keyed by the WithToolCallContext session id. In a long-running `serve`
// daemon a finished conversation's tool state would otherwise accumulate forever
// (audit R-41 / AP-16) — a slow unbounded leak. The Runner calls Evict on every
// registry tool implementing this interface when a conversation's lifecycle ends
// (sessionID == conversationID per D-26). Evict MUST be idempotent (an unknown id
// is a no-op) and concurrency-safe (it holds the same lock the state uses).
type SessionEvictor interface {
	Evict(sessionID string)
}
