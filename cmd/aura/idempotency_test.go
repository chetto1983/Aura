package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
)

type cliMemoryRegistry struct {
	request          *idempotency.BeginRequest
	replay           *idempotency.ReplayResult
	marked           int
	recover          bool
	recovered        int
	activeClaimToken idempotency.ClaimToken
}

func (m *cliMemoryRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	if m.request == nil {
		copyRequest := request
		m.request = &copyRequest
		m.activeClaimToken = 1
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionAcquired, ClaimToken: m.activeClaimToken,
		}, nil
	}
	if m.request.Operation != request.Operation || m.request.Fingerprint != request.Fingerprint {
		return idempotency.BeginDecision{Decision: idempotency.DecisionConflict}, idempotency.ErrConflict
	}
	if m.replay == nil {
		return idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}, nil
	}
	return idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: m.replay}, nil
}

func (m *cliMemoryRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	if m.request == nil || m.request.Operation != request.Operation ||
		m.request.Fingerprint != request.Fingerprint ||
		request.ClaimToken != m.activeClaimToken {
		return errors.New("unexpected CLI completion")
	}
	result := request.Result
	m.replay = &result
	return nil
}

func (m *cliMemoryRegistry) MarkIndeterminate(
	_ context.Context,
	_ idempotency.OperationKey,
	_ [32]byte,
	claimToken idempotency.ClaimToken,
) error {
	if claimToken != m.activeClaimToken {
		return errors.New("unexpected CLI indeterminate claim token")
	}
	m.marked++
	return nil
}

func (m *cliMemoryRegistry) RecoverExpired(
	_ context.Context,
	request idempotency.BeginRequest,
) (idempotency.ClaimToken, bool, error) {
	m.recovered++
	if m.request == nil ||
		m.request.Operation != request.Operation ||
		m.request.Fingerprint != request.Fingerprint {
		return 0, false, nil
	}
	if !m.recover {
		return 0, false, nil
	}
	if m.activeClaimToken == 0 {
		m.activeClaimToken = 1
	}
	m.activeClaimToken++
	return m.activeClaimToken, true, nil
}

// cliOperationFromContext reads back what prepareCLIIdempotency stamped on the context.
//
// It was production code until 2026-08-16 and no production caller ever existed: three tests
// were its only readers, so it lives with them now rather than sitting in cmd/aura pretending
// the CLI consults it.
func cliOperationFromContext(ctx context.Context) (string, [32]byte, bool) {
	operation, ok := idempotency.OperationFromContext(ctx)
	return operation.Key.Key, operation.Fingerprint, ok
}

func TestPrepareCLIIdempotencyExplicitKeyIsStable(t *testing.T) {
	t.Parallel()

	args := []string{"task", "run_now", "task-1", "--operation-key", "caller-key"}
	first, firstArgs, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	second, secondArgs, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	firstKey, firstFingerprint, firstOK := cliOperationFromContext(first)
	secondKey, secondFingerprint, secondOK := cliOperationFromContext(second)
	if !firstOK || !secondOK {
		t.Fatal("prepared CLI contexts lack operation metadata")
	}
	if firstKey != secondKey || firstFingerprint != secondFingerprint || firstKey != "caller-key" {
		t.Fatalf("retry operations differ: %q/%x != %q/%x", firstKey, firstFingerprint, secondKey, secondFingerprint)
	}
	if strings.Join(firstArgs, " ") != "task run_now task-1" || strings.Join(secondArgs, " ") != "task run_now task-1" {
		t.Fatalf("operation flag leaked to command args: %q / %q", firstArgs, secondArgs)
	}
}

func TestPrepareCLIIdempotencyUsesDurableServiceIdentity(t *testing.T) {
	t.Parallel()

	ctx, _, err := prepareCLIIdempotency(context.Background(), []string{
		"db", "migrate", "--operation-key", "deploy-migration-key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		t.Fatal("prepared CLI context lacks operation metadata")
	}
	const durableCLIIdentity = "00000000-0000-0000-0000-000000000039"
	if operation.Key.IdentityID != durableCLIIdentity {
		t.Fatalf("CLI operation identity = %q, want durable service identity %q", operation.Key.IdentityID, durableCLIIdentity)
	}
}

func TestPrepareCLIIdempotencyDisplaysGeneratedKeyBeforeExecution(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ctx, _, err := prepareCLIIdempotency(context.Background(), []string{"chat", "delete", "conv-1"}, &out)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	key, _, ok := cliOperationFromContext(ctx)
	if !ok || key == "" {
		t.Fatalf("generated operation missing: key=%q ok=%v", key, ok)
	}
	if !strings.Contains(out.String(), key) {
		t.Fatalf("generated key %q was not displayed in %q", key, out.String())
	}
}

func TestPrepareCLIIdempotencyRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	_, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-1", "--operation-key", "bad\nkey"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("invalid operation key accepted")
	}
}

func TestPrepareCLIIdempotencyFingerprintsNormalizedIntent(t *testing.T) {
	t.Parallel()

	first, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-1", "--operation-key", "same-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	changed, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-2", "--operation-key", "same-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	_, firstFingerprint, _ := cliOperationFromContext(first)
	_, changedFingerprint, _ := cliOperationFromContext(changed)
	if firstFingerprint == changedFingerprint {
		t.Fatal("changed command payload produced the same operation fingerprint")
	}
}

func TestPrepareCLIIdempotencyPreservesGenericFingerprintGolden(t *testing.T) {
	t.Parallel()
	ctx, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"task", "cancel", "task-1",
			"--operation-key", "generic-golden-key",
		},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fingerprint, ok := cliOperationFromContext(ctx)
	if !ok {
		t.Fatal("generic command lacks operation fingerprint")
	}
	want := sha256.Sum256(
		[]byte(`{"command":"task cancel","args":["task-1"]}`),
	)
	if fingerprint != want {
		t.Fatalf("generic fingerprint = %x, want legacy golden %x", fingerprint, want)
	}
}

func TestCLIMutationPathMatchesWholeCommandsOnly(t *testing.T) {
	t.Parallel()

	command, mutating := cliMutationPath([]string{
		"identity", "revoke", "--id", "11111111-1111-4111-8111-111111111111",
	})
	if !mutating || command != "identity revoke" {
		t.Fatalf("revoke mutation = %q/%t, want the exact two-token command", command, mutating)
	}
	if command, mutating := cliMutationPath([]string{
		"identity", "list",
	}); mutating || command != "" {
		t.Fatalf("list mutation = %q/%t, want read-only", command, mutating)
	}
	if command, mutating := cliMutationPath([]string{
		"identity", "revoke-extra",
	}); mutating || command != "" {
		t.Fatalf("prefix mutation = %q/%t, want exact rejection", command, mutating)
	}
}

func TestCLIMutationPathIncludesMemoryUpdate(t *testing.T) {
	t.Parallel()

	command, mutating := cliMutationPath([]string{
		"memory", "update", "preference", "pref-1", "preference=corrected",
	})
	if !mutating || command != "memory update" {
		t.Fatalf("memory update mutation = %q/%t, want exact idempotent command", command, mutating)
	}
}

func TestMemoryCommandKeepsPreparedInvocationContext(t *testing.T) {
	t.Parallel()

	ctx, _, err := prepareCLIIdempotency(context.Background(), []string{
		"memory", "update", "entity", "entity-1", "name=corrected",
		"--operation-key", "memory-update-key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok || operation.Key.Key != "memory-update-key" {
		t.Fatalf("memory update operation = %#v/%t, want prepared replay identity", operation, ok)
	}
}

func TestPrepareCLIIdempotencyRejectsOperationKeyForAReadOnlyCommand(
	t *testing.T,
) {
	t.Parallel()
	_, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{"identity", "list", "--operation-key", "read-only-key"},
		io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "only valid for mutating commands") {
		t.Fatalf("read-only operation key error = %v", err)
	}
}

func TestRunCLIIdempotentExecutesOnceAndReplaysRealOutput(t *testing.T) {
	t.Parallel()

	args := []string{"task", "cancel", "task-1", "--operation-key", "stable-cli-key"}
	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &cliMemoryRegistry{}
	effects := 0
	executor := func(ctx context.Context, childArgs []string, stdout, _ io.Writer) int {
		effects++
		operation, ok := idempotency.OperationFromContext(ctx)
		if !ok || operation.ClaimToken != 1 {
			t.Fatalf("executor operation = %+v/%t, want claim token 1", operation, ok)
		}
		if !slices.Contains(childArgs, "stable-cli-key") {
			t.Errorf("child args = %q, operation key missing", childArgs)
		}
		_, _ = io.WriteString(stdout, "cancelled task-1\n")
		return 0
	}
	var firstOut, firstErr bytes.Buffer
	if code := runCLIIdempotent(
		ctx, cleaned, &firstOut, &firstErr, registry, executor,
	); code != 0 {
		t.Fatalf("first exit = %d, stderr=%q", code, firstErr.String())
	}
	var replayOut, replayErr bytes.Buffer
	if code := runCLIIdempotent(
		ctx, cleaned, &replayOut, &replayErr, registry, executor,
	); code != 0 {
		t.Fatalf("replay exit = %d, stderr=%q", code, replayErr.String())
	}
	if effects != 1 {
		t.Fatalf("effects = %d, want one", effects)
	}
	if firstOut.String() != replayOut.String() || replayOut.String() != "cancelled task-1\n" {
		t.Fatalf("outputs first=%q replay=%q", firstOut.String(), replayOut.String())
	}
}

func TestRunCLIIdempotentFailureBecomesTerminalIndeterminate(t *testing.T) {
	t.Parallel()

	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), []string{"config", "set", "x", "y", "--operation-key", "failed-cli-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &cliMemoryRegistry{}
	executor := func(context.Context, []string, io.Writer, io.Writer) int { return 7 }
	if code := runCLIIdempotent(
		ctx, cleaned, io.Discard, io.Discard, registry, executor,
	); code != 7 {
		t.Fatalf("exit = %d, want 7", code)
	}
	if registry.marked != 1 || registry.replay != nil {
		t.Fatalf("marked=%d replay=%v, want terminal indeterminate", registry.marked, registry.replay)
	}
}

func TestExecuteCLIIdempotentParentMigrationUsesSchemaOwner(t *testing.T) {
	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), []string{
		"db", "migrate", "--operation-key", "migration-owner-key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	original := cliMutationCommands["db migrate"]
	t.Cleanup(func() { cliMutationCommands["db migrate"] = original })
	called := 0
	meta := original
	meta.Execute = func(_ context.Context, args []string, _, _ io.Writer) int {
		called++
		if !slices.Contains(args, "migration-owner-key") {
			t.Fatalf("migration child args = %q, stable operation key missing", args)
		}
		return 0
	}
	cliMutationCommands["db migrate"] = meta

	handled, code := executeCLIIdempotentParent(ctx, cleaned, io.Discard, io.Discard)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d, want schema owner success", handled, code)
	}
	if called != 1 {
		t.Fatalf("migration executions=%d, want one", called)
	}
}
