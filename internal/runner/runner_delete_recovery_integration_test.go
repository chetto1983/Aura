//go:build db_integration

package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
)

var errSimulatedProcessExitAfterReserve = errors.New("simulated process exit after durable reserve")

type crashAfterReserveConversationStore struct {
	*conversations.Store
}

func (*crashAfterReserveConversationStore) ClaimDeleteTeardown(context.Context, string, string, string, string, time.Time) (int64, error) {
	return 0, errSimulatedProcessExitAfterReserve
}

func TestExportDeleteProcessRestartRecoversDurableReservation(t *testing.T) {
	pool := migratedRunnerPool(t)
	first, store, _ := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	conversationID := newIntegrationConversation(t, pool, store)
	version, err := store.VersionForIdentity(context.Background(), conversationID, localIdentityID)
	if err != nil {
		t.Fatal(err)
	}
	first.Conv = &crashAfterReserveConversationStore{Store: store}
	ctx := exportDeleteTestContext(t, localIdentityID, "restart-recovery")
	if _, err := first.DeleteConversationLifecycleIfVersion(ctx, localIdentityID, conversationID, version); !errors.Is(err, errSimulatedProcessExitAfterReserve) {
		t.Fatalf("first process error=%v, want simulated post-reserve exit", err)
	}
	reserved, err := store.ListReservedDeletes(context.Background(), 10)
	if err != nil || len(reserved) != 1 || reserved[0].ConversationID != conversationID || reserved[0].Phase != "reserved" {
		t.Fatalf("durable reservation after exit=%+v err=%v", reserved, err)
	}

	restarted, restartedStore, _ := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	completed, err := restarted.reconcileReservedConversationDeletes(context.Background(), 10)
	if err != nil || completed != 1 {
		t.Fatalf("restart recovery completed=%d err=%v", completed, err)
	}
	if _, err := restartedStore.GetForIdentity(context.Background(), conversationID, localIdentityID); !errors.Is(err, conversations.ErrConversationNotFound) {
		t.Fatalf("conversation survived restart reconciliation: %v", err)
	}
}
