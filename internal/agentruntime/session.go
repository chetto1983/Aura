package agentruntime

import (
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/conversation"
)

type SessionStore struct {
	active    sync.Map
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
	HiddenToolRejected      bool
	SkillsRead              bool
	SwarmUsed               bool
	SandboxUsed             bool
	TerminalTool            string
	DuplicateToolCall       bool
	TokensPrompt            int
	TokensCompletion        int
	TokensTotal             int
	CostUSD                 float64
	RetryNudgesSent         int
	SpiralBreakerFired      bool
	TieredBudgetTier        string
}

type Session struct {
	store  *SessionStore
	userID string
	ctx    *conversation.Context
	once   sync.Once
}

func NewSessionStore() *SessionStore {
	return &SessionStore{}
}

func (s *SessionStore) Begin(userID string, cfg conversation.Config) (*Session, bool) {
	if s == nil {
		s = NewSessionStore()
	}
	userID = strings.TrimSpace(userID)
	value, loaded := s.context.LoadOrStore(userID, conversation.NewContext(cfg))
	ctx, _ := value.(*conversation.Context)
	s.active.Store(userID, true)
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
	s.active.Delete(userID)
	s.snapshots.Delete(userID)
}

func (s *SessionStore) IsActive(userID string) bool {
	if s == nil {
		return false
	}
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

func (s *SessionStore) clearActive(userID string) {
	if s == nil {
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

func (s *Session) Finish() {
	s.clearActive()
}

func (s *Session) Abort() {
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
