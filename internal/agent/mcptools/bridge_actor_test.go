package mcptools

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestActorFromContextDistinguishesParentAndWorker pins the D-10 signal:
// a delegated (worker) dispatch reads role=worker, everything else reads
// role=parent, and RunID is whatever tools.WithRequestID stamped -- never
// invented here.
func TestActorFromContextDistinguishesParentAndWorker(t *testing.T) {
	t.Run("no request id, no delegation marker", func(t *testing.T) {
		got := actorFromContext(context.Background())
		if got.Role != actorRoleParent || got.RunID != "" {
			t.Fatalf("actor = %+v, want {Role:parent, RunID:\"\"}", got)
		}
	})
	t.Run("parent turn", func(t *testing.T) {
		ctx := tools.WithRequestID(context.Background(), "run-parent-1")
		got := actorFromContext(ctx)
		if got.Role != actorRoleParent || got.RunID != "run-parent-1" {
			t.Fatalf("actor = %+v, want {Role:parent, RunID:run-parent-1}", got)
		}
	})
	t.Run("worker dispatch", func(t *testing.T) {
		ctx := tools.WithRequestID(context.Background(), "run-worker-1")
		ctx = tools.WithDelegatedDispatch(ctx)
		got := actorFromContext(ctx)
		if got.Role != actorRoleWorker || got.RunID != "run-worker-1" {
			t.Fatalf("actor = %+v, want {Role:worker, RunID:run-worker-1}", got)
		}
	})
}

// TestActorSessionKeyOnlyDivergesForWorkers pins the scope of the session-pool
// key extension: the operator's own turns keep sharing one session (identical
// key to pre-D-10 behavior); only a worker with a real run id gets a distinct,
// per-run bucket.
func TestActorSessionKeyOnlyDivergesForWorkers(t *testing.T) {
	cases := []struct {
		name  string
		actor mcpActor
		want  string
	}{
		{"parent, no run id", mcpActor{Role: actorRoleParent}, "identity-1"},
		{"parent, with run id", mcpActor{Role: actorRoleParent, RunID: "run-1"}, "identity-1"},
		{"worker, no run id yet", mcpActor{Role: actorRoleWorker}, "identity-1"},
		{"worker, with run id", mcpActor{Role: actorRoleWorker, RunID: "run-w1"}, "identity-1|actor|run-w1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actorSessionKey("identity-1", tc.actor); got != tc.want {
				t.Errorf("actorSessionKey = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("two distinct workers never collide", func(t *testing.T) {
		a := actorSessionKey("identity-1", mcpActor{Role: actorRoleWorker, RunID: "run-w1"})
		b := actorSessionKey("identity-1", mcpActor{Role: actorRoleWorker, RunID: "run-w2"})
		if a == b {
			t.Fatalf("two distinct worker runs collided on key %q", a)
		}
	})
}

// TestActorHeaderFuncNilWhenNoRunID proves a ctx with no derivable run id (no
// turn active) adds no header at all, rather than a header with an empty
// value -- an empty-but-present header is what a naive "always set" version
// would produce and is indistinguishable from a real empty actor on the wire.
func TestActorHeaderFuncNilWhenNoRunID(t *testing.T) {
	if got := actorHeaderFunc(context.Background()); got != nil {
		t.Fatalf("actorHeaderFunc = %v, want nil", got)
	}
}

// TestActorHeaderFuncCarriesRoleAndRunID pins the exact header names
// cmd/arcadedb-mcp reads (bridge_actor.go's actorRunIDHeader/actorRoleHeader).
func TestActorHeaderFuncCarriesRoleAndRunID(t *testing.T) {
	ctx := tools.WithRequestID(context.Background(), "run-w9")
	ctx = tools.WithDelegatedDispatch(ctx)
	got := actorHeaderFunc(ctx)
	if got["X-Aura-Actor-Run-Id"] != "run-w9" {
		t.Errorf("run id header = %q, want run-w9", got["X-Aura-Actor-Run-Id"])
	}
	if got["X-Aura-Actor-Role"] != "worker" {
		t.Errorf("role header = %q, want worker", got["X-Aura-Actor-Role"])
	}
}

func TestRecallContextHeaders(t *testing.T) {
	ctx := tools.WithRequestID(context.Background(), "turn-a")
	ctx = tools.WithToolCallContext(ctx, "conversation-a", "call-a", t.TempDir(), 2048)
	ctx = tools.WithDelegatedDispatch(ctx)
	headersA := memoryHeaderFunc(ctx)
	if headersA[actorRunIDHeader] != "turn-a" || headersA[actorRoleHeader] != actorRoleWorker {
		t.Fatalf("actor headers were not preserved: %v", headersA)
	}
	valueA := headersA[recallContextHeader]
	if valueA == "" {
		t.Fatal("active-source header is absent")
	}
	sourcesA, err := decodeRecallContextHeader(valueA)
	if err != nil {
		t.Fatalf("decode first active-source header: %v", err)
	}
	if len(sourcesA) != 1 || sourcesA[0] != (recallSourceKey{ConversationID: "conversation-a", TurnID: "turn-a"}) {
		t.Fatalf("first active sources = %+v", sourcesA)
	}

	ctx = tools.WithRequestID(context.Background(), "turn-b")
	ctx = tools.WithToolCallContext(ctx, "conversation-b", "call-b", t.TempDir(), 2048)
	headersB := memoryHeaderFunc(ctx)
	valueB := headersB[recallContextHeader]
	if valueB == "" || valueB == valueA {
		t.Fatalf("reused-session headers are stale: first=%q second=%q", valueA, valueB)
	}
	sourcesB, err := decodeRecallContextHeader(valueB)
	if err != nil {
		t.Fatalf("decode second active-source header: %v", err)
	}
	if len(sourcesB) != 1 || sourcesB[0] != (recallSourceKey{ConversationID: "conversation-b", TurnID: "turn-b"}) {
		t.Fatalf("second active sources = %+v", sourcesB)
	}

	if got := memoryHeaderFunc(context.Background()); got != nil {
		t.Fatalf("bare context headers = %v, want nil", got)
	}

	_, err = encodeRecallContextHeader([]recallSourceKey{{
		ConversationID: strings.Repeat("c", 4096), TurnID: "turn-over-cap",
	}})
	if err == nil {
		t.Fatal("over-cap active source encoded successfully")
	}
}
