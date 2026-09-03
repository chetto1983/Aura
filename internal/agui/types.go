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
	// ListTurnReasoning is the amendment #91 (fix-plan 1.12) DISPLAY-ONLY read:
	// the persisted per-turn CoT + duration for the snapshot projection, kept
	// separate from LoadHistory so llm.Message stays structurally reasoning-free.
	ListTurnReasoning(ctx context.Context, conversationID string) ([]conversations.TurnReasoning, error)
	// ListTurnAttachments is the migration-0116 DISPLAY-ONLY read: what each user turn
	// was sent with. Like the reasoning read it stays off the history rebuild, so an
	// attachment can never re-enter the model context as a list of ids.
	ListTurnAttachments(ctx context.Context, conversationID string) ([]conversations.TurnAttachments, error)
	List(ctx context.Context, includeArchived bool) ([]conversations.Conversation, error)
	SearchConversationTurns(ctx context.Context, query string, limit int) ([]conversations.SearchResult, error)
	UpdateStatus(ctx context.Context, conversationID, status string) error
	Rename(ctx context.Context, conversationID, title string) error
	SetTitleIfNull(ctx context.Context, conversationID, title string) error
	Delete(ctx context.Context, conversationID string) error
	ListContextRotEvents(ctx context.Context, conversationID string) ([]conversations.RotEvent, error)
	// Phase 36 (MUSR-01 / D-06) owner-scoped surface: the AG-UI handlers route every
	// authenticated read/mutate through these so the principal (identityctx) is the sole
	// owner key and RLS backstops a forgotten filter. The four mutating variants return
	// rows-affected (0 = the caller does not own the id → 403 vs 404). *conversations.Store
	// satisfies these implicitly.
	GetForIdentity(ctx context.Context, conversationID, identityID string) (conversations.Conversation, error)
	ListForIdentity(ctx context.Context, identityID string, includeArchived bool) ([]conversations.Conversation, error)
	SearchConversationTurnsForIdentity(ctx context.Context, query, identityID string, limit int) ([]conversations.SearchResult, error)
	DeleteForIdentity(ctx context.Context, conversationID, identityID string) (int64, error)
	UpdateStatusForIdentity(ctx context.Context, conversationID, identityID, status string) (int64, error)
	RenameForIdentity(ctx context.Context, conversationID, identityID, title string) (int64, error)
	// Phase 37E (WEBMODEL-01 / D-06) owner-scoped effort persistence: handleRun (plan 06)
	// calls this to persist the per-conversation reasoning-effort symbol into the metadata
	// jsonb (no migration). rows-affected==0 = the caller does not own the id. On the read
	// side the value rides Conversation.ReasoningEffort (surfaced by conversationFromRow).
	UpdateReasoningEffortForIdentity(ctx context.Context, conversationID, identityID, effort string) (int64, error)
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

// ErrEmptyThreadID is the Aura-semantic validation sentinel ValidateRunInput returns.
// It is distinct from the SDK's parse errors: the SDK's RunAgentInput.UnmarshalJSON
// owns camel/snake JSON shape, while this guards Aura's one hard run precondition — a
// thread to resolve.
var ErrEmptyThreadID = errors.New("agui: threadId must not be empty")

// ValidateRunInput checks the Aura-semantic preconditions for a run: a non-empty
// threadId (resolved to a conversation downstream by conversations.Get, which owns the
// UUID-shape check). It deliberately does NOT require a message: an empty Messages list
// is the continue-after-resume signal (D-05 / CR-01) — handleRun resolves userMsg=nil
// (lastUserMessage) and drives Runner.Turn(…, nil) over the rehydrated history (the
// resolved ask_user answer is already a RoleTool turn the cockpit re-drive POSTs against
// after an inline approval resolves). It does NOT re-implement JSON parsing (the SDK's
// UnmarshalJSON does) nor re-validate the UUID shape (conversations.Get's parseUUID does).
func ValidateRunInput(in types.RunAgentInput) error {
	if in.ThreadID == "" {
		return ErrEmptyThreadID
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
