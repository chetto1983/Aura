package arcadedb

import "time"

// RecallMode selects one bounded operation on the unified memory read surface.
type RecallMode string

const (
	// RecallModeSemantic searches fact and conversation evidence together.
	RecallModeSemantic RecallMode = "semantic"
	// RecallModeRecent browses bounded recent conversation windows.
	RecallModeRecent RecallMode = "recent"
	// RecallModePeriod reads every projected conversation turn in a bounded interval.
	RecallModePeriod RecallMode = "period"
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
	From           time.Time
	To             time.Time
	ConversationID string
	AnchorSeq      int
	Cursor         string
	Direction      RecallDirection
	Limit          int
	// ExcludeConversationIDs is host-derived negative scope. It can suppress
	// conversation evidence but never choose IdentityID or add candidates.
	ExcludeConversationIDs []string
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
	EntityCount            int
	BackendLatency         time.Duration
}

// RecallCursor is unsigned transport state that is revalidated before every query.
type RecallCursor struct {
	Version        int             `json:"version"`
	Mode           RecallMode      `json:"mode,omitempty"`
	IdentityID     string          `json:"identity_id"`
	ConversationID string          `json:"conversation_id"`
	AnchorSeq      int             `json:"anchor_seq"`
	Direction      RecallDirection `json:"direction"`
	PageSize       int             `json:"page_size"`
	From           string          `json:"from,omitempty"`
	To             string          `json:"to,omitempty"`
	AfterMillis    int64           `json:"after_millis,omitempty"`
}

// RecallResult is the storage-layer result consumed by the MCP adapter.
type RecallResult struct {
	Evidence []RecallEvidence
	// Entities are the graph nodes the question reached through its evidence,
	// each with its own facts (memory_recall_expand.go). Additive: never a
	// substitute for Evidence, never counted against its budget.
	Entities  []RecallEntityNode
	Abstained bool
	Reason    string
	// FloorsReason is ReasonUncalibratedFloors on a hybrid recall admitted by floors never
	// measured for its space (spec §9).
	FloorsReason string
	NextCursor   string
	Retrieval    RecallRetrieval
}
