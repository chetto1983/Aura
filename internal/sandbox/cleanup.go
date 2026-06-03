package sandbox

// ConversationCleaner is the narrow purge surface that conversations.Store.Delete
// will call to tear down a conversation's on-disk tree with the os.Root no-follow
// cascade (D-14). The interface is DEFINED in package conversations (accept-
// interfaces-there) and the concrete impl is THIS package's *WorkspaceManager,
// wired by the composition root (main.go, 08-08). Declaring the shape here too
// documents the contract and lets a compile-time assertion catch drift — without
// sandbox importing conversations (import-cycle guard, landmine #4).
type ConversationCleaner interface {
	PurgeConversationDir(convID string) error
}

// WorkspaceManager is the production ConversationCleaner. The assertion fails to
// compile if the method set drifts from the contract conversations expects.
var _ ConversationCleaner = (*WorkspaceManager)(nil)
