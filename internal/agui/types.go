// Package agui is the AG-UI protocol transport adapter (Slice 8a). It consumes
// Aura's in-process iter.Seq2[*agent.Event, error] stream and maps it onto the
// official AG-UI community Go SDK event surface. The boundary is one-way: agui
// imports agent, the agent runtime NEVER imports agui (D-17, CI-enforced via
// scripts/agui_boundary_check.sh). The translator (translator.go) is a pure
// function — no I/O, no goroutines — so it is property- and golden-testable in
// isolation.
package agui

import (
	"context"
	"errors"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// ConversationStore is the narrow conversation surface the AG-UI server consumes
// (D-A2-02, "accept interfaces, return structs"). *conversations.Store satisfies
// it implicitly. Get resolves a threadId → conversation (404 chokepoint);
// LoadHistory rehydrates the byte-identical turn history for the GET messages
// projection. The CHAT-02 conversation-management surface (Phase 25) widens this
// with List/SearchConversationTurns/UpdateStatus/Rename/Delete/SetTitleIfNull and a
// read-only ListContextRotEvents (the D-11 microcompact ladder gauge marker) — all
// already on the concrete *conversations.Store, declared here consumer-side so agui
// depends only on the methods it calls, never the whole Store.
type ConversationStore interface {
	Get(ctx context.Context, conversationID string) (conversations.Conversation, error)
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
	List(ctx context.Context, includeArchived bool) ([]conversations.Conversation, error)
	SearchConversationTurns(ctx context.Context, query string, limit int) ([]conversations.SearchResult, error)
	UpdateStatus(ctx context.Context, conversationID, status string) error
	Rename(ctx context.Context, conversationID, title string) error
	SetTitleIfNull(ctx context.Context, conversationID, title string) error
	Delete(ctx context.Context, conversationID string) error
	ListContextRotEvents(ctx context.Context, conversationID string) ([]conversations.RotEvent, error)
	// D-09 / CHAT-05 branch surface (plan 25-07). ListBranches enumerates the navigable
	// branch leaves; ForkBranch writes a new sibling branch (edit-a-user-turn /
	// regenerate) chained off the diverging turn's parent and returns the new leaf seq.
	// CanonicalBranchLeaf is the default selection (the conversation's canonical tip).
	// All three are on the concrete *conversations.Store; declared here so agui depends
	// only on the methods it calls.
	ListBranches(ctx context.Context, conversationID string) ([]conversations.Branch, error)
	ForkBranch(ctx context.Context, conversationID string, divergeSeq int, role, content string) (int, uuid.UUID, error)
	CanonicalBranchLeaf(ctx context.Context, conversationID string) (int, error)
}

// ErrEmptyThreadID and ErrNoMessages are the Aura-semantic validation sentinels
// ValidateRunInput returns. They are distinct from the SDK's parse errors: the
// SDK's RunAgentInput.UnmarshalJSON owns camel/snake JSON shape, while these
// guard Aura's run preconditions (a thread to resolve, a message to drive a turn).
var (
	ErrEmptyThreadID = errors.New("agui: threadId must not be empty")
	ErrNoMessages    = errors.New("agui: messages must not be empty")
)

// ValidateRunInput checks the Aura-semantic preconditions for a run: a non-empty
// threadId (resolved to a conversation downstream by conversations.Get, which
// owns the UUID-shape check) and at least one message to drive the turn. It does
// NOT re-implement JSON parsing (the SDK's UnmarshalJSON does) nor re-validate
// the UUID shape (conversations.Get's parseUUID does).
func ValidateRunInput(in types.RunAgentInput) error {
	if in.ThreadID == "" {
		return ErrEmptyThreadID
	}
	if len(in.Messages) == 0 {
		return ErrNoMessages
	}
	return nil
}

// IDGenerator owns AG-UI id minting so the translator can guarantee non-empty
// messageId/toolCallId (both are Validate()-required by the SDK) and so tests can
// inject deterministic ids for stable golden compares. NewMessageID mints an
// assistant message run id; NewReasoningID mints the SEPARATE rsn- message id a
// REASONING lifecycle carries (distinct from the assistant TEXT_MESSAGE id so a
// consumer never conflates CoT with the answer); NewToolResultID mints the message
// id carrying a tool result (correlated to its toolCallID).
type IDGenerator interface {
	NewMessageID() string
	NewReasoningID() string
	NewToolResultID(toolCallID string) string
}

// uuidIDGenerator is the default production IDGenerator. Message ids are uuid-v4
// with the PRD "msg-" prefix; reasoning ids with the "rsn-" prefix; tool-result
// message ids derive from the originating tool call id so the correlation is
// observable on the wire.
type uuidIDGenerator struct{}

// NewIDGenerator returns the default uuid-v4-backed IDGenerator.
func NewIDGenerator() IDGenerator { return uuidIDGenerator{} }

func (uuidIDGenerator) NewMessageID() string { return "msg-" + uuid.NewString() }

func (uuidIDGenerator) NewReasoningID() string { return "rsn-" + uuid.NewString() }

func (uuidIDGenerator) NewToolResultID(toolCallID string) string {
	return "msg-tool-" + toolCallID
}
