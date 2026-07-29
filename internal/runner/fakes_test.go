package runner

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
)

// fakeCacheMetricStore is a hand-written in-memory CacheMetricStore (D-A2-02 fakes). It
// records each persisted metric so the persist-seam tests can assert one row per turn,
// and supports an injectable error for the failure path.
type fakeCacheMetricStore struct {
	mu       sync.Mutex
	inserts  []sqlc.InsertCacheMetricParams
	insertEr error
}

func newFakeCacheMetricStore() *fakeCacheMetricStore {
	return &fakeCacheMetricStore{}
}

func (f *fakeCacheMetricStore) Insert(_ context.Context, p sqlc.InsertCacheMetricParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertEr != nil {
		return f.insertEr
	}
	f.inserts = append(f.inserts, p)
	return nil
}

func (f *fakeCacheMetricStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inserts)
}

// fakeConvStore is a hand-written in-memory ConversationStore (D-A2-02 fakes). It
// keeps ordered turns per conversation so LoadHistory reconstructs them, and tracks
// the title + aggregates the Runner sets. No DB → supports the 85% coverage floor.
type fakeConvStore struct {
	mu        sync.Mutex
	convs     map[string]*conversations.Conversation
	turns     map[string][]conversations.AppendTurnParams
	cache     *fakeCacheMetricStore
	titleErr  error // injectable SetTitleIfNull error (auto-title resilience test)
	manErr    error // injectable LoadManagedHistory error
	countErr  error // injectable CountTurns error
	appendEr  error // injectable AppendTurn error
	failAppN  int   // when >0, fail the Nth AppendTurn call with appendEr/errFake
	appendN   int   // AppendTurn call counter
	createEr  error // injectable Create error (EnsureConversation classification test)
	createRow bool  // when true, store the row even though Create returns createEr (a lost concurrent-create race the re-Get reconciles)
	lastCfg   conversations.ContextConfig
	// lastBranchLeaf records the leaf passed to LoadManagedHistoryForBranch so the
	// TurnBranch re-run-from-branch path is assertable (D-09).
	lastBranchLeaf int
}

func newFakeConvStore() *fakeConvStore {
	return &fakeConvStore{
		convs: map[string]*conversations.Conversation{},
		turns: map[string][]conversations.AppendTurnParams{},
	}
}

func (f *fakeConvStore) Create(_ context.Context, p conversations.CreateParams) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := conversations.Conversation{ID: p.ID, IdentityID: p.IdentityID, Status: conversations.StatusActive, Model: p.Model}
	if f.createEr != nil {
		if f.createRow {
			f.convs[p.ID] = &c // a concurrent creator won the race: the row exists despite our error
		}
		return conversations.Conversation{}, f.createEr
	}
	f.convs[p.ID] = &c
	return c, nil
}

func (f *fakeConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok {
		return conversations.Conversation{}, conversations.ErrConversationNotFound
	}
	return *c, nil
}

func (f *fakeConvStore) List(_ context.Context, includeArchived bool) ([]conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]conversations.Conversation, 0, len(f.convs))
	for _, c := range f.convs {
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

func (f *fakeConvStore) UpdateStatus(_ context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	c.Status = status
	return nil
}

func (f *fakeConvStore) Rename(_ context.Context, id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	c.Title, c.TitleSet = title, true
	return nil
}

func (f *fakeConvStore) SetTitleIfNull(_ context.Context, id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.titleErr != nil {
		return f.titleErr
	}
	c, ok := f.convs[id]
	if !ok {
		return conversations.ErrConversationNotFound
	}
	if !c.TitleSet {
		c.Title, c.TitleSet = title, true
	}
	return nil
}

func (f *fakeConvStore) CountTurns(_ context.Context, id string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countErr != nil {
		return 0, f.countErr
	}
	return len(f.turns[id]), nil
}

func (f *fakeConvStore) AppendTurn(_ context.Context, p conversations.AppendTurnParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appendTurnLocked(p)
}

func (f *fakeConvStore) AppendAssistantTurnWithCacheMetric(_ context.Context, p conversations.AppendTurnParams, metric sqlc.InsertCacheMetricParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendN++
	if f.appendEr != nil {
		return f.appendEr
	}
	if f.failAppN > 0 && f.appendN == f.failAppN {
		return errFake
	}
	if f.cache != nil {
		f.cache.mu.Lock()
		if f.cache.insertEr != nil {
			f.cache.mu.Unlock()
			return f.cache.insertEr
		}
		f.cache.mu.Unlock()
	}
	p = f.assignTurnSeqLocked(p)
	if metric.Seq <= 0 {
		metric.Seq = int32(p.Seq)
	}
	f.appendTurnFieldsLocked(p)
	if f.cache != nil {
		f.cache.mu.Lock()
		f.cache.inserts = append(f.cache.inserts, metric)
		f.cache.mu.Unlock()
	}
	return nil
}

func (f *fakeConvStore) appendTurnLocked(p conversations.AppendTurnParams) error {
	f.appendN++
	if f.appendEr != nil {
		return f.appendEr
	}
	if f.failAppN > 0 && f.appendN == f.failAppN {
		return errFake
	}
	f.appendTurnFieldsLocked(p)
	return nil
}

func (f *fakeConvStore) assignTurnSeqLocked(p conversations.AppendTurnParams) conversations.AppendTurnParams {
	if p.Seq <= 0 {
		p.Seq = len(f.turns[p.ConversationID]) + 1
	}
	return p
}

func (f *fakeConvStore) appendTurnFieldsLocked(p conversations.AppendTurnParams) {
	p = f.assignTurnSeqLocked(p)
	f.turns[p.ConversationID] = append(f.turns[p.ConversationID], p)
	c, ok := f.convs[p.ConversationID]
	if ok {
		c.TotalInputTokens += int64(p.InputTokens)
		c.TotalOutputTokens += int64(p.OutputTokens)
		c.TotalCachedTokens += int64(p.CachedTokens)
		c.TotalCostUSD += p.CostUSD
	}
}

func (f *fakeConvStore) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messagesLocked(id), nil
}

func (f *fakeConvStore) LoadManagedHistory(_ context.Context, id string, cfg conversations.ContextConfig) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCfg = cfg
	if f.manErr != nil {
		return nil, f.manErr
	}
	return managedMessagesForFake(f.messagesLocked(id), cfg), nil
}

// LoadManagedHistoryForBranch records the selected leaf so the branch-aware re-run path
// (TurnBranch) is assertable, then returns the same in-memory history as the linear
// loader (the fake has no branch topology — the path-walk fidelity is the conversations
// integration test's job; here we only prove TurnBranch routes through this method).
func (f *fakeConvStore) LoadManagedHistoryForBranch(_ context.Context, id string, leafSeq int, cfg conversations.ContextConfig) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCfg = cfg
	f.lastBranchLeaf = leafSeq
	if f.manErr != nil {
		return nil, f.manErr
	}
	return managedMessagesForFake(f.messagesLocked(id), cfg), nil
}

func managedMessagesForFake(
	messages []llm.Message,
	cfg conversations.ContextConfig,
) []llm.Message {
	if cfg.DynamicTail == nil || cfg.DynamicTail.ID == "" ||
		cfg.DynamicTail.Content == "" {
		return messages
	}
	index := len(messages)
	if cfg.DynamicTail.BeforeCurrentUser {
		for candidate, v := range slices.Backward(messages) {
			if v.Role == llm.RoleUser {
				index = candidate
				break
			}
		}
	}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, messages[:index]...)
	out = append(out, llm.Message{
		Role: llm.RoleUser, Content: cfg.DynamicTail.Content,
		DynamicTailID: cfg.DynamicTail.ID,
	})
	out = append(out, messages[index:]...)
	return out
}

// messagesLocked rebuilds the loop messages from the persisted turns (the same
// shape conversations.Store.LoadHistory yields). Caller holds the lock.
func (f *fakeConvStore) messagesLocked(id string) []llm.Message {
	turns := f.turns[id]
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg := llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
		if len(t.ToolCalls) > 0 {
			msg.ToolCalls = decodeToolCallsForFake(t.ToolCalls)
		}
		out = append(out, msg)
	}
	return out
}

func (f *fakeConvStore) SearchConversationTurns(_ context.Context, query string, limit int) ([]conversations.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []conversations.SearchResult
	for id, turns := range f.turns {
		for _, t := range turns {
			if t.Content != "" && containsFold(t.Content, query) {
				out = append(out, conversations.SearchResult{
					ConversationID: id, Seq: t.Seq, Content: t.Content, Similarity: 1.0,
				})
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeConvStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.convs, id)
	delete(f.turns, id)
	return nil
}

// GetForIdentity models the Phase-36 owner gate: a conversation is visible only to its owner
// (identityID == the stored IdentityID); a foreign or absent id is ErrConversationNotFound
// (D-06 read = 404). The delete lifecycle's ownership gate exercises this.
func (f *fakeConvStore) GetForIdentity(_ context.Context, id, identityID string) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok || c.IdentityID != identityID {
		return conversations.Conversation{}, conversations.ErrConversationNotFound
	}
	return *c, nil
}

// DeleteForIdentity models the owner-scoped hard delete: it removes + returns 1 only when
// identityID owns the id, else 0 rows (the not-owned signal the surface maps to 403/404).
func (f *fakeConvStore) DeleteForIdentity(_ context.Context, id, identityID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok || c.IdentityID != identityID {
		return 0, nil
	}
	delete(f.convs, id)
	delete(f.turns, id)
	return 1, nil
}

// fakePauseStore is a hand-written in-memory PauseStore (D-A2-02 fakes). It keeps
// pending rows per conversation and resolves them in FIFO order.
type fakePauseStore struct {
	mu         sync.Mutex
	byToken    map[string]*askuser.Pending
	answers    map[string]askuser.ResumeAnswer
	inserts    int // number of Insert calls (sole-writer assertion)
	insertEr   error
	lastInsert askuser.InsertParams // the most recent Insert arg (proxied-id forwarding assertion)
}

func newFakePauseStore() *fakePauseStore {
	return &fakePauseStore{byToken: map[string]*askuser.Pending{}, answers: map[string]askuser.ResumeAnswer{}}
}

func (f *fakePauseStore) Insert(_ context.Context, p askuser.InsertParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertEr != nil {
		return f.insertEr
	}
	f.inserts++
	f.lastInsert = p
	f.byToken[p.Token] = &askuser.Pending{
		Token: p.Token, ConversationID: p.ConversationID, Kind: p.Kind,
		Question: p.Question, Options: p.Options, Priority: p.Priority, ToolCallID: p.ToolCallID,
		ResumeContext: p.ResumeContext,
	}
	return nil
}

func (f *fakePauseStore) GetByToken(_ context.Context, token string) (askuser.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byToken[token]
	if !ok {
		return askuser.Pending{}, askuser.ErrPauseNotFound
	}
	return *p, nil
}

func (f *fakePauseStore) ListPending(_ context.Context, conversationID string) ([]askuser.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []askuser.Pending
	for tok, p := range f.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, resolved := f.answers[tok]; resolved {
			continue
		}
		out = append(out, *p)
	}
	// FIFO: priority DESC, token ASC (created_at is now-tied in tests).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Token < out[j].Token
	})
	return out, nil
}

func (f *fakePauseStore) MarkResumed(_ context.Context, token string, ans askuser.ResumeAnswer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byToken[token]; !ok {
		return askuser.ErrPauseNotFound
	}
	if _, done := f.answers[token]; done {
		return askuser.ErrPauseNotFound
	}
	f.answers[token] = ans
	return nil
}

func (f *fakePauseStore) MarkResumedBatch(_ context.Context, answers map[string]askuser.ResumeAnswer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for token := range answers {
		if _, ok := f.byToken[token]; !ok {
			return askuser.ErrPauseNotFound
		}
		if _, done := f.answers[token]; done {
			return askuser.ErrPauseNotFound
		}
	}
	maps.Copy(f.answers, answers)
	return nil
}

func (f *fakePauseStore) AutoResolveForConversation(_ context.Context, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, p := range f.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, done := f.answers[tok]; !done {
			f.answers[tok] = askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: "<auto-terminated>"}
		}
	}
	return nil
}

// unresolvedCount reports how many pending rows remain unresolved for a
// conversation (the Stop/SubmitAnswer assertions read this).
func (f *fakePauseStore) unresolvedCount(conversationID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for tok, p := range f.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, done := f.answers[tok]; !done {
			n++
		}
	}
	return n
}

// fakeIdentityStore is a hand-written in-memory IdentityStore (D-A2-02 fakes).
type fakeIdentityStore struct {
	byName map[string]identity.Identity
}

func newFakeIdentityStore() *fakeIdentityStore {
	return &fakeIdentityStore{byName: map[string]identity.Identity{
		"local": {ID: "00000000-0000-0000-0000-000000000001", Name: "local", Kind: "system"},
	}}
}

func (f *fakeIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	id, ok := f.byName[name]
	if !ok {
		return identity.Identity{}, identity.ErrIdentityNotFound
	}
	return id, nil
}

func (f *fakeIdentityStore) ListIdentities(context.Context) ([]identity.Identity, error) {
	out := make([]identity.Identity, 0, len(f.byName))
	for _, id := range f.byName {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (f *fakeIdentityStore) GetIdentityByID(_ context.Context, identityID string) (identity.Identity, error) {
	for _, id := range f.byName {
		if id.ID == identityID {
			return id, nil
		}
	}
	return identity.Identity{}, identity.ErrIdentityNotFound
}

// errFake is a reusable injectable error for the fakes.
var errFake = errors.New("fake error")
