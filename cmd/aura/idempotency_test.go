package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
)

type cliMemoryRegistry struct {
	request *idempotency.BeginRequest
	replay  *idempotency.ReplayResult
	marked  int
}

func (m *cliMemoryRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	if m.request == nil {
		copyRequest := request
		m.request = &copyRequest
		return idempotency.BeginDecision{Decision: idempotency.DecisionAcquired}, nil
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
	if m.request == nil || m.request.Operation != request.Operation || m.request.Fingerprint != request.Fingerprint {
		return errors.New("unexpected CLI completion")
	}
	result := request.Result
	m.replay = &result
	return nil
}

func (m *cliMemoryRegistry) MarkIndeterminate(context.Context, idempotency.OperationKey, [32]byte) error {
	m.marked++
	return nil
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

func TestRunCLIIdempotentExecutesOnceAndReplaysRealOutput(t *testing.T) {
	t.Parallel()

	args := []string{"task", "cancel", "task-1", "--operation-key", "stable-cli-key"}
	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &cliMemoryRegistry{}
	effects := 0
	executor := func(_ context.Context, childArgs []string, stdout, _ io.Writer) int {
		effects++
		if !slices.Contains(childArgs, "stable-cli-key") {
			t.Errorf("child args = %q, operation key missing", childArgs)
		}
		_, _ = io.WriteString(stdout, "cancelled task-1\n")
		return 0
	}
	var firstOut, firstErr bytes.Buffer
	if code := runCLIIdempotent(ctx, cleaned, &firstOut, &firstErr, registry, executor); code != 0 {
		t.Fatalf("first exit = %d, stderr=%q", code, firstErr.String())
	}
	var replayOut, replayErr bytes.Buffer
	if code := runCLIIdempotent(ctx, cleaned, &replayOut, &replayErr, registry, executor); code != 0 {
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
	if code := runCLIIdempotent(ctx, cleaned, io.Discard, io.Discard, registry, executor); code != 7 {
		t.Fatalf("exit = %d, want 7", code)
	}
	if registry.marked != 1 || registry.replay != nil {
		t.Fatalf("marked=%d replay=%v, want terminal indeterminate", registry.marked, registry.replay)
	}
}
