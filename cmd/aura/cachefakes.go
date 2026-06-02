// In-memory, Postgres-free Store fakes for the hidden `aura cache-audit` replay
// (D-05 / Open Question 2). They live in package main (NON-test) because the
// shipped subcommand imports them — runner/fakes_test.go is unreachable from a
// binary. Each fake satisfies the narrow runner consumer interface
// (ConversationStore / PauseStore / IdentityStore / CacheMetricStore) with just
// enough behaviour for Runner.Turn to drive a real round in-process: the
// conversation store round-trips an in-memory turn history, the pause/identity
// stores answer the lookups the loop makes, and the cache-metric store is a
// no-op (the audit asserts the prefix invariant, not the metrics).
package main

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
)

// memConvStore is an in-memory conversations.Store stand-in. It keeps ordered
// turns per conversation so LoadHistory / LoadManagedHistory reconstruct them
// byte-for-byte — exactly the shape the real Store yields — without a DB.
type memConvStore struct {
	mu    sync.Mutex
	convs map[string]*conversations.Conversation
	turns map[string][]conversations.AppendTurnParams
}

func newMemConvStore() *memConvStore {
	return &memConvStore{
		convs: map[string]*conversations.Conversation{},
		turns: map[string][]conversations.AppendTurnParams{},
	}
}

func (m *memConvStore) Create(_ context.Context, p conversations.CreateParams) (conversations.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// TitleSet=true so the Runner's auto-title worker (seq>=3) never fires: a
	// title call streams a DIFFERENT system prompt and would both consume a
	// scripted FakeClient turn and race LastRequest() — irrelevant to the prefix
	// invariant the audit asserts.
	c := conversations.Conversation{
		ID: p.ID, IdentityID: p.IdentityID, Status: conversations.StatusActive,
		Model: p.Model, Title: "cache-audit", TitleSet: true,
	}
	m.convs[p.ID] = &c
	return c, nil
}

func (m *memConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok {
		return conversations.Conversation{}, conversations.ErrConversationNotFound
	}
	return *c, nil
}

func (m *memConvStore) List(_ context.Context, includeArchived bool) ([]conversations.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]conversations.Conversation, 0, len(m.convs))
	for _, c := range m.convs {
		if c.Status == conversations.StatusDeleted {
			continue
		}
		if c.Status == conversations.StatusArchived && !includeArchived {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memConvStore) UpdateStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	c.Status = status
	return nil
}

func (m *memConvStore) Rename(_ context.Context, id, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	c.Title, c.TitleSet = title, true
	return nil
}

func (m *memConvStore) SetTitleIfNull(_ context.Context, id, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	if !c.TitleSet {
		c.Title, c.TitleSet = title, true
	}
	return nil
}

func (m *memConvStore) CountTurns(_ context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.turns[id]), nil
}

func (m *memConvStore) AppendTurn(_ context.Context, p conversations.AppendTurnParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[p.ConversationID] = append(m.turns[p.ConversationID], p)
	return nil
}

func (m *memConvStore) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messagesLocked(id), nil
}

func (m *memConvStore) LoadManagedHistory(_ context.Context, id string, _ conversations.ContextConfig) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messagesLocked(id), nil
}

// messagesLocked rebuilds the loop messages from the persisted turns (the same
// shape conversations.Store.LoadHistory yields). Caller holds the lock.
func (m *memConvStore) messagesLocked(id string) []llm.Message {
	return rebuildMessages(m.turns[id])
}

// rebuildMessages reconstructs the llm.Message history from persisted turns,
// decoding the tool_calls jsonb — the in-process equivalent of
// conversations.Store.LoadHistory. Shared by the audit + REPL in-memory fakes.
func rebuildMessages(turns []conversations.AppendTurnParams) []llm.Message {
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg := llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
		if len(t.ToolCalls) > 0 {
			var calls []llm.ToolCall
			_ = json.Unmarshal(t.ToolCalls, &calls)
			msg.ToolCalls = calls
		}
		out = append(out, msg)
	}
	return out
}

func (m *memConvStore) SearchConversationTurns(_ context.Context, _ string, _ int) ([]conversations.SearchResult, error) {
	return nil, nil
}

func (m *memConvStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.convs, id)
	delete(m.turns, id)
	return nil
}

// memPauseStore is an in-memory paused_states stand-in. The audit fixtures never
// pause, but Runner.Stop calls AutoResolveForConversation, so the full surface is
// implemented as harmless no-ops over an in-memory map.
type memPauseStore struct {
	mu      sync.Mutex
	byToken map[string]*askuser.Pending
	answers map[string]askuser.ResumeAnswer
}

func newMemPauseStore() *memPauseStore {
	return &memPauseStore{byToken: map[string]*askuser.Pending{}, answers: map[string]askuser.ResumeAnswer{}}
}

func (m *memPauseStore) Insert(_ context.Context, p askuser.InsertParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byToken[p.Token] = &askuser.Pending{
		Token: p.Token, ConversationID: p.ConversationID, Kind: p.Kind,
		Question: p.Question, Options: p.Options, Priority: p.Priority, ToolCallID: p.ToolCallID,
	}
	return nil
}

func (m *memPauseStore) GetByToken(_ context.Context, token string) (askuser.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byToken[token]
	if !ok {
		return askuser.Pending{}, askuser.ErrPauseNotFound
	}
	return *p, nil
}

func (m *memPauseStore) ListPending(_ context.Context, conversationID string) ([]askuser.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []askuser.Pending
	for tok, p := range m.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, resolved := m.answers[tok]; resolved {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Token < out[j].Token })
	return out, nil
}

func (m *memPauseStore) MarkResumed(_ context.Context, token string, ans askuser.ResumeAnswer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byToken[token]; !ok {
		return askuser.ErrPauseNotFound
	}
	m.answers[token] = ans
	return nil
}

func (m *memPauseStore) MarkResumedBatch(_ context.Context, answers map[string]askuser.ResumeAnswer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for token, ans := range answers {
		if _, ok := m.byToken[token]; !ok {
			return askuser.ErrPauseNotFound
		}
		m.answers[token] = ans
	}
	return nil
}

func (m *memPauseStore) AutoResolveForConversation(_ context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tok, p := range m.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, done := m.answers[tok]; !done {
			m.answers[tok] = askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: "<auto-terminated>"}
		}
	}
	return nil
}

// memIdentityStore answers the single `local` identity lookup the Runner makes
// when it creates the audit conversation.
type memIdentityStore struct{}

func (memIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	if name != "local" {
		return identity.Identity{}, identity.ErrIdentityNotFound
	}
	return identity.Identity{ID: "00000000-0000-0000-0000-000000000001", Name: "local", Kind: "system"}, nil
}

// memCacheMetricStore is a no-op CacheMetricStore — the audit proves the prefix
// invariant, not the metrics write (which is the prod-path / integration tier).
type memCacheMetricStore struct{}

func (memCacheMetricStore) Insert(_ context.Context, _ sqlc.InsertCacheMetricParams) error {
	return nil
}
