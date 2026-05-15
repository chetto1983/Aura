// Package agent — session.go owns the per-user conversation lifecycle.
//
// Concurrency model (CONC-01, F-012, F-016):
//   - In production, callers MUST wire a *concurrency.UserGate. The gate is
//     the single source of "user X is active" truth and serializes message
//     processing per user, so the *conversation.Context owned by a Session
//     is single-writer by construction.
//   - When SessionStore is constructed without a gate, two concurrent Begin
//     calls for the same user share the SAME *conversation.Context and write
//     to its message slice without any mutex. The active sync.Map is a thin
//     stand-in for the gate's IsActive signal. This mode is for tests only;
//     production deployments must pass a non-nil gate.
//
// Snapshot lifecycle (F-013):
//   - Finish and Abort do NOT clear snapshots. Snapshots intentionally
//     survive session boundaries so a returning user / dashboard can read
//     the most recent turn's metadata. Operators are expected to call
//     PruneSnapshots periodically (the bot wires this on a maintenance
//     cadence). Clear() removes both context and snapshot when an explicit
//     "forget this user" action is invoked.
package agent

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/conversation"
)

type SessionStore struct {
	gate      *concurrency.UserGate
	active    sync.Map // used only when gate == nil (fallback for tests/no-gate mode)
	context   sync.Map
	snapshots sync.Map
}

type Snapshot struct {
	StoredAt                time.Time
	PromptVersion           string
	PromptModules           []string
	PromptHash              string
	Toolset                 string
	ToolsetSelectReason     string
	ToolsExposed            []string
	ToolsCalled             []string
	ReadSkills              []string
	RetrievalCapsulePresent bool
	LoopSteps               int
	LLMCalls                int
	ToolCalls               int
	SkillsRead              bool
	SwarmUsed               bool
	SandboxUsed             bool
	TerminalTool            string
	DuplicateToolCall       bool
	TokensPrompt            int
	TokensCompletion        int
	TokensTotal             int
	CostUSD                 float64
}

type Session struct {
	store  *SessionStore
	userID string
	ctx    *conversation.Context
	once   sync.Once
}

// NewSessionStore creates a SessionStore. An optional *concurrency.UserGate may
// be passed; when set, IsActive delegates to gate.IsActive instead of
// maintaining a separate active sync.Map (CONC-01, D-11).
func NewSessionStore(gate ...*concurrency.UserGate) *SessionStore {
	s := &SessionStore{}
	if len(gate) > 0 && gate[0] != nil {
		s.gate = gate[0]
	}
	return s
}

func (s *SessionStore) Begin(userID string, cfg conversation.Config) (*Session, bool) {
	if s == nil {
		s = NewSessionStore()
	}
	userID = strings.TrimSpace(userID)
	// Lazy allocation: only build a fresh Context when this user has never
	// been seen. LoadOrStore would otherwise allocate on every Begin and
	// immediately discard the new value when the user already has a
	// Context — wasteful and a config-drift trap if the caller passes a
	// different cfg on the second call (the first cfg wins; F-014).
	ctxValue, ok := s.context.Load(userID)
	if !ok {
		ctxValue, _ = s.context.LoadOrStore(userID, conversation.NewContext(cfg))
	}
	ctx, _ := ctxValue.(*conversation.Context)
	// When no gate, maintain the active map for backward compat (tests).
	// When gate is set, the actor's presence in gate.actors IS the active signal (CONC-01).
	loaded := ok
	if s.gate == nil {
		s.active.Store(userID, true)
	}
	return &Session{store: s, userID: userID, ctx: ctx}, loaded
}

func (s *SessionStore) Load(userID string) (*conversation.Context, bool) {
	if s == nil {
		return nil, false
	}
	value, ok := s.context.Load(strings.TrimSpace(userID))
	if !ok {
		return nil, false
	}
	ctx, ok := value.(*conversation.Context)
	return ctx, ok && ctx != nil
}

func (s *SessionStore) Clear(userID string) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	s.context.Delete(userID)
	s.snapshots.Delete(userID)
	if s.gate == nil {
		s.active.Delete(userID)
	}
}

func (s *SessionStore) IsActive(userID string) bool {
	if s == nil {
		return false
	}
	if s.gate != nil {
		// Delegate to UserGate: actor alive == user is active (CONC-01, D-11).
		return s.gate.IsActive(strings.TrimSpace(userID))
	}
	// Fallback when no gate: use active map (backward compat, tests).
	_, ok := s.active.Load(strings.TrimSpace(userID))
	return ok
}

func (s *SessionStore) StoreSnapshot(userID string, snapshot Snapshot) {
	if s == nil || strings.TrimSpace(userID) == "" {
		return
	}
	if snapshot.StoredAt.IsZero() {
		snapshot.StoredAt = time.Now().UTC()
	}
	snapshot.PromptModules = append([]string(nil), snapshot.PromptModules...)
	snapshot.ToolsExposed = append([]string(nil), snapshot.ToolsExposed...)
	snapshot.ToolsCalled = append([]string(nil), snapshot.ToolsCalled...)
	snapshot.ReadSkills = append([]string(nil), snapshot.ReadSkills...)
	s.snapshots.Store(strings.TrimSpace(userID), snapshot)
}

func (s *SessionStore) Snapshot(userID string) (Snapshot, bool) {
	if s == nil {
		return Snapshot{}, false
	}
	value, ok := s.snapshots.Load(strings.TrimSpace(userID))
	if !ok {
		return Snapshot{}, false
	}
	snapshot, ok := value.(Snapshot)
	if !ok {
		return Snapshot{}, false
	}
	snapshot.PromptModules = append([]string(nil), snapshot.PromptModules...)
	snapshot.ToolsExposed = append([]string(nil), snapshot.ToolsExposed...)
	snapshot.ToolsCalled = append([]string(nil), snapshot.ToolsCalled...)
	snapshot.ReadSkills = append([]string(nil), snapshot.ReadSkills...)
	return snapshot, true
}

func (s *SessionStore) PruneSnapshots(now time.Time, retentionDays int) {
	if s == nil || retentionDays <= 0 {
		return
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	s.snapshots.Range(func(key, value any) bool {
		snapshot, ok := value.(Snapshot)
		if ok && !snapshot.StoredAt.IsZero() && snapshot.StoredAt.Before(cutoff) {
			s.snapshots.Delete(key)
		}
		return true
	})
}

// NewSnapshotFromTurnStats builds an agent.Snapshot from a TurnStats value and
// a wall-clock timestamp. Slice fields are deep-copied so the caller cannot
// mutate the returned value via the original stats slices.
func NewSnapshotFromTurnStats(stats TurnStats, now time.Time) Snapshot {
	return Snapshot{
		StoredAt:                now,
		PromptVersion:           stats.PromptVersion,
		PromptModules:           append([]string(nil), stats.PromptModules...),
		PromptHash:              stats.PromptHash,
		Toolset:                 stats.Toolset,
		ToolsetSelectReason:     stats.ToolsetSelectReason,
		ToolsExposed:            append([]string(nil), stats.ToolsExposed...),
		ToolsCalled:             append([]string(nil), stats.ToolsCalled...),
		ReadSkills:              append([]string(nil), stats.ReadSkills...),
		RetrievalCapsulePresent: stats.RetrievalCapsulePresent,
		LoopSteps:               stats.LoopSteps,
		LLMCalls:                stats.LLMCalls,
		ToolCalls:               stats.ToolCalls,
		SkillsRead:              stats.SkillsRead,
		SwarmUsed:               stats.SwarmUsed,
		SandboxUsed:             stats.SandboxUsed,
		TerminalTool:            stats.TerminalTool,
		DuplicateToolCall:       stats.DuplicateToolCall,
		TokensPrompt:            stats.TokensPrompt,
		TokensCompletion:        stats.TokensCompletion,
		TokensTotal:             stats.TokensTotal,
		CostUSD:                 stats.CostUSD,
	}
}

// clearActive removes the active marker for userID.
// When gate is set, this is a no-op -- lifecycle is owned by the gate (CONC-01).
// When gate is nil, it removes from the active sync.Map (backward compat, tests).
func (s *SessionStore) clearActive(userID string) {
	if s == nil || s.gate != nil {
		return
	}
	s.active.Delete(strings.TrimSpace(userID))
}

func (s *Session) Conversation() *conversation.Context {
	if s == nil {
		return nil
	}
	return s.ctx
}

// Finish marks a graceful end of the session. Snapshots are preserved (see
// SessionStore docs).
func (s *Session) Finish() {
	s.clearActive()
}

// Abort marks a non-graceful end (panic recovery, parent ctx cancellation,
// fatal tool error). Snapshots are still preserved so the operator can inspect
// the last-recorded state, but a warn-level log distinguishes Abort from a
// clean Finish (F-015).
func (s *Session) Abort() {
	if s != nil {
		slog.Default().Warn("agent: session aborted", "user_id", s.userID)
	}
	s.clearActive()
}

func (s *Session) clearActive() {
	if s == nil || s.store == nil {
		return
	}
	s.once.Do(func() {
		s.store.clearActive(s.userID)
	})
}
