package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/runner"
)

type fakeChatCompact struct {
	preview runner.CompactPreview
	req     runner.CompactRequest
}

func (f *fakeChatCompact) Preview(_ context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.req = req
	return f.preview, nil
}

func (f *fakeChatCompact) Restore(_ context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.req = req
	return runner.CompactPreview{OperationID: req.OperationID, CheckpointID: req.CheckpointID, Status: runner.CompactStatusRecovered}, nil
}

func TestChatCompactCommandsShareCoordinatorAndRejectMidStreamRestore(t *testing.T) {
	service := &fakeChatCompact{preview: runner.CompactPreview{OperationID: "op-1", CheckpointID: "cp-1", PriorCheckpointID: "cp-0", Status: runner.CompactStatusDisabled}}
	var out bytes.Buffer

	for _, command := range []string{"/compact", "/compact history", "/compact preview", "/compact diff cp-1"} {
		handled := dispatchCompactCommand(context.Background(), service, "conv-1", "operator", false, command, &out)
		if !handled {
			t.Fatalf("%q was not intercepted before model dispatch", command)
		}
		if service.req.ConversationID != "conv-1" || service.req.ActorID != "operator" || service.req.Trigger != runner.CompactTriggerManual {
			t.Fatalf("%q request = %+v", command, service.req)
		}
	}

	out.Reset()
	dispatchCompactCommand(context.Background(), service, "conv-1", "operator", true, "/compact restore cp-0", &out)
	if service.req.SafePoint || !strings.Contains(out.String(), "model response") {
		t.Fatalf("mid-stream restore must be rejected before coordinator call, req=%+v out=%q", service.req, out.String())
	}
}
