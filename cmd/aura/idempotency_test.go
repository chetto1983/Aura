package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
