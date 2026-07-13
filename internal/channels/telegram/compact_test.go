package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/runner"
)

type fakeTelegramCompact struct {
	req runner.CompactRequest
}

func (f *fakeTelegramCompact) Preview(_ context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.req = req
	return runner.CompactPreview{OperationID: req.OperationID, CheckpointID: "cp-1", PriorCheckpointID: "cp-0", Status: runner.CompactStatusDisabled}, nil
}

func (f *fakeTelegramCompact) Restore(_ context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.req = req
	return runner.CompactPreview{OperationID: req.OperationID, CheckpointID: req.CheckpointID, Status: runner.CompactStatusRecovered}, nil
}

func TestTelegramCompactLocalizedAndSafePointBound(t *testing.T) {
	service := &fakeTelegramCompact{}
	for _, command := range []string{"compact", "history", "preview", "diff cp-1"} {
		reply := dispatchCompact(context.Background(), service, "conv-1", "operator", false, command)
		if !strings.Contains(reply, "Compattazione") || service.req.Trigger != runner.CompactTriggerManual {
			t.Fatalf("%q reply=%q req=%+v", command, reply, service.req)
		}
	}
	before := service.req
	reply := dispatchCompact(context.Background(), service, "conv-1", "operator", true, "restore cp-0")
	if service.req != before || !strings.Contains(reply, "risposta") {
		t.Fatalf("mid-stream restore reached coordinator or lacked localized rejection: %q", reply)
	}
}

func TestTelegramCompactUsesItalianMessageCatalog(t *testing.T) {
	reply := dispatchCompact(context.Background(), nil, "", "", false, "")
	if reply != compactMessagesItalian.unavailable {
		t.Fatalf("reply = %q", reply)
	}
}
