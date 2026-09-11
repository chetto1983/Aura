package main

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// A pass that expires, prunes or deletes has nothing to do in an identity's memory before
// that identity has written any. The first boot of a fresh install ran the projection and
// retention sweeps over the seeded `local` operator and `aura-cli`, and TenantClients.For
// created mem_…0001 and mem_…0039 to do nothing in them; retiring `local` at the next boot
// left its database with no owner (measured 2026-09-11: ArcadeDB logged both databases at
// 10:11:43 and 10:11:47, before any user existed).
func TestMemoryMaintenanceNeverCreatesADatabase(t *testing.T) {
	fake := &fakeArcadeServer{databases: map[string]bool{}, users: map[string]string{}}
	purger, _, _ := newArcadeMemoryPurger(t, fake)
	clients := arcadedb.NewTenantClients(purger.base, purger.admin, nil, purger.credentials)
	reasoning := &tenantReasoningMemory{clients: clients}
	projection := tenantConversationProjectionSink{clients: clients}
	ctx := t.Context()

	if n, err := reasoning.DeleteExpiredReasoning(ctx, testMemoryIdentityID, time.Now(), 10); err != nil || n != 0 {
		t.Errorf("DeleteExpiredReasoning = %d, %v; want 0, nil", n, err)
	}
	if n, err := reasoning.DeleteReasoningBySource(ctx, arcadedb.ReasoningDeleteSelector{
		IdentityID: testMemoryIdentityID, ConversationID: "c1",
	}); err != nil || n != 0 {
		t.Errorf("DeleteReasoningBySource = %d, %v; want 0, nil", n, err)
	}
	if err := projection.PruneConversationProjections(ctx, testMemoryIdentityID, nil); err != nil {
		t.Errorf("PruneConversationProjections: %v", err)
	}
	if err := projection.DeleteConversationProjection(ctx, testMemoryIdentityID, "c1"); err != nil {
		t.Errorf("DeleteConversationProjection: %v", err)
	}
	if err := projection.DeleteIdentityConversationProjections(ctx, testMemoryIdentityID); err != nil {
		t.Errorf("DeleteIdentityConversationProjections: %v", err)
	}
	if commands := fake.recordedCommands(); len(commands) != 0 {
		t.Fatalf("maintenance on an identity with no memory sent %q", commands)
	}
}
