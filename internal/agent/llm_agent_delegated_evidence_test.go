package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

type evidenceTool struct {
	mutating bool
	execute  func(context.Context) (tools.ToolResult, error)
}

func (t evidenceTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "evidence_probe", Mutating: t.mutating,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (t evidenceTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return t.execute(ctx)
}

func evidenceContext(t *testing.T, tool tools.Tool, args json.RawMessage) context.Context {
	t.Helper()
	ctx := identityctx.WithIdentityID(t.Context(), identityctx.LocalOperatorIdentity)
	if tool.Spec().Mutating {
		fingerprint, err := tools.OperationFingerprint(tool.Spec(), args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = idempotency.WithOperation(ctx, idempotency.Operation{
			Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: tools.OperationScopeAgent, Key: "ledger-probe"},
			Fingerprint: fingerprint,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx = tools.WithRequestID(ctx, "22222222-2222-2222-2222-222222222222")
	return gateway.WithDelegatedDispatch(tools.WithToolCallContext(ctx, "worker", "call-evidence", "", 0))
}

func assertDispatchEvidence(t *testing.T, rows []toolinvocations.Event, args json.RawMessage, status, errText string) {
	t.Helper()
	if len(rows) != 2 || rows[0].Event != toolinvocations.EventStart || rows[1].Event != toolinvocations.EventEnd {
		t.Fatalf("dispatch rows = %+v, want exactly one start and one end", rows)
	}
	start, end := rows[0], rows[1]
	for _, row := range rows {
		if row.Arguments != string(args) || row.ArgsBytes != len(args) {
			t.Errorf("%s arguments = %q/%d, want actual dispatch %q/%d", row.Event, row.Arguments, row.ArgsBytes, args, len(args))
		}
	}
	if end.StartedAt.IsZero() || !end.StartedAt.Equal(start.StartedAt) || end.EndedAt.Before(start.StartedAt) {
		t.Errorf("start/end timing = %s / %s -> %s", start.StartedAt, end.StartedAt, end.EndedAt)
	}
	if end.DurationMS < 10 || end.DurationMS != end.EndedAt.Sub(end.StartedAt).Milliseconds() {
		t.Errorf("duration = %dms, want actual elapsed time >=10ms", end.DurationMS)
	}
	if end.Status != status || end.Error != errText {
		t.Errorf("outcome = %q/%q, want %q/%q", end.Status, end.Error, status, errText)
	}
}

func TestExecToolPreservesDelegatedInvocationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, status                                     string
		mutating, registry, canceled, bookkeepingFailure bool
		callErr                                          error
	}{
		{name: "read success", status: "ok"},
		{name: "read failure", status: "error", callErr: errors.New("provider unavailable")},
		{name: "read canceled", status: "error", canceled: true, callErr: context.Canceled},
		{name: "write without registry", status: "ok", mutating: true},
		{name: "write success", status: "ok", mutating: true, registry: true},
		{name: "write failure", status: "error", mutating: true, registry: true, callErr: errors.New("provider unavailable")},
		{name: "write canceled", status: "error", mutating: true, registry: true, canceled: true, callErr: context.Canceled},
		{name: "write rejected", status: "error", mutating: true, registry: true, callErr: &mcp.ToolCallError{Server: "broker", Tool: "analysis", Outcome: mcp.ToolOutcomeRejected, Code: "invalid_argument", Message: "period invalid", Effect: mcp.ToolEffectNone}},
		{name: "completion failed after effect", status: "ok", mutating: true, registry: true, bookkeepingFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger := &recordingLedger{}
			gw := gateway.New(config.ProfileSingleUserHardened, ledger)
			registry := &replayingOperationRegistry{}
			if tc.bookkeepingFailure {
				registry.completeErr = errors.New("registry unavailable")
			}
			if tc.registry {
				gw.SetOperationRegistry(registry)
			}
			var cancel context.CancelFunc
			executions := 0
			tool := evidenceTool{mutating: tc.mutating, execute: func(ctx context.Context) (tools.ToolResult, error) {
				executions++
				<-time.After(12 * time.Millisecond)
				if tc.canceled {
					cancel()
					if ctx.Err() != context.Canceled {
						t.Fatal("execution did not observe cancellation")
					}
				}
				return tools.ToolResult{Preview: "partial or complete output", Bytes: 26}, tc.callErr
			}}
			args := json.RawMessage(`{"period":"1y","symbols":["AAPL"]}`)
			ctx, stop := context.WithCancel(evidenceContext(t, tool, args))
			cancel = stop
			defer stop()
			a := &LlmAgent{gateway: gw, ledgerConvID: "11111111-1111-1111-1111-111111111111"}
			_, err := a.execTool(ctx, tool, tc.mutating, args)
			if tc.callErr != nil && !errors.Is(err, tc.callErr) {
				t.Fatalf("execution error = %v, want %v", err, tc.callErr)
			}
			if tc.callErr == nil && !tc.bookkeepingFailure && err != nil {
				t.Fatal(err)
			}
			if tc.bookkeepingFailure && !errors.Is(err, registry.completeErr) {
				t.Fatalf("completion error = %v", err)
			}
			errText := ""
			if tc.callErr != nil {
				errText = tc.callErr.Error()
			}
			assertDispatchEvidence(t, ledger.rows, args, tc.status, errText)
			if len(ledger.rows) == 2 && ledger.rows[1].ResultPreview != "partial or complete output" {
				t.Error("actual result was lost")
			}
			if tc.name == "write success" {
				if _, err := a.execTool(ctx, tool, true, args); err != nil {
					t.Fatal(err)
				}
				if executions != 1 || len(ledger.rows) != 2 {
					t.Fatalf("replay executed %d times with %d ledger rows", executions, len(ledger.rows))
				}
			}
		})
	}
}

func TestExecToolMeasuresOneDelegatedDispatchAcrossRetries(t *testing.T) {
	ledger := &recordingLedger{}
	a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, ledger), ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	attempts := 0
	tool := evidenceTool{execute: func(context.Context) (tools.ToolResult, error) {
		attempts++
		if attempts == 1 {
			return tools.ToolResult{}, timeoutErr{}
		}
		return tools.ToolResult{Preview: "retried result"}, nil
	}}
	args := json.RawMessage(`{"period":"1y"}`)
	if _, err := a.execTool(evidenceContext(t, tool, args), tool, false, args); err != nil {
		t.Fatal(err)
	}
	assertDispatchEvidence(t, ledger.rows, args, "ok", "")
	if attempts != 2 || ledger.rows[1].DurationMS < toolRetryBaseDelay.Milliseconds() {
		t.Fatalf("attempts/duration = %d/%dms, want one dispatch including its retry backoff", attempts, ledger.rows[1].DurationMS)
	}
}

func TestExecToolClosesDelegatedCallCanceledDuringRetryBackoff(t *testing.T) {
	ledger := &recordingLedger{}
	a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, ledger), ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	tool := evidenceTool{execute: func(context.Context) (tools.ToolResult, error) { return tools.ToolResult{}, timeoutErr{} }}
	args := json.RawMessage(`{"period":"1y"}`)
	ctx, cancel := context.WithCancel(evidenceContext(t, tool, args))
	defer cancel()
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()
	_, err := a.execTool(ctx, tool, false, args)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("backoff error = %v, want cancellation", err)
	}
	assertDispatchEvidence(t, ledger.rows, args, "error", "context canceled")
}

func TestExecToolRecordsDelegatedInlineFailure(t *testing.T) {
	ledger := &recordingLedger{}
	a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, ledger), ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	meta := tools.ToolResultMeta{tools.MetaToolError: "provider rejected period"}
	tool := evidenceTool{execute: func(context.Context) (tools.ToolResult, error) {
		<-time.After(12 * time.Millisecond)
		return tools.ToolResult{Meta: &meta}, nil
	}}
	args := json.RawMessage(`{"period":"1d"}`)
	if _, err := a.execTool(evidenceContext(t, tool, args), tool, false, args); err != nil {
		t.Fatal(err)
	}
	assertDispatchEvidence(t, ledger.rows, args, "error", "provider rejected period")
}

func TestExecToolRecordsDelegatedPanicAsFailure(t *testing.T) {
	ledger := &recordingLedger{}
	a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, ledger), ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	tool := evidenceTool{execute: func(context.Context) (tools.ToolResult, error) {
		<-time.After(12 * time.Millisecond)
		panic("provider panic")
	}}
	args := json.RawMessage(`{"period":"1y"}`)
	defer func() {
		if got := recover(); got != "provider panic" {
			t.Errorf("panic = %v, want original panic", got)
		}
		assertDispatchEvidence(t, ledger.rows, args, "error", "tool panic: provider panic")
	}()
	_, _ = a.execTool(evidenceContext(t, tool, args), tool, false, args)
}
