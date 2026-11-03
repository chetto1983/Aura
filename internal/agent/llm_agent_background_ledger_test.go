package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

type countedCompletionRegistry struct {
	replayingOperationRegistry
	completedCalls int
}

func (r *countedCompletionRegistry) Complete(ctx context.Context, request idempotency.CompleteRequest) error {
	r.completedCalls++
	return r.replayingOperationRegistry.Complete(ctx, request)
}

func ledgerSnapshot(ledger *recordingLedger) []toolinvocations.Event {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return append([]toolinvocations.Event(nil), ledger.rows...)
}

func TestExecToolRecordsActualDelegatedBackgroundCompletion(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		mutating, canceled, inline, observed bool
		callErr                              error
	}{
		{name: "read success"},
		{name: "read failure", callErr: errors.New("late provider failure")},
		{name: "read canceled", canceled: true},
		{name: "write success", mutating: true},
		{name: "write failure", mutating: true, callErr: errors.New("late provider failure")},
		{name: "write canceled", mutating: true, canceled: true},
		{name: "inline read", inline: true},
		{name: "inline write", inline: true, mutating: true},
		{name: "observed parent", observed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := tools.NewBackgroundCalls()
			defer stopBackground(t, calls)
			completed := make(chan struct{}, 1)
			calls.SetCompletionHook(func(tools.BackgroundCallCompletion) { completed <- struct{}{} })
			release := make(chan struct{})
			if tc.inline {
				close(release)
			}
			tool := evidenceTool{mutating: tc.mutating, execute: func(ctx context.Context) (tools.ToolResult, error) {
				if tc.inline {
					<-time.After(12 * time.Millisecond)
				}
				select {
				case <-release:
					return tools.ToolResult{Preview: "actual late result", Bytes: 18}, tc.callErr
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				}
			}}
			ledger := &recordingLedger{}
			gw := gateway.New(config.ProfileSingleUserHardened, ledger)
			registry := &countedCompletionRegistry{}
			if tc.mutating {
				gw.SetOperationRegistry(registry)
			}
			a := &LlmAgent{gateway: gw, background: calls, ledgerConvID: "11111111-1111-1111-1111-111111111111"}
			args := json.RawMessage(`{"period":"1y"}`)
			ctx := evidenceContext(t, tool, args)
			if tc.observed {
				ctx = tools.WithRequestID(t.Context(), "22222222-2222-2222-2222-222222222222")
			}
			ctx = tools.WithToolCallContext(ctx, "worker", "call-evidence", t.TempDir(), 4096)
			window := 12 * time.Millisecond
			if tc.inline {
				window = time.Second
			}
			ctx = context.WithValue(ctx, backgroundLimitsKey{}, backgroundLimits{window: window, ceiling: time.Second})
			result, err := a.execTool(ctx, tool, tc.mutating, args)
			if err != nil || (!tc.inline && !strings.Contains(result.Preview, `"status":"in_progress"`)) {
				t.Fatalf("promotion = %+v, %v", result, err)
			}
			if rows := ledgerSnapshot(ledger); !tc.inline && len(rows) != 1 {
				t.Errorf("running call already has terminal row: %+v", rows)
			}
			if !tc.inline && (registry.completed != nil || registry.marked != 0 || registry.rejected != nil) {
				t.Error("running background mutation finalized its operation before Execute settled")
			}
			if tc.canceled {
				stopBackground(t, calls)
			} else if !tc.inline {
				close(release)
				select {
				case <-completed:
				case <-time.After(2 * time.Second):
					t.Fatal("background execution did not settle")
				}
			}
			status, errText := "ok", ""
			if tc.callErr != nil {
				status, errText = "error", tc.callErr.Error()
			}
			if tc.canceled {
				status, errText = "error", "context canceled"
			}
			rows := ledgerSnapshot(ledger)
			if tc.observed {
				if len(rows) != 1 {
					t.Fatalf("gateway wrote %d rows for observed parent; Runner owns its end", len(rows))
				}
				return
			}
			assertDispatchEvidence(t, rows, args, status, errText)
			if !tc.canceled && rows[1].ResultPreview != "actual late result" {
				t.Errorf("recorded notice instead of terminal result: %q", rows[1].ResultPreview)
			}
			if tc.mutating {
				if status == "ok" && (registry.completed == nil || registry.completedCalls != 1 || registry.marked != 0) {
					t.Error("successful background effect was not completed")
				}
				if status == "error" && (registry.completed != nil || registry.marked != 1) {
					t.Error("failed background effect did not become indeterminate")
				}
			}
		})
	}
}
