package arcadedb

import (
	"context"
	"errors"
	"time"
)

// RecallMode selects one bounded operation on the unified memory read surface.
type RecallMode string

const (
	// RecallModeSemantic searches fact and conversation evidence together.
	RecallModeSemantic RecallMode = "semantic"
	// RecallModeRecent browses bounded recent conversation windows.
	RecallModeRecent RecallMode = "recent"
	// RecallModeOpen opens one conversation at a stable turn anchor.
	RecallModeOpen RecallMode = "open"
	// RecallModeScroll continues a bounded cursor-bound conversation read.
	RecallModeScroll RecallMode = "scroll"
	// RecallModeReasoning reserves the explicit reasoning-only read contract.
	RecallModeReasoning RecallMode = "reasoning"
)

// RecallDirection selects which side of a stable conversation anchor to read.
type RecallDirection string

const (
	// RecallDirectionBefore pages toward lower stable turn sequences.
	RecallDirectionBefore RecallDirection = "before"
	// RecallDirectionAfter pages toward higher stable turn sequences.
	RecallDirectionAfter RecallDirection = "after"
)

// RecallRequest is the identity-scoped internal form of memory_recall.
type RecallRequest struct {
	IdentityID     string
	Mode           RecallMode
	Query          string
	Entity         string
	Predicate      string
	AsOf           time.Time
	ConversationID string
	AnchorSeq      int
	Cursor         string
	Direction      RecallDirection
	Limit          int
}

// RecallEvidenceKind discriminates the typed evidence union.
type RecallEvidenceKind string

const (
	// RecallEvidenceFact carries one atomic long-term fact.
	RecallEvidenceFact RecallEvidenceKind = "fact"
	// RecallEvidenceConversation carries one bounded historical span.
	RecallEvidenceConversation RecallEvidenceKind = "conversation"
)

// RecallConversationWindow is a bounded chronological view around one hit.
type RecallConversationWindow struct {
	ConversationID string
	AnchorSeq      int
	Turns          []ConversationTurnHit
}

// RecallEvidence carries exactly one fact or conversation window.
type RecallEvidence struct {
	Kind         RecallEvidenceKind
	Rank         int
	Score        float64
	Fact         *FactHit
	Conversation *RecallConversationWindow
}

// RecallRetrieval separates contributing evidence tiers from the backend used.
type RecallRetrieval struct {
	EffectivePath          string
	Path                   string
	FactCandidateCount     int
	ConversationCandidates int
	FactCount              int
	ConversationCount      int
	ReasoningCount         int
	BackendLatency         time.Duration
}

// RecallCursor is unsigned transport state that is revalidated before every query.
type RecallCursor struct {
	Version        int             `json:"version"`
	IdentityID     string          `json:"identity_id"`
	ConversationID string          `json:"conversation_id"`
	AnchorSeq      int             `json:"anchor_seq"`
	Direction      RecallDirection `json:"direction"`
	PageSize       int             `json:"page_size"`
}

// RecallResult is the storage-layer result consumed by the MCP adapter.
type RecallResult struct {
	Evidence   []RecallEvidence
	Abstained  bool
	Reason     string
	NextCursor string
	Retrieval  RecallRetrieval
}

var errMemoryRecallNotImplemented = errors.New("arcadedb: unified memory recall not implemented")

// RecallMemory executes one identity-scoped unified recall operation.
func (c *Client) RecallMemory(context.Context, RecallRequest) (RecallResult, error) {
	return RecallResult{}, errMemoryRecallNotImplemented
}
