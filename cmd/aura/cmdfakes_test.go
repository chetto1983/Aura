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
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// cmdConvFake is an in-memory runner.ConversationStore for the REPL tests (no DB).
type cmdConvFake struct {
	mu    sync.Mutex
	convs map[string]*conversations.Conversation
	turns map[string][]conversations.AppendTurnParams
}

func newCmdConvFake() *cmdConvFake {
	return &cmdConvFake{convs: map[string]*conversations.Conversation{}, turns: map[string][]conversations.AppendTurnParams{}}
}

func (f *cmdConvFake) Create(_ context.Context, p conversations.CreateParams) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := conversations.Conversation{ID: p.ID, IdentityID: p.IdentityID, Status: conversations.StatusActive, Model: p.Model}
	f.convs[p.ID] = &c
	return c, nil
}

func (f *cmdConvFake) Get(_ context.Context, id string) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok {
		return conversations.Conversation{}, conversations.ErrConversationNotFound
	}
	return *c, nil
}

func (f *cmdConvFake) List(_ context.Context, includeArchived bool) ([]conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]conversations.Conversation, 0, len(f.convs))
	for _, c := range f.convs {
		if c.Status == conversations.StatusArchived && !includeArchived {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *cmdConvFake) UpdateStatus(_ context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.convs[id]; ok {
		c.Status = status
	}
	return nil
}

func (f *cmdConvFake) Rename(_ context.Context, id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.convs[id]; ok {
		c.Title, c.TitleSet = title, true
	}
	return nil
}

func (f *cmdConvFake) SetTitleIfNull(_ context.Context, id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.convs[id]; ok && !c.TitleSet {
		c.Title, c.TitleSet = title, true
	}
	return nil
}

func (f *cmdConvFake) CountTurns(_ context.Context, id string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.turns[id]), nil
}

func (f *cmdConvFake) AppendTurn(_ context.Context, p conversations.AppendTurnParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendTurnFieldsLocked(p)
	return nil
}

func (f *cmdConvFake) AppendAssistantTurnWithCacheMetric(_ context.Context, p conversations.AppendTurnParams, metric sqlc.InsertCacheMetricParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = f.assignTurnSeqLocked(p)
	if metric.Seq <= 0 {
		metric.Seq = int32(p.Seq)
	}
	f.appendTurnFieldsLocked(p)
	return nil
}

func (f *cmdConvFake) assignTurnSeqLocked(p conversations.AppendTurnParams) conversations.AppendTurnParams {
	if p.Seq <= 0 {
		p.Seq = len(f.turns[p.ConversationID]) + 1
	}
	return p
}

func (f *cmdConvFake) appendTurnFieldsLocked(p conversations.AppendTurnParams) {
	p = f.assignTurnSeqLocked(p)
	f.turns[p.ConversationID] = append(f.turns[p.ConversationID], p)
	if c, ok := f.convs[p.ConversationID]; ok {
		c.TotalInputTokens += int64(p.InputTokens)
		c.TotalOutputTokens += int64(p.OutputTokens)
		c.TotalCostUSD += p.CostUSD
	}
}

func (f *cmdConvFake) messages(id string) []llm.Message {
	turns := f.turns[id]
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

func (f *cmdConvFake) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messages(id), nil
}

func (f *cmdConvFake) LoadManagedHistory(_ context.Context, id string, _ conversations.ContextConfig) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messages(id), nil
}

func (f *cmdConvFake) SearchConversationTurns(_ context.Context, _ string, _ int) ([]conversations.SearchResult, error) {
	return nil, nil
}

func (f *cmdConvFake) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.convs, id)
	delete(f.turns, id)
	return nil
}

// cmdPauseFake is an in-memory runner.PauseStore for the REPL tests.
type cmdPauseFake struct {
	mu      sync.Mutex
	byToken map[string]*askuser.Pending
	answers map[string]askuser.ResumeAnswer
}

func newCmdPauseFake() *cmdPauseFake {
	return &cmdPauseFake{byToken: map[string]*askuser.Pending{}, answers: map[string]askuser.ResumeAnswer{}}
}

func (f *cmdPauseFake) Insert(_ context.Context, p askuser.InsertParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byToken[p.Token] = &askuser.Pending{
		Token: p.Token, ConversationID: p.ConversationID, Kind: p.Kind,
		Question: p.Question, Options: p.Options, Priority: p.Priority, ToolCallID: p.ToolCallID,
	}
	return nil
}

func (f *cmdPauseFake) GetByToken(_ context.Context, token string) (askuser.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byToken[token]
	if !ok {
		return askuser.Pending{}, askuser.ErrPauseNotFound
	}
	return *p, nil
}

func (f *cmdPauseFake) ListPending(_ context.Context, conversationID string) ([]askuser.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []askuser.Pending
	for tok, p := range f.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, done := f.answers[tok]; done {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Token < out[j].Token
	})
	return out, nil
}

func (f *cmdPauseFake) MarkResumed(_ context.Context, token string, ans askuser.ResumeAnswer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byToken[token]; !ok {
		return askuser.ErrPauseNotFound
	}
	f.answers[token] = ans
	return nil
}

func (f *cmdPauseFake) MarkResumedBatch(_ context.Context, answers map[string]askuser.ResumeAnswer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for token, ans := range answers {
		if _, ok := f.byToken[token]; !ok {
			return askuser.ErrPauseNotFound
		}
		f.answers[token] = ans
	}
	return nil
}

func (f *cmdPauseFake) AutoResolveForConversation(_ context.Context, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, p := range f.byToken {
		if p.ConversationID == conversationID {
			if _, done := f.answers[tok]; !done {
				f.answers[tok] = askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: "<auto-terminated>"}
			}
		}
	}
	return nil
}

// cmdIdentityFake is an in-memory runner.IdentityStore for the REPL tests.
type cmdIdentityFake struct{}

func newCmdIdentityFake() *cmdIdentityFake { return &cmdIdentityFake{} }

func (cmdIdentityFake) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	if name != "local" {
		return identity.Identity{}, identity.ErrIdentityNotFound
	}
	return identity.Identity{ID: "00000000-0000-0000-0000-000000000001", Name: "local", Kind: "system"}, nil
}

// cmdCacheMetricFake is an in-memory runner.CacheMetricStore for the REPL tests (no DB).
type cmdCacheMetricFake struct{}

func newCmdCacheMetricFake() *cmdCacheMetricFake { return &cmdCacheMetricFake{} }

func (cmdCacheMetricFake) Insert(_ context.Context, _ sqlc.InsertCacheMetricParams) error {
	return nil
}

// cmdToolInvocationFake is an in-memory runner.ToolInvocationStore for REPL tests.
type cmdToolInvocationFake struct{}

func newCmdToolInvocationFake() *cmdToolInvocationFake { return &cmdToolInvocationFake{} }

func (cmdToolInvocationFake) Insert(_ context.Context, _ toolinvocations.Event) error {
	return nil
}
