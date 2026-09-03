package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// fakeConvStore is an in-memory ConversationStore: known thread ids resolve, history
// projects the seeded turns, and any other id maps to ErrConversationNotFound (the 404
// chokepoint). loadErr injects a LoadHistory failure for the error-body test.
type fakeConvStore struct {
	known    map[string]bool
	titles   map[string]string
	history  map[string][]llm.Message
	loadErr  error
	branches []conversations.Branch // ListBranches result (WR-01 membership tests)
	// reasonings seeds ListTurnReasoning (amendment #91 display rehydration);
	// reasoningErr injects a read failure for the fail-soft snapshot test.
	reasonings   map[string][]conversations.TurnReasoning
	reasoningErr error
}

func (f *fakeConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	if f.known[id] {
		title := ""
		titleSet := false
		if f.titles != nil {
			title, titleSet = f.titles[id]
		}
		return conversations.Conversation{ID: id, Title: title, TitleSet: titleSet}, nil
	}
	return conversations.Conversation{}, conversations.ErrConversationNotFound
}

func (f *fakeConvStore) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.history[id], nil
}

func (f *fakeConvStore) ListTurnAttachments(_ context.Context, id string) ([]conversations.TurnAttachments, error) {
	return nil, nil
}

func (f *fakeConvStore) ListTurnReasoning(_ context.Context, id string) ([]conversations.TurnReasoning, error) {
	if f.reasoningErr != nil {
		return nil, f.reasoningErr
	}
	return f.reasonings[id], nil
}

// The CHAT-02 surface widened ConversationStore; the in-memory fake satisfies the
// new methods with no-op/zero returns so the unit suite (which only exercises Get +
// LoadHistory through /agent/run and /threads) keeps compiling. The route→method
// mapping is asserted live in conversations_api_test.go against the real store.
func (f *fakeConvStore) List(context.Context, bool) ([]conversations.Conversation, error) {
	return nil, nil
}

func (f *fakeConvStore) SearchConversationTurns(context.Context, string, int) ([]conversations.SearchResult, error) {
	return nil, nil
}

func (f *fakeConvStore) UpdateStatus(context.Context, string, string) error { return nil }

func (f *fakeConvStore) Rename(context.Context, string, string) error { return nil }

func (f *fakeConvStore) SetTitleIfNull(_ context.Context, id, title string) error {
	if f.titles == nil {
		f.titles = map[string]string{}
	}
	if _, exists := f.titles[id]; !exists {
		f.titles[id] = title
	}
	return nil
}

func (f *fakeConvStore) Delete(context.Context, string) error { return nil }

func (f *fakeConvStore) ListContextRotEvents(context.Context, string) ([]conversations.RotEvent, error) {
	return nil, nil
}

// Phase 36 owner-scoped surface. GetForIdentity mirrors Get (the fake models no ownership)
// so handleRun/handleMessages — which now resolve the thread via GetForIdentity — still find
// a known thread and 404 an unknown one. The rest are no-op success shims (rows-affected==1)
// exercised only for interface satisfaction here.
func (f *fakeConvStore) GetForIdentity(ctx context.Context, id, _ string) (conversations.Conversation, error) {
	return f.Get(ctx, id)
}

func (f *fakeConvStore) ListForIdentity(context.Context, string, bool) ([]conversations.Conversation, error) {
	return nil, nil
}

func (f *fakeConvStore) SearchConversationTurnsForIdentity(context.Context, string, string, int) ([]conversations.SearchResult, error) {
	return nil, nil
}

func (f *fakeConvStore) DeleteForIdentity(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (f *fakeConvStore) UpdateStatusForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}

func (f *fakeConvStore) RenameForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}

func (f *fakeConvStore) UpdateReasoningEffortForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}
