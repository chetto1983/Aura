package runner

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

type recordingReasoningDeletion struct {
	steps     *stepLog
	selectors []arcadedb.ReasoningDeleteSelector
	err       error
}

func (s *recordingReasoningDeletion) DeleteReasoningBySource(
	_ context.Context,
	selector arcadedb.ReasoningDeleteSelector,
) (int, error) {
	s.steps.add("reasoning-delete:" + selector.IdentityID + ":" + selector.ConversationID)
	s.selectors = append(s.selectors, selector)
	return 1, s.err
}

func TestReasoningDeletionPrecedence(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	t.Run("owned conversation deletes reasoning before source persistence", func(t *testing.T) {
		r, _, steps := wireLifecycleRecorder(t)
		convID := newConvID(t)
		seedOwnedConversation(t, r, owner, convID)
		store := &recordingReasoningDeletion{steps: steps}
		r.reasoningDeletion = store
		affected, err := r.DeleteConversationLifecycle(context.Background(), owner, convID)
		if err != nil || affected != 1 {
			t.Fatalf("delete affected=%d err=%v", affected, err)
		}
		got := steps.snapshot()
		wantTail := []string{"reasoning-delete:" + owner + ":" + convID, "delete"}
		if len(got) < 2 || fmt.Sprint(got[len(got)-2:]) != fmt.Sprint(wantTail) {
			t.Fatalf("delete tail = %v, want %v", got, wantTail)
		}
		if len(store.selectors) != 1 || store.selectors[0].IdentityID != owner ||
			store.selectors[0].ConversationID != convID {
			t.Fatalf("reasoning selector = %+v", store.selectors)
		}
	})

	t.Run("reasoning failure preserves the authoritative source for retry", func(t *testing.T) {
		r, _, steps := wireLifecycleRecorder(t)
		convID := newConvID(t)
		seedOwnedConversation(t, r, owner, convID)
		r.reasoningDeletion = &recordingReasoningDeletion{steps: steps, err: errors.New("graph unavailable")}
		if _, err := r.DeleteConversationLifecycle(context.Background(), owner, convID); err == nil {
			t.Fatal("reasoning deletion failure was hidden")
		}
		if _, err := r.Conv.GetForIdentity(context.Background(), convID, owner); err != nil {
			t.Fatalf("source disappeared before derived deletion could be retried: %v", err)
		}
	})

	t.Run("foreign owner gate performs no reasoning deletion", func(t *testing.T) {
		r, _, steps := wireLifecycleRecorder(t)
		convID := newConvID(t)
		seedOwnedConversation(t, r, owner, convID)
		store := &recordingReasoningDeletion{steps: steps}
		r.reasoningDeletion = store
		if affected, err := r.DeleteConversationLifecycle(context.Background(), "identity-b", convID); err != nil || affected != 0 {
			t.Fatalf("foreign delete affected=%d err=%v", affected, err)
		}
		if len(store.selectors) != 0 {
			t.Fatalf("foreign delete reached reasoning store: %+v", store.selectors)
		}
	})
}
