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

// SessionJobTerminator is implemented by tools that own live per-(identity, session)
// background work — the background-shell registry (shell_poll over BackgroundShells). Where
// Evict only reclaims FINISHED session state, TerminateSession actively kills the RUNNING
// jobs bound to (ownerID, sessionID) so a conversation delete (MUSR-05 step 4) does not leave
// a detached shell outliving its conversation. It is owner-scoped by the plan-03
// (identity, session) job binding: a co-tenant that happens to share a session id is never
// touched. TerminateSession MUST be idempotent (an unknown owner/session is a no-op) and
// concurrency-safe.
type SessionJobTerminator interface {
	TerminateSession(ownerID, sessionID string)
}
