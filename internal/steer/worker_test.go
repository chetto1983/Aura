package steer

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

func TestWorkerScopeRejectsUntrustedOrMalformedCoordinates(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	const conv = "22222222-2222-2222-2222-222222222222"
	const run = "run-33333333-3333-3333-3333-333333333333"
	ctx := identityctx.WithIdentityID(context.Background(), owner)
	for _, tc := range []struct {
		name             string
		ctx              context.Context
		conv, child, run string
	}{
		{"missing context", nil, conv, "w1", run},
		{"missing identity", context.Background(), conv, "w1", run},
		{"flat conversation", ctx, conv + "-swarm-w1", "w2", run},
		{"missing child", ctx, conv, "", run},
		{"missing run", ctx, conv, "w1", ""},
		{"malformed run", ctx, conv, "w1", "run-w1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newWorkerScope(tc.ctx, tc.conv, tc.child, tc.run); err == nil {
				t.Fatal("invalid scope was accepted")
			}
		})
	}
	var missing *PostgresStore
	if err := missing.PushWorker(ctx, conv, "w1", run, "change output"); err == nil {
		t.Fatal("missing operation was accepted")
	}
	if _, err := missing.ForWorker(ctx, conv, "w1", run); err == nil {
		t.Fatal("missing store was accepted")
	}
	if _, err := missing.WorkerHistory(ctx, conv, "w1"); err == nil {
		t.Fatal("missing store was accepted")
	}
	var inbox *WorkerInbox
	if len(inbox.Drain("anything")) != 0 {
		t.Fatal("nil inbox returned messages")
	}
	if err := inbox.Close(); err != nil {
		t.Fatal(err)
	}
}
