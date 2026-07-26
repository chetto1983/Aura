package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestCLIExpiredRecoveryIsEnabledOnlyForAdaptiveAdmission(t *testing.T) {
	t.Parallel()
	for command, meta := range cliMutationCommands {
		want := command == "eval adaptive seal-admission"
		if meta.RecoverExpired != want {
			t.Fatalf(
				"%q RecoverExpired = %t, want %t",
				command,
				meta.RecoverExpired,
				want,
			)
		}
		executePointer := reflect.ValueOf(meta.Execute).Pointer()
		if want {
			if executePointer != reflect.ValueOf(
				executeCLIAdaptiveSealAdmission,
			).Pointer() {
				t.Fatalf("%q does not use the in-process claimed executor", command)
			}
		} else if executePointer != reflect.ValueOf(executeCLIChild).Pointer() {
			t.Fatalf("%q unexpectedly changed its subprocess executor", command)
		}
	}
}

func TestRunCLIIdempotentRecoversOnlyAdaptiveExpiredParent(t *testing.T) {
	t.Parallel()
	request := testCLIBeginRequest(t, "adaptive-expired-parent")
	registry := &cliMemoryRegistry{
		request: &request,
		recover: true,
	}
	ctx := cliOperationContextForTest(t, request)
	executions := 0
	executor := func(
		ctx context.Context,
		_ []string,
		_ io.Writer,
		_ io.Writer,
	) int {
		executions++
		operation, ok := idempotency.OperationFromContext(ctx)
		if !ok || operation.ClaimToken != 2 {
			t.Fatalf(
				"recovered executor operation = %+v/%t, want claim token 2",
				operation,
				ok,
			)
		}
		return 0
	}
	if code := runCLIIdempotent(
		ctx,
		[]string{"eval", "adaptive", "seal-admission"},
		io.Discard,
		io.Discard,
		registry,
		executor,
		true,
	); code != 0 {
		t.Fatalf("recovered adaptive parent exit = %d", code)
	}
	if registry.recovered != 1 || executions != 1 {
		t.Fatalf(
			"adaptive recovery/executions = %d/%d, want 1/1",
			registry.recovered,
			executions,
		)
	}

	registry = &cliMemoryRegistry{request: &request, recover: true}
	if code := runCLIIdempotent(
		ctx,
		[]string{"task", "cancel", "task-1"},
		io.Discard,
		io.Discard,
		registry,
		executor,
		false,
	); code == 0 {
		t.Fatal("non-adaptive in-progress parent was re-executed")
	}
	if registry.recovered != 0 || executions != 1 {
		t.Fatalf(
			"non-adaptive recovery/executions = %d/%d, want 0/1",
			registry.recovered,
			executions,
		)
	}
}

type cliStaleCompletionRegistry struct {
	request       idempotency.BeginRequest
	replay        *idempotency.ReplayResult
	beginCalls    int
	recoveryCalls int
	marked        int
	changeReplay  bool
	terminalToken idempotency.ClaimToken
}

func (registry *cliStaleCompletionRegistry) Begin(
	_ context.Context,
	request idempotency.BeginRequest,
) (idempotency.BeginDecision, error) {
	registry.beginCalls++
	if request != registry.request {
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionConflict,
		}, idempotency.ErrConflict
	}
	if registry.beginCalls == 1 {
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionInProgress,
		}, nil
	}
	return idempotency.BeginDecision{
		Decision: idempotency.DecisionReplay,
		Replay:   registry.replay,
	}, nil
}

func (registry *cliStaleCompletionRegistry) RecoverExpired(
	context.Context,
	idempotency.BeginRequest,
) (idempotency.ClaimToken, bool, error) {
	registry.recoveryCalls++
	return 2, true, nil
}

func (registry *cliStaleCompletionRegistry) Complete(
	_ context.Context,
	request idempotency.CompleteRequest,
) error {
	registry.terminalToken = request.ClaimToken
	replay := request.Result
	var normalized bytes.Buffer
	if err := json.Indent(&normalized, replay.Body, "", "  "); err != nil {
		return err
	}
	replay.Body = normalized.Bytes()
	if registry.changeReplay {
		replay.Body = []byte(`{"exit_code":0,"stdout":"different\n"}`)
	}
	registry.replay = &replay
	return idempotency.ErrStaleTransition
}

func (registry *cliStaleCompletionRegistry) MarkIndeterminate(
	context.Context,
	idempotency.OperationKey,
	[32]byte,
	idempotency.ClaimToken,
) error {
	registry.marked++
	return nil
}

func TestRunCLIIdempotentReconcilesStaleCompletionWithoutSecondPrint(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name         string
		changeReplay bool
		wantCode     int
		wantMarked   int
	}{
		{name: "equivalent completed replay", wantCode: 0},
		{
			name:         "different completed replay",
			changeReplay: true,
			wantCode:     2,
			wantMarked:   1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := testCLIBeginRequest(t, "stale-completion")
			registry := &cliStaleCompletionRegistry{
				request: request, changeReplay: testCase.changeReplay,
			}
			var stdout, stderr bytes.Buffer
			code := runCLIIdempotent(
				cliOperationContextForTest(t, request),
				[]string{"eval", "adaptive", "seal-admission"},
				&stdout,
				&stderr,
				registry,
				func(
					_ context.Context,
					_ []string,
					output io.Writer,
					_ io.Writer,
				) int {
					_, _ = io.WriteString(output, "sealed once\n")
					return 0
				},
				true,
			)
			if code != testCase.wantCode {
				t.Fatalf(
					"exit = %d, want %d; stderr=%q",
					code,
					testCase.wantCode,
					stderr.String(),
				)
			}
			if stdout.String() != "sealed once\n" {
				t.Fatalf("stdout = %q, want one produced result", stdout.String())
			}
			if registry.beginCalls != 2 ||
				registry.recoveryCalls != 1 ||
				registry.marked != testCase.wantMarked ||
				registry.terminalToken != 2 {
				t.Fatalf(
					"begin/recovery/marked/token = %d/%d/%d/%d, want 2/1/%d/2",
					registry.beginCalls,
					registry.recoveryCalls,
					registry.marked,
					registry.terminalToken,
					testCase.wantMarked,
				)
			}
		})
	}
}

func TestCanonicalCLIReplayBodyRejectsAmbiguousEnvelope(t *testing.T) {
	t.Parallel()
	canonical := []byte(
		`{"exit_code":0,"stdout":"sealed\n"}`,
	)
	normalized, ok := canonicalCLIReplayBody([]byte(
		"{\n  \"stdout\": \"sealed\\n\",\n  \"exit_code\": 0\n}",
	))
	if !ok || !bytes.Equal(normalized, canonical) {
		t.Fatalf("normalized replay = %q/%t, want %q/true", normalized, ok, canonical)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate", body: `{"exit_code":0,"exit_code":0}`},
		{name: "unknown", body: `{"exit_code":0,"unknown":true}`},
		{name: "missing exit code", body: `{"stdout":"sealed\n"}`},
		{name: "trailing", body: `{"exit_code":0} {}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, ok := canonicalCLIReplayBody(
				[]byte(testCase.body),
			); ok {
				t.Fatalf("accepted ambiguous replay %s", testCase.name)
			}
		})
	}
}

func testCLIBeginRequest(t *testing.T, key string) idempotency.BeginRequest {
	t.Helper()
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Key string `json:"key"`
	}{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return idempotency.BeginRequest{
		Operation: idempotency.OperationKey{
			IdentityID: "00000000-0000-0000-0000-000000000039",
			Scope:      idempotency.ScopeCLICommand,
			Key:        key,
		},
		Fingerprint: fingerprint,
	}
}

func cliOperationContextForTest(
	t *testing.T,
	request idempotency.BeginRequest,
) context.Context {
	t.Helper()
	ctx, err := idempotency.WithOperation(
		identityctx.WithIdentityID(
			context.Background(),
			request.Operation.IdentityID,
		),
		idempotency.Operation{
			Key: request.Operation, Fingerprint: request.Fingerprint,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
