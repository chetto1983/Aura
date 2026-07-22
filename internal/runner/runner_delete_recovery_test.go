package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
)

type resumableDeleteStore struct {
	*recordingConvStore

	stateMu            sync.Mutex
	conversationID     string
	owner              string
	exists             bool
	reservation        string
	worker             string
	phase              string
	cancelAfterReserve context.CancelFunc
	deleteFailures     int
	deleteCalls        int
	claimSawCanceled   bool
	deleted            chan struct{}
	deleteOnce         sync.Once
}

func (s *resumableDeleteStore) ReserveDeleteForIdentityIfVersion(ctx context.Context, id, identityID string, _ int64, reservation string) (int64, error) {
	s.stateMu.Lock()
	if !s.exists || s.conversationID != id || s.owner != identityID || (s.reservation != "" && s.reservation != reservation) {
		s.stateMu.Unlock()
		return 0, nil
	}
	if s.reservation == "" {
		s.reservation = reservation
		s.phase = "reserved"
	}
	cancel := s.cancelAfterReserve
	s.cancelAfterReserve = nil
	s.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return 1, nil
}

func (s *resumableDeleteStore) ClaimDeleteTeardown(ctx context.Context, id, identityID, reservation, worker string, _ time.Time) (int64, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if ctx.Err() != nil {
		s.claimSawCanceled = true
		return 0, ctx.Err()
	}
	if !s.exists || s.conversationID != id || s.owner != identityID || s.reservation != reservation || (s.worker != "" && s.worker != worker) {
		return 0, nil
	}
	s.worker = worker
	s.phase = "teardown_started"
	return 1, nil
}

func (s *resumableDeleteStore) ReleaseDeleteLease(_ context.Context, _, _, reservation, worker string) (int64, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.reservation != reservation || s.worker != worker {
		return 0, nil
	}
	s.worker = ""
	return 1, nil
}

func (s *resumableDeleteStore) DeleteForIdentityIfReservation(ctx context.Context, id, identityID, reservation, worker string) (int64, error) {
	s.stateMu.Lock()
	if s.reservation != reservation || s.worker != worker || s.phase != "teardown_started" {
		s.stateMu.Unlock()
		return 0, nil
	}
	s.deleteCalls++
	if s.deleteFailures > 0 {
		s.deleteFailures--
		s.stateMu.Unlock()
		return 0, errors.New("injected final delete failure")
	}
	s.stateMu.Unlock()
	affected, err := s.DeleteForIdentity(ctx, id, identityID)
	if err == nil && affected == 1 {
		s.stateMu.Lock()
		s.exists = false
		s.stateMu.Unlock()
		s.deleteOnce.Do(func() { close(s.deleted) })
	}
	return affected, err
}

func (s *resumableDeleteStore) ListReservedDeletes(context.Context, int32) ([]conversations.ReservedDelete, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.exists && s.reservation != "" {
		return []conversations.ReservedDelete{{
			ConversationID: s.conversationID, IdentityID: s.owner, ExpectedVersion: 1,
			Reservation: s.reservation, Phase: s.phase,
		}}, nil
	}
	return []conversations.ReservedDelete{}, nil
}

func newResumableDeleteRunner(t *testing.T, owner, convID string) (*Runner, *resumableDeleteStore) {
	t.Helper()
	r, _, _ := wireLifecycleRecorder(t)
	base := r.Conv.(*recordingConvStore)
	store := &resumableDeleteStore{recordingConvStore: base, conversationID: convID, owner: owner, exists: true, deleted: make(chan struct{})}
	r.Conv = store
	if _, err := store.Create(context.Background(), conversations.CreateParams{ID: convID, IdentityID: owner, Model: "test-model"}); err != nil {
		t.Fatal(err)
	}
	return r, store
}

func TestDeleteReconcilerBootOneShotRecoversStoredOperation(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	convID := newConvID(t)
	r, store := newResumableDeleteRunner(t, owner, convID)
	store.reservation = "stored-operation-reservation"
	store.phase = "reserved"
	reconciler := NewDeleteReconciler(r, time.Hour)
	reconciler.Start(context.Background())
	t.Cleanup(reconciler.Stop)
	select {
	case <-store.deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("boot recovery did not resume stored export-delete")
	}
}

func TestExportDeleteCancellationImmediatelyAfterReserveCompletesDetached(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	convID := newConvID(t)
	r, store := newResumableDeleteRunner(t, owner, convID)
	ctx, cancel := context.WithCancel(exportDeleteTestContext(t, owner, "cancel-after-reserve"))
	store.cancelAfterReserve = cancel

	affected, err := r.DeleteConversationLifecycleIfVersion(ctx, owner, convID, 1)
	if err != nil || affected != 1 {
		t.Fatalf("detached finalization affected=%d err=%v", affected, err)
	}
	if store.claimSawCanceled {
		t.Fatal("teardown inherited the canceled request context")
	}
}

func TestExportDeleteFinalFailureOnlySameOperationCanResume(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	convID := newConvID(t)
	r, store := newResumableDeleteRunner(t, owner, convID)
	store.deleteFailures = 1

	if _, err := r.DeleteConversationLifecycleIfVersion(exportDeleteTestContext(t, owner, "original-operation"), owner, convID, 1); err == nil {
		t.Fatal("injected final delete failure was hidden")
	}
	if affected, err := r.DeleteConversationLifecycleIfVersion(exportDeleteTestContext(t, owner, "different-operation"), owner, convID, 1); err != nil || affected != 0 {
		t.Fatalf("different operation adopted reservation: affected=%d err=%v", affected, err)
	}
	if affected, err := r.DeleteConversationLifecycleIfVersion(exportDeleteTestContext(t, owner, "original-operation"), owner, convID, 1); err != nil || affected != 1 {
		t.Fatalf("original operation did not resume: affected=%d err=%v", affected, err)
	}
	if store.deleteCalls != 2 {
		t.Fatalf("final delete calls=%d, want failed attempt plus same-operation retry", store.deleteCalls)
	}
}

func TestExportDeleteSameOperationRaceHasOneTeardownOwner(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	convID := newConvID(t)
	r, store := newResumableDeleteRunner(t, owner, convID)
	ctx := exportDeleteTestContext(t, owner, "same-operation-race")

	const callers = 16
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_, _ = r.DeleteConversationLifecycleIfVersion(ctx, owner, convID, 1)
		}()
	}
	wg.Wait()
	if store.deleteCalls != 1 {
		t.Fatalf("racing final delete calls=%d, want 1", store.deleteCalls)
	}
}
