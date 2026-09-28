package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/toolinvocations"
)

// ToolInvocationReader is the append-only read needed to rehydrate trusted MCP
// provenance. The conversation ownership gate runs before this reader is called.
type ToolInvocationReader interface {
	ListByConversation(context.Context, string) ([]toolinvocations.Event, error)
}

// SetToolInvocationReader wires the existing ledger for display replay.
func (s *Server) SetToolInvocationReader(reader ToolInvocationReader) {
	s.toolInvocations = reader
}
